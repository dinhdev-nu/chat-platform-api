package repository

import (
	"context"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

type RoomRepository interface {
	GetDMConversation(ctx context.Context, userID1, userID2 []byte) ([]byte, error)
	GetConversationByID(ctx context.Context, id []byte) (*model.Conversation, error)
	GetMemberRole(ctx context.Context, convID, userID []byte) (model.MemberRole, error)
	GetConversationMember(ctx context.Context, convID, userID []byte) (*model.ConversationMember, error)
	GetConversationMemberIDs(ctx context.Context, convID []byte) ([][]byte, error)
	GetUserConversationIDs(ctx context.Context, userID []byte) ([][]byte, error)
	CreateConversation(ctx context.Context, conv *model.Conversation) error

	InsertConversationMember(ctx context.Context, memb *model.ConversationMember) error

	UpdateConversationLastActivity(ctx context.Context, convID, lastMsgID []byte, lastMsgText *string, activityAt time.Time) error
	UpdateLastReadAt(ctx context.Context, convID, userID []byte, cursorTS *time.Time) error

	BatchInsertConversationMembers(ctx context.Context, membs []*model.ConversationMember) error

	ListConversations(ctx context.Context, userID []byte, cursorTS *time.Time, cursorID []byte, limit int32) ([]*model.ConversationListRow, error)

	DeleteConversationMember(ctx context.Context, convID, userID []byte) error
}
