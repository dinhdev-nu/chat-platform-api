package seed

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/config"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
)

const registryTable = "_stello_local_seeds"

type userRef struct {
	ID    string `json:"id"`
	Owned bool   `json:"owned"`
}

type manifest struct {
	Version      int       `json:"version"`
	Options      Options   `json:"options"`
	Users        []userRef `json:"users"`
	RedisAddress string    `json:"redis_address"`
	RedisDB      int       `json:"redis_db"`
}

type Store struct {
	db           *sql.DB
	conn         *sql.Conn // Owns the advisory lock for the entire command.
	redis        *redis.Client
	redisAddress string
	redisDB      int
	batchSize    int
	out          io.Writer
}

// Progress is best effort: a closed output stream must not turn a committed write
// into a reported database failure and prompt an unnecessary retry.
func (s *Store) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(s.out, format, args...)
}

// CheckTarget is deliberately independent of connection creation.
func CheckTarget(cfg *config.Config, appEnv, database string) error {
	if appEnv != "local" || cfg.Server.IsProduction() {
		return fmt.Errorf("database commands require APP_ENV=local and a non-production server mode")
	}
	if database == "" || database != cfg.MySQL.Database {
		return fmt.Errorf("--database must explicitly match the configured local MySQL database")
	}
	local := func(host, service string) bool {
		host = strings.Trim(strings.ToLower(host), "[]")
		ip := net.ParseIP(host)
		return host == "localhost" || host == service || (ip != nil && ip.IsLoopback())
	}
	if !local(cfg.MySQL.Host, "mysql") || !local(cfg.Redis.Host, "redis") {
		return fmt.Errorf("seed connections must use loopback addresses or Docker service names mysql/redis")
	}
	return nil
}

// Open never migrates the application schema. Callers must validate the target first.
func Open(ctx context.Context, cfg *config.Config, batchSize int, out io.Writer) (*Store, error) {
	if batchSize < 1 || batchSize > 1000 {
		return nil, fmt.Errorf("batch-size must be between 1 and 1000")
	}
	dsn := mysqldriver.NewConfig()
	dsn.User, dsn.Passwd, dsn.DBName = cfg.MySQL.Username, cfg.MySQL.Password, cfg.MySQL.Database
	dsn.Net, dsn.Addr = "tcp", net.JoinHostPort(cfg.MySQL.Host, strconv.Itoa(cfg.MySQL.Port))
	// Match config.MySQLConfig.BuildDSN (loc=Local) used by API and worker.
	dsn.ParseTime, dsn.Loc = true, time.Local
	dsn.Timeout, dsn.ReadTimeout, dsn.WriteTimeout = 5*time.Second, 30*time.Second, 30*time.Second
	dsn.Params = map[string]string{"charset": "utf8mb4"}
	db, err := sql.Open("mysql", dsn.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	addr := net.JoinHostPort(cfg.Redis.Host, strconv.Itoa(cfg.Redis.Port))
	s := &Store{db: db, redisAddress: addr, redisDB: cfg.Redis.Database, batchSize: batchSize, out: out}
	s.redis = redis.NewClient(&redis.Options{Addr: addr, Password: cfg.Redis.Password, DB: cfg.Redis.Database,
		DialTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second})
	s.conn, err = db.Conn(ctx)
	if err == nil {
		err = s.redis.Ping(ctx).Err()
	}
	if err == nil {
		var locked sql.NullInt64
		err = s.conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('stello-seed:', LEFT(SHA2(DATABASE(), 256), 48)), 0)").Scan(&locked)
		if err == nil && (!locked.Valid || locked.Int64 != 1) {
			err = fmt.Errorf("another seed command holds this database's seed lock")
		}
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() {
	if s.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, _ = s.conn.ExecContext(ctx, "SELECT RELEASE_LOCK(CONCAT('stello-seed:', LEFT(SHA2(DATABASE(), 256), 48)))")
		cancel()
		_ = s.conn.Close()
	}
	if s.redis != nil {
		_ = s.redis.Close()
	}
	_ = s.db.Close()
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) checkSchema(ctx context.Context) error {
	queries := []string{
		"SELECT id, username, email, bio, status, created_at, updated_at FROM users LIMIT 0",
		"SELECT id, type, name, created_by, last_message_id, last_message_text, last_activity_at, created_at, updated_at FROM conversations LIMIT 0",
		"SELECT conversation_id, user_id, role, is_muted, joined_at, last_read_at, last_read_seq FROM conversation_members LIMIT 0",
		"SELECT id, conversation_id, sender_id, parent_id, type, content, seq, is_edited, is_deleted, deleted_at, created_at, updated_at FROM messages LIMIT 0",
		"SELECT message_id, user_id, emoji, created_at FROM message_reactions LIMIT 0",
	}
	for _, query := range queries {
		rows, err := s.conn.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("schema is not ready; run cmd/schema then Goose migrations first: %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureRegistry(ctx context.Context) error {
	_, err := s.conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS _stello_local_seeds (
 dataset VARCHAR(48) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 state VARCHAR(24) NOT NULL,
 manifest JSON NOT NULL
) ENGINE=InnoDB COMMENT='Local seed tool ownership and recovery metadata'`)
	return err
}

func (s *Store) load(ctx context.Context, name string) (*Dataset, string, error) {
	var state string
	var raw []byte
	err := s.conn.QueryRowContext(ctx, "SELECT state, manifest FROM "+registryTable+" WHERE dataset = ?", name).Scan(&state, &raw)
	if err != nil {
		return nil, "", err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, "", err
	}
	if m.Version != Version || m.Options.Dataset != name || m.RedisAddress != s.redisAddress || m.RedisDB != s.redisDB {
		return nil, "", fmt.Errorf("seed manifest version or Redis target does not match")
	}
	d, err := Generate(m.Options)
	if err != nil {
		return nil, "", err
	}
	if len(m.Users) != len(d.Users) {
		return nil, "", fmt.Errorf("invalid seed ownership manifest")
	}
	for i, ref := range m.Users {
		id, err := hex.DecodeString(ref.ID)
		if err != nil || len(id) != 16 {
			return nil, "", fmt.Errorf("invalid user ID in seed manifest")
		}
		if ref.Owned {
			if ref.ID != hex.EncodeToString(d.Users[i].ID) {
				return nil, "", fmt.Errorf("owned user ID does not match generated identity")
			}
		} else {
			if i >= 2 {
				return nil, "", fmt.Errorf("only the two test users can be reused")
			}
			d.RebindUser(i, id)
		}
	}
	if err := d.Validate(); err != nil {
		return nil, "", err
	}
	return d, state, nil
}

func (s *Store) Apply(ctx context.Context, d *Dataset) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if err := s.checkSchema(ctx); err != nil {
		return err
	}
	if err := s.ensureRegistry(ctx); err != nil {
		return err
	}
	existing, state, err := s.load(ctx, d.Options.Dataset)
	if err == nil {
		old, next := existing.Options, d.Options
		// An omitted --at defaults to now. The persisted anchor governs reruns.
		next.At = old.At
		if old != next {
			return fmt.Errorf("dataset already exists with different options; reset it before changing the profile")
		}
		if state != "ready" {
			return fmt.Errorf("dataset already exists in state %q; use sync for sql-ready or reset to finish cleanup", state)
		}
		s.logf("Dataset already exists; no rows or Redis keys changed. Use verify to inspect it.\n")
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := reuseTestAccounts(ctx, tx, d); err != nil {
		return err
	}
	if err := checkDirectConversations(ctx, tx, d); err != nil {
		return err
	}
	if err := s.insertDataset(ctx, tx, d); err != nil {
		return err
	}
	m := manifest{Version: Version, Options: d.Options, RedisAddress: s.redisAddress, RedisDB: s.redisDB}
	for _, u := range d.Users {
		m.Users = append(m.Users, userRef{ID: hex.EncodeToString(u.ID), Owned: u.Owned})
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO "+registryTable+" (dataset, state, manifest) VALUES (?, 'sql-ready', ?)", d.Options.Dataset, raw); err != nil {
		return err
	}
	if err := verifySQL(ctx, tx, d, true); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit result is uncertain; rerun apply to inspect the registry before retrying: %w", err)
	}
	s.logf("MySQL committed. Synchronizing Redis for seed conversations only...\n")
	if err := s.syncDataset(ctx, d); err != nil {
		return fmt.Errorf("MySQL is committed; keep writers stopped and run sync: %w", err)
	}
	s.logf("Seed complete: %d users in the dataset, %d conversations, %d messages.\n", len(d.Users), len(d.Conversations), d.Options.Messages)
	return nil
}

func reuseTestAccounts(ctx context.Context, tx *sql.Tx, d *Dataset) error {
	for i := range 2 {
		var id []byte
		var status int
		err := tx.QueryRowContext(ctx, "SELECT id, status FROM users WHERE email = ? FOR UPDATE", d.Users[i].Email).Scan(&id, &status)
		if err == nil {
			if status != 1 {
				return fmt.Errorf("test account %s is not active; its status will not be changed", d.Users[i].Email)
			}
			d.RebindUser(i, id)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return d.Validate()
}

func checkDirectConversations(ctx context.Context, tx *sql.Tx, d *Dataset) error {
	// An existing DM between reused accounts must not be duplicated or adopted.
	for _, c := range d.Conversations {
		if c.Type != 1 {
			continue
		}
		var n int
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations c
 JOIN conversation_members a ON a.conversation_id=c.id AND a.user_id=?
 JOIN conversation_members b ON b.conversation_id=c.id AND b.user_id=? WHERE c.type=1`, c.Members[0].UserID, c.Members[1].UserID).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("a planned DM already exists; use a dedicated local database to avoid changing existing conversations")
		}
	}
	return nil
}

func (s *Store) insertDataset(ctx context.Context, tx *sql.Tx, d *Dataset) error {
	var users, members, messages, reactions [][]any
	rooms := make([][]any, 0, len(d.Conversations))
	for _, u := range d.Users {
		if u.Owned {
			users = append(users, []any{u.ID, u.Username, u.Email, u.Bio, 1, u.CreatedAt, u.CreatedAt})
		}
	}
	for _, c := range d.Conversations {
		lastID, preview := c.LastMessage()
		activity := c.CreatedAt
		if len(c.Messages) > 0 {
			activity = c.Messages[len(c.Messages)-1].CreatedAt
		}
		rooms = append(rooms, []any{c.ID, c.Type, c.Name, c.CreatorID, nullBytes(lastID), preview, activity, c.CreatedAt, activity})
		for _, m := range c.Members {
			members = append(members, []any{c.ID, m.UserID, m.Role, m.Muted, m.JoinedAt, m.ReadAt, m.ReadSeq})
		}
		for _, m := range c.Messages {
			messages = append(messages, []any{m.ID, c.ID, m.SenderID, nullBytes(m.ParentID), m.Type, m.Content,
				m.Seq, m.Edited, m.DeletedAt != nil, m.DeletedAt, m.CreatedAt, m.UpdatedAt})
			for _, r := range m.Reactions {
				reactions = append(reactions, []any{m.ID, r.UserID, r.Emoji, m.CreatedAt.Add(time.Second)})
			}
		}
	}
	tables := []struct {
		name, columns string
		rows          [][]any
	}{
		{"users", "id,username,email,bio,status,created_at,updated_at", users},
		{"conversations", "id,type,name,created_by,last_message_id,last_message_text,last_activity_at,created_at,updated_at", rooms},
		{"conversation_members", "conversation_id,user_id,role,is_muted,joined_at,last_read_at,last_read_seq", members},
		{"messages", "id,conversation_id,sender_id,parent_id,type,content,seq,is_edited,is_deleted,deleted_at,created_at,updated_at", messages},
		{"message_reactions", "message_id,user_id,emoji,created_at", reactions},
	}
	for _, table := range tables {
		if err := insertBatches(ctx, tx, table.name, table.columns, table.rows, s.batchSize); err != nil {
			return fmt.Errorf("insert %s (transaction will roll back): %w", table.name, err)
		}
		s.logf("%s: %d rows staged\n", table.name, len(table.rows))
	}
	return nil
}

func nullBytes(id []byte) any {
	if id == nil {
		return nil
	}
	return id
}

func placeholders(count int) string { return strings.TrimSuffix(strings.Repeat("?,", count), ",") }

func insertBatches(ctx context.Context, tx *sql.Tx, table, columns string, rows [][]any, size int) error {
	for start := 0; start < len(rows); start += size {
		batch := rows[start:min(start+size, len(rows))]
		var args []any
		for _, row := range batch {
			args = append(args, row...)
		}
		values := strings.TrimSuffix(strings.Repeat("("+placeholders(len(batch[0]))+"),", len(batch)), ",")
		// Identifiers are compile-time constants above; every value is a bound parameter.
		// #nosec G202 -- Only the fixed table/column lists in insertDataset and generated placeholders form SQL; row values are bound.
		if _, err := tx.ExecContext(ctx, "INSERT INTO "+table+" ("+columns+") VALUES "+values, args...); err != nil {
			return err
		}
	}
	return nil
}
