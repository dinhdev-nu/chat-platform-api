package seed

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis/cache"
)

// Sequence values are decimal strings: lexical/length comparison avoids Lua's
// floating-point integer limit and never lowers an already allocated sequence.
const syncSequence = `
local current = redis.call('GET', KEYS[1])
local wanted = ARGV[1]
if current and not string.match(current, '^%d+$') then return redis.error_reply('invalid sequence value') end
if not current or #current < #wanted or (#current == #wanted and current < wanted) then
  redis.call('SET', KEYS[1], wanted)
end
return 1`

func (s *Store) Sync(ctx context.Context, name string) error {
	d, state, err := s.load(ctx, name)
	if err != nil {
		return err
	}
	if state != "ready" && state != "sql-ready" {
		return fmt.Errorf("cannot sync a dataset in state %s; finish reset first", state)
	}
	if err := verifySQL(ctx, s.conn, d, false); err != nil {
		return err
	}
	return s.syncDataset(ctx, d)
}

func (s *Store) syncDataset(ctx context.Context, d *Dataset) error {
	if _, err := s.conn.ExecContext(ctx, "UPDATE "+registryTable+" SET state='sql-ready' WHERE dataset=?", d.Options.Dataset); err != nil {
		return err
	}
	counters, err := s.counters(ctx, d)
	if err != nil {
		return err
	}
	for start := 0; start < len(counters); start += 500 {
		pipe := s.redis.Pipeline()
		for _, c := range counters[start:min(start+500, len(counters))] {
			if c.Seq {
				pipe.Eval(ctx, syncSequence, []string{c.Key}, c.Value)
			} else {
				pipe.Set(ctx, c.Key, c.Value, 0)
			}
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	pipe := s.redis.Pipeline()
	for _, c := range d.Conversations {
		pipe.Del(ctx, "conv:members:v2:"+hex.EncodeToString(c.ID))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}
	if err := s.verifyCounters(ctx, d); err != nil {
		return err
	}
	_, err = s.conn.ExecContext(ctx, "UPDATE "+registryTable+" SET state='ready' WHERE dataset=?", d.Options.Dataset)
	if err == nil {
		s.logf("Redis synchronized and verified. Dataset is ready.\n")
	}
	return err
}

// Reset preserves the two test accounts. It refuses to delete synthetic users
// with relationships outside this dataset; it never truncates tables or flushes Redis.
func (s *Store) Reset(ctx context.Context, name string) error {
	d, state, err := s.load(ctx, name)
	if err != nil {
		return err
	}
	if state != "ready" && state != "sql-ready" && state != "reset-db-done" {
		return fmt.Errorf("unknown dataset state %q", state)
	}
	if state != "reset-db-done" {
		tx, err := s.conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		rooms, owned, users := roomArgs(d), userArgs(d, true), userArgs(d, false)
		ri, ui := "("+placeholders(len(rooms))+")", "("+placeholders(len(owned))+")"
		// These checks cover ownership, memberships, authored messages, reactions,
		// contacts, sessions and OAuth records before cascading deletes can affect them.
		checks := []struct {
			name, query string
			args        []any
		}{
			{"external conversation ownership", "SELECT COUNT(*) FROM conversations WHERE created_by IN " + ui + " AND id NOT IN " + ri, concatArgs(owned, rooms)},
			{"external memberships", "SELECT COUNT(*) FROM conversation_members WHERE user_id IN " + ui + " AND conversation_id NOT IN " + ri, concatArgs(owned, rooms)},
			{"external sent messages", "SELECT COUNT(*) FROM messages WHERE sender_id IN " + ui + " AND conversation_id NOT IN " + ri, concatArgs(owned, rooms)},
			{"external reactions", "SELECT COUNT(*) FROM message_reactions r JOIN messages m ON m.id=r.message_id WHERE r.user_id IN " + ui + " AND m.conversation_id NOT IN " + ri, concatArgs(owned, rooms)},
			{"external read receipts", "SELECT COUNT(*) FROM message_status s JOIN messages m ON m.id=s.message_id WHERE s.user_id IN " + ui + " AND m.conversation_id NOT IN " + ri, concatArgs(owned, rooms)},
			{"user contacts", "SELECT COUNT(*) FROM user_contacts WHERE user_id IN " + ui + " OR contact_id IN " + ui, concatArgs(owned, owned)},
			{"user sessions", "SELECT COUNT(*) FROM user_tokens WHERE user_id IN " + ui, owned},
			{"OAuth accounts", "SELECT COUNT(*) FROM oauth_accounts WHERE user_id IN " + ui, owned},
			{"external replies to seed messages", "SELECT COUNT(*) FROM messages m JOIN messages p ON p.id=m.parent_id WHERE p.conversation_id IN " + ri + " AND m.conversation_id NOT IN " + ri, concatArgs(rooms, rooms)},
			{"new participants outside the dataset", "SELECT COUNT(*) FROM conversation_members WHERE conversation_id IN " + ri + " AND user_id NOT IN (" + placeholders(len(users)) + ")", concatArgs(rooms, users)},
		}
		for _, check := range checks {
			if err := expectCount(ctx, tx, check.name, check.query, check.args, 0); err != nil {
				return fmt.Errorf("reset refused to protect related data: %w", err)
			}
		}
		// #nosec G202 -- ri is generated placeholders only; every conversation ID is bound.
		if _, err := tx.ExecContext(ctx, "DELETE FROM conversations WHERE id IN "+ri, rooms...); err != nil {
			return err
		}
		// #nosec G202 -- ui is generated placeholders only; every owned user ID is bound.
		if _, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id IN "+ui, owned...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE "+registryTable+" SET state='reset-db-done' WHERE dataset=?", name); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("reset commit result uncertain; rerun reset to inspect the registry: %w", err)
		}
	}
	// All dataset users x rooms also removes stale unread keys after users left rooms.
	var keys []string
	for _, c := range d.Conversations {
		id := hex.EncodeToString(c.ID)
		keys = append(keys, "seq:"+id, "look:seq_init:"+id, "conv:members:v2:"+id)
		for _, u := range d.Users {
			keys = append(keys, cache.UnreadKey(u.ID, c.ID))
		}
	}
	for start := 0; start < len(keys); start += 500 {
		if err := s.redis.Del(ctx, keys[start:min(start+500, len(keys))]...).Err(); err != nil {
			return fmt.Errorf("MySQL reset is committed; rerun reset to finish Redis cleanup: %w", err)
		}
	}
	if _, err := s.conn.ExecContext(ctx, "DELETE FROM "+registryTable+" WHERE dataset=?", name); err != nil {
		return err
	}
	s.logf("Dataset reset. Both test accounts and their sessions were preserved.\n")
	return nil
}

func concatArgs(a, b []any) []any {
	return append(append([]any(nil), a...), b...)
}
