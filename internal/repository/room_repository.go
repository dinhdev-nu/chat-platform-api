package repository

import (
	"context"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

// ConversationMembers is scoped to the transaction passed to WithMembershipLock.
type ConversationMembers interface {
	GetConversationMemberIDs(ctx context.Context, convID []byte) ([][]byte, error)
	InsertConversationMember(ctx context.Context, memb *model.ConversationMember) error
	BatchInsertConversationMembers(ctx context.Context, membs []*model.ConversationMember) error
	DeleteConversationMember(ctx context.Context, convID, userID []byte) error
}

type RoomRepository interface {
	ConversationMembers
	WithMembershipLock(ctx context.Context, convID []byte, apply func(ConversationMembers) error) error
	GetDMConversation(ctx context.Context, userID1, userID2 []byte) ([]byte, error)
	GetConversationByID(ctx context.Context, id []byte) (*model.Conversation, error)
	GetMemberRole(ctx context.Context, convID, userID []byte) (model.MemberRole, error)
	GetConversationMember(ctx context.Context, convID, userID []byte) (*model.ConversationMember, error)
	GetUserConversationIDs(ctx context.Context, userID []byte) ([][]byte, error)
	CreateConversation(ctx context.Context, conv *model.Conversation) error

	UpdateConversationLastActivity(ctx context.Context, convID, lastMsgID []byte, lastMsgText *string, activityAt time.Time) error
	RefreshConversationLastMessage(ctx context.Context, convID, msgID []byte) error
	AdvanceReadWatermark(ctx context.Context, convID, userID []byte, cursor model.MessageCursor) (bool, error)

	ListConversations(ctx context.Context, userID []byte, cursorTS *time.Time, cursorID []byte, limit int32) ([]*model.ConversationListRow, error)
}
