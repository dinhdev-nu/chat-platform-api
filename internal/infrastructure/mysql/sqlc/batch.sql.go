package sqlc

import (
	"context"
	"fmt"
	"strings"
)

func (q *Queries) BatchInsertConversationMembers(ctx context.Context, args []InsertConversationMemberParams) error {
	if len(args) == 0 {
		return nil
	}

	placeholders := make([]string, len(args))
	queryArgs := make([]interface{}, 0, len(args)*3)

	for i := range args {
		placeholders[i] = "(?, ?, ?)"
		queryArgs = append(queryArgs, args[i].ConversationID, args[i].UserID, args[i].Role) // ✅ Thêm dòng này
	}

	query := fmt.Sprintf(
		"INSERT IGNORE INTO conversation_members (conversation_id, user_id, role) VALUES %s",
		strings.Join(placeholders, ", "),
	)

	_, err := q.db.ExecContext(ctx, query, queryArgs...)
	return err
}

// -- name: InsertAttachment :exec
// INSERT INTO attachments
//     (id, message_id, file_name, file_url, mime_type,
//      file_size_bytes, width, height, duration_sec)
// VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

func (q *Queries) BatchInsertAttachments(ctx context.Context, args []InsertAttachmentParams) error {
	if len(args) == 0 {
		return nil
	}

	placeholders := make([]string, len(args))
	queryArgs := make([]interface{}, 0, len(args)*9)

	for i := range args {
		placeholders[i] = "(?, ?, ?, ?, ?, ?, ?, ?, ?)"
		queryArgs = append(queryArgs, args[i].ID, args[i].MessageID, args[i].FileName, args[i].FileUrl, args[i].MimeType, args[i].FileSizeBytes, args[i].Width, args[i].Height, args[i].DurationSec)
	}

	query := fmt.Sprintf(
		"INSERT IGNORE INTO attachments (id, message_id, file_name, file_url, mime_type, file_size_bytes, width, height, duration_sec) VALUES %s",
		strings.Join(placeholders, ", "),
	)

	_, err := q.db.ExecContext(ctx, query, queryArgs...)
	return err
}
