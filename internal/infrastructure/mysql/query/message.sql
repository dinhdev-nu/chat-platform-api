-- name: GetMaxSeq :one
-- Xử lý conversation chưa có message (NULL → 0).
SELECT CAST(COALESCE(MAX(seq), 0) AS UNSIGNED) AS max_seq
FROM messages
WHERE conversation_id = ?;

-- name: InsertSystemMessage :exec
-- System message (type=6): member joined/left, group created, v.v.
INSERT INTO messages (id, conversation_id, sender_id, type, content, seq)
VALUES (?, ?, ?, 6, ?, ?);

-- name: InsertMessage :exec
INSERT INTO messages
    (id, conversation_id, sender_id, parent_id, type,
     content, content_encrypted, iv, seq)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: InsertAttachment :exec
INSERT INTO attachments
    (id, message_id, file_name, file_url, mime_type,
     file_size_bytes, width, height, duration_sec)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListMessagesFirstPage :many
SELECT id, conversation_id, sender_id, parent_id, type,
       content, content_encrypted, iv, seq,
       is_edited, is_deleted, deleted_at, created_at, updated_at
FROM messages
WHERE conversation_id = ? AND is_deleted = 0
ORDER BY created_at DESC, seq DESC
LIMIT ?;

-- name: ListMessagesNextPage :many
-- Cursor pagination dùng (created_at, seq) — time trước thu hẹp phạm vi.
SELECT id, conversation_id, sender_id, parent_id, type,
       content, content_encrypted, iv, seq,
       is_edited, is_deleted, deleted_at, created_at, updated_at
FROM messages
WHERE conversation_id = ?
  AND is_deleted = 0
  AND (created_at < ? OR (created_at = ? AND seq < ?))
ORDER BY created_at DESC, seq DESC
LIMIT ?;

-- name: GetAttachmentsByMessageIDs :many
SELECT id, message_id, file_name, file_url,
    mime_type, file_size_bytes, width, height,
    duration_sec, created_at
FROM attachments
WHERE message_id IN (sqlc.slice(message_ids))
ORDER BY created_at ASC;

-- name: GetReactionsByMessageIDs :many
SELECT id, message_id, user_id, emoji , created_at
FROM message_reactions 
WHERE message_id IN (sqlc.slice(message_ids))
ORDER BY created_at ASC;

-- name: GetMessageCursorTS :one
SELECT created_at
FROM messages
WHERE id = ? AND conversation_id = ?
LIMIT 1;

-- name: GetUnreadCountByWatermark :one
SELECT COUNT(*) AS count
FROM   messages        m
JOIN   conversation_members cm
         ON  cm.conversation_id = m.conversation_id
         AND cm.user_id         = ?
WHERE  m.conversation_id = ?
  AND  m.sender_id       != ?
  AND  m.is_deleted      = 0
  AND  (cm.last_read_at IS NULL OR m.created_at > cm.last_read_at);


-- name: GetMessageByID :one
SELECT id, conversation_id, sender_id, parent_id, type,
       content, content_encrypted, iv, seq,
       is_edited, is_deleted, deleted_at, created_at, updated_at
FROM messages
WHERE id = ?
LIMIT 1;

-- name: UpdateMessageContent :execrows
UPDATE messages
SET content = ?, is_edited = 1, updated_at = NOW(3)
WHERE id = ?
  AND sender_id = ?
  AND is_deleted = 0
  AND created_at > DATE_SUB(NOW(), INTERVAL 24 HOUR);

-- name: SoftDeleteMessage :exec
UPDATE messages
SET is_deleted = 1, deleted_at = NOW(), content = NULL, updated_at = NOW(3)
WHERE id = ? AND is_deleted = 0;

-- name: InsertMessageReaction :execrows
-- INSERT IGNORE: nếu đã react → affected = 0 → logic bỏ reaction
INSERT IGNORE INTO message_reactions (message_id, user_id, emoji)
VALUES (?, ?, ?);

-- name: DeleteMessageReaction :exec
DELETE FROM message_reactions
WHERE message_id = ? AND user_id = ? AND emoji = ?;