package repository

import (
	"context"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

type MessageRepository interface {
	GetMaxSeq(ctx context.Context, convID []byte) (int64, error)

	InsertMessage(ctx context.Context, msg *model.Message) error
	InsertSystemMessage(ctx context.Context, msg *model.Message) error
	InsertAttachment(ctx context.Context, att *model.Attachment) error
	InsertMessageReaction(ctx context.Context, msgID, userID []byte, emoji string) (int64, error)
	BatchInsertAttachments(ctx context.Context, args []*model.Attachment) error

	ListMessages(ctx context.Context, convID []byte, cursorTS *time.Time, cursorSeq *uint64, limit int32) ([]*model.Message, error)
	GetAttachmentsByMessageIDs(ctx context.Context, msgIDs [][]byte) ([]*model.Attachment, error)
	GetReactionsByMessageIDs(ctx context.Context, msgIDs [][]byte) ([]*model.MessageReaction, error)
	GetMessageCursorTS(ctx context.Context, msgID, convID []byte) (*time.Time, error)
	GetUnreadCountByWatermark(ctx context.Context, userID, convID []byte) (int64, error)
	GetMessageByID(ctx context.Context, id []byte) (*model.Message, error)

	UpdateMessageContent(ctx context.Context, arg *model.Message) (int64, error)

	SoftDeleteMessage(ctx context.Context, id []byte) error

	DeleteMessageReaction(ctx context.Context, msgID, userID []byte, emoji string) error
}
