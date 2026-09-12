package seed

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis/cache"
)

func roomArgs(d *Dataset) []any {
	args := make([]any, len(d.Conversations))
	for i, c := range d.Conversations {
		args[i] = c.ID
	}
	return args
}

func userArgs(d *Dataset, ownedOnly bool) []any {
	var args []any
	for i, u := range d.Users {
		// Keep both test accounts on reset, including accounts initially created by seed.
		if !ownedOnly || (i >= 2 && u.Owned) {
			args = append(args, u.ID)
		}
	}
	return args
}

func expectCount(ctx context.Context, q queryer, label, query string, args []any, expected int) error {
	var count int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return fmt.Errorf("verify %s: %w", label, err)
	}
	if count != expected {
		return fmt.Errorf("verify %s: got %d, expected %d", label, count, expected)
	}
	return nil
}

// Baseline checks exact initial totals and memberships. Normal verification allows
// new messages, reads, edits and membership changes made through the application.
func verifySQL(ctx context.Context, q queryer, d *Dataset, baseline bool) error {
	rooms, users := roomArgs(d), userArgs(d, false)
	in := "(" + placeholders(len(rooms)) + ")"
	if err := expectCount(ctx, q, "users", "SELECT COUNT(*) FROM users WHERE id IN ("+placeholders(len(users))+")", users, len(users)); err != nil {
		return err
	}
	if err := expectCount(ctx, q, "conversations", "SELECT COUNT(*) FROM conversations WHERE id IN "+in, rooms, len(rooms)); err != nil {
		return err
	}
	checks := []struct{ name, query string }{
		{"message sequence collisions", `SELECT COUNT(*) FROM (SELECT conversation_id, seq FROM messages WHERE conversation_id IN ` + in + ` GROUP BY conversation_id, seq HAVING COUNT(*) > 1 OR seq = 0) collisions`},
		{"cross-room or missing reply parents", `SELECT COUNT(*) FROM messages m LEFT JOIN messages p ON p.id=m.parent_id WHERE m.conversation_id IN ` + in + ` AND m.parent_id IS NOT NULL AND (p.id IS NULL OR p.conversation_id <> m.conversation_id OR p.seq >= m.seq)`},
		{"invalid read cursors", `SELECT COUNT(*) FROM conversation_members cm WHERE cm.conversation_id IN ` + in + ` AND ((cm.last_read_at IS NULL AND cm.last_read_seq <> 0) OR (cm.last_read_at IS NOT NULL AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id=cm.conversation_id AND m.created_at=cm.last_read_at AND m.seq=cm.last_read_seq)))`},
		{"conversation previews/activity", `SELECT COUNT(*) FROM conversations c LEFT JOIN messages m ON m.id=(SELECT latest.id FROM messages latest WHERE latest.conversation_id=c.id ORDER BY latest.created_at DESC, latest.seq DESC LIMIT 1) WHERE c.id IN ` + in + ` AND ((m.id IS NULL AND (c.last_message_id IS NOT NULL OR c.last_message_text IS NOT NULL OR c.last_activity_at IS NULL)) OR (m.id IS NOT NULL AND (NOT (c.last_message_id <=> m.id) OR NOT (c.last_activity_at <=> m.created_at) OR NOT (c.last_message_text <=> CASE WHEN m.is_deleted=1 THEN 'Message deleted' ELSE LEFT(m.content,1000) END))))`},
		{"invalid direct memberships", `SELECT COUNT(*) FROM conversations c WHERE c.id IN ` + in + ` AND c.type=1 AND (SELECT COUNT(*) FROM conversation_members cm WHERE cm.conversation_id=c.id) <> 2`},
	}
	if baseline {
		checks = append(checks, struct{ name, query string }{"senders outside membership", `SELECT COUNT(*) FROM messages m LEFT JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.user_id=m.sender_id WHERE m.conversation_id IN ` + in + ` AND (cm.user_id IS NULL OR m.created_at < cm.joined_at)`})
		for _, check := range []struct {
			name, query string
			count       int
		}{
			{"messages", "SELECT COUNT(*) FROM messages WHERE conversation_id IN " + in, d.Options.Messages},
			{"memberships", "SELECT COUNT(*) FROM conversation_members WHERE conversation_id IN " + in, d.Summary().Members},
			{"reactions", "SELECT COUNT(*) FROM message_reactions r JOIN messages m ON m.id=r.message_id WHERE m.conversation_id IN " + in, d.Summary().Reactions},
		} {
			if err := expectCount(ctx, q, check.name, check.query, rooms, check.count); err != nil {
				return err
			}
		}
	}
	for _, check := range checks {
		if err := expectCount(ctx, q, check.name, check.query, rooms, 0); err != nil {
			return err
		}
	}
	return nil
}

type counter struct {
	Key   string
	Value int64
	Seq   bool
}

func (s *Store) counters(ctx context.Context, d *Dataset) ([]counter, error) {
	var result []counter
	for _, c := range d.Conversations {
		var seq int64
		if err := s.conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(seq),0) FROM messages WHERE conversation_id=?", c.ID).Scan(&seq); err != nil {
			return nil, err
		}
		result = append(result, counter{Key: "seq:" + hex.EncodeToString(c.ID), Value: seq, Seq: true})
		rows, err := s.conn.QueryContext(ctx, `SELECT cm.user_id, COUNT(m.id)
 FROM conversation_members cm LEFT JOIN messages m
 ON m.conversation_id=cm.conversation_id AND m.sender_id <> cm.user_id AND m.is_deleted=0
 AND (cm.last_read_at IS NULL OR m.created_at > cm.last_read_at OR (m.created_at=cm.last_read_at AND m.seq > cm.last_read_seq))
 WHERE cm.conversation_id=? GROUP BY cm.user_id`, c.ID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id []byte
			var unread int64
			if err := rows.Scan(&id, &unread); err != nil {
				_ = rows.Close()
				return nil, err
			}
			result = append(result, counter{Key: cache.UnreadKey(id, c.ID), Value: unread})
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Store) verifyCounters(ctx context.Context, d *Dataset) error {
	counters, err := s.counters(ctx, d)
	if err != nil {
		return err
	}
	for start := 0; start < len(counters); start += 500 {
		batch := counters[start:min(start+500, len(counters))]
		keys := make([]string, len(batch))
		for i, c := range batch {
			keys[i] = c.Key
		}
		values, err := s.redis.MGet(ctx, keys...).Result()
		if err != nil {
			return err
		}
		for i, c := range batch {
			value, ok := values[i].(string)
			actual, err := strconv.ParseInt(value, 10, 64)
			if !ok || err != nil || (c.Seq && actual < c.Value) || (!c.Seq && actual != c.Value) {
				return fmt.Errorf("redis %s is missing or inconsistent with MySQL (expected %d)", c.Key, c.Value)
			}
		}
	}
	return nil
}

func (s *Store) Verify(ctx context.Context, name string, baseline bool) error {
	d, state, err := s.load(ctx, name)
	if err != nil {
		return err
	}
	if state != "ready" {
		return fmt.Errorf("dataset is %s, not ready; finish sync or reset first", state)
	}
	if err := verifySQL(ctx, s.conn, d, baseline); err != nil {
		return err
	}
	if err := s.verifyCounters(ctx, d); err != nil {
		return err
	}
	s.logf("Verified: MySQL relationships, message ordering, previews, read watermarks and Redis counters.\n")
	return nil
}
