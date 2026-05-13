-- name: GetDMConversation :one
-- Tìm DM đã tồn tại giữa 2 users — idempotent create
SELECT c.id
FROM conversations c
JOIN conversation_members m1
  ON m1.conversation_id = c.id AND m1.user_id = ?
JOIN conversation_members m2
  ON m2.conversation_id = c.id AND m2.user_id = ?
WHERE c.type = 1
LIMIT 1;

-- name: GetConversationByID :one
SELECT id, type, name, avatar_url, description, created_by,
       last_message_id, last_activity_at, created_at, updated_at
FROM conversations
WHERE id = ?
LIMIT 1;

-- name: CreateConversation :exec
INSERT INTO conversations
    (id, type, name, avatar_url, description, created_by, last_activity_at)
VALUES (?, ?, ?, ?, ?, ?, NOW(3));

-- name: InsertConversationMember :exec
-- INSERT IGNORE: user đã là member → bỏ qua, không lỗi.
INSERT IGNORE INTO conversation_members (conversation_id, user_id, role)
VALUES (?, ?, ?);


-- name: ListConversationsFirstPage :many
SELECT
    c.id, c.type, c.name, c.avatar_url,
    c.last_message_id, c.last_activity_at, c.created_at, c.updated_at,
    cm.role, cm.is_muted
FROM conversations c
INNER JOIN conversation_members cm
    ON cm.conversation_id = c.id AND cm.user_id = ?
ORDER BY c.last_activity_at DESC, c.id DESC
LIMIT ?;

-- name: ListConversationsNextPage :many
SELECT
    c.id, c.type, c.name, c.avatar_url,
    c.last_message_id, c.last_activity_at, c.created_at, c.updated_at,
    cm.role, cm.is_muted
FROM conversations c
INNER JOIN conversation_members cm
    ON cm.conversation_id = c.id AND cm.user_id = ? 
WHERE (c.last_activity_at < ? OR (c.last_activity_at = ? AND c.id < ?))
ORDER BY c.last_activity_at DESC, c.id DESC
LIMIT ?;

-- name: UpdateLastReadAt :exec
UPDATE conversation_members
SET    last_read_at = ?
WHERE  conversation_id = ?
  AND  user_id        = ?
  AND  (last_read_at IS NULL OR last_read_at < ?);


-- name: GetMemberRole :one
SELECT role FROM conversation_members
WHERE conversation_id = ? AND user_id = ?
LIMIT 1;

-- name: DeleteConversationMember :exec
DELETE FROM conversation_members
WHERE conversation_id = ? AND user_id = ?;

-- name: GetConversationMember :one
SELECT id, conversation_id, user_id, role, is_muted, last_read_at, joined_at
FROM conversation_members
WHERE conversation_id = ? AND user_id = ?
LIMIT 1;

-- name: GetConversationMemberIDs :many
-- Dùng để warm conv:members Redis cache
SELECT user_id FROM conversation_members
WHERE conversation_id = ?;


-- name: UpdateConversationLastActivity :exec
-- Gọi bởi Kafka worker async sau khi gửi tin nhắn.
UPDATE conversations
SET last_message_id = ?, last_activity_at = NOW(3), updated_at = NOW(3)
WHERE id = ?;

-- name: GetUserConversationIDs :many
-- Dùng khi WS connect: load toàn bộ conv user đang tham gia.
SELECT conversation_id
FROM conversation_members
WHERE user_id = ?;