-- Pause API/worker writes before applying this migration (MySQL DDL auto-commits).
-- See migrations/README.md for the unread-cache rebuild and rollout procedure.
-- +goose Up
-- +goose StatementBegin
ALTER TABLE conversation_members
  MODIFY COLUMN last_read_at DATETIME(3) NULL DEFAULT NULL
    COMMENT 'Read cursor timestamp; compare together with last_read_seq',
  ADD COLUMN last_read_seq BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER last_read_at;
-- +goose StatementEnd

-- The old DATETIME rounded or truncated milliseconds, and did not retain seq.
-- Its exact read position cannot be recovered. Rewind one second conservatively
-- rather than marking unread messages as read; nearby read messages may reappear.
-- +goose StatementBegin
UPDATE conversation_members
SET last_read_at = DATE_SUB(last_read_at, INTERVAL 1 SECOND)
WHERE last_read_at IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE conversations
  MODIFY COLUMN last_activity_at DATETIME(3) NULL DEFAULT NULL
    COMMENT 'Timestamp of the latest message, or creation time for an empty conversation';
-- +goose StatementEnd

-- Recover activity and preview from the stored messages, including soft deletes.
-- Empty conversations retain their existing activity time.
-- +goose StatementBegin
UPDATE conversations c
JOIN messages latest ON latest.id = (
  SELECT m.id FROM messages m
  WHERE m.conversation_id = c.id
  ORDER BY m.created_at DESC, m.seq DESC
  LIMIT 1
)
SET c.last_message_id = latest.id,
    c.last_message_text = CASE WHEN latest.is_deleted = 1 THEN 'Message deleted' ELSE LEFT(latest.content, 1000) END,
    c.last_activity_at = latest.created_at;
-- +goose StatementEnd

-- +goose Down
-- Lossy rollback: fractional precision and same-millisecond read positions are lost.
-- +goose StatementBegin
ALTER TABLE conversation_members
  DROP COLUMN last_read_seq,
  MODIFY COLUMN last_read_at DATETIME NULL DEFAULT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE conversations
  MODIFY COLUMN last_activity_at DATETIME NULL DEFAULT NULL;
-- +goose StatementEnd
