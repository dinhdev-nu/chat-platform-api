package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	"go.uber.org/zap"
)

type OTPStore interface {
	Set(ctx context.Context, email, code string) error
	Get(ctx context.Context, email string) (string, error)
	Delete(ctx context.Context, email string) error
	ClearSendState(ctx context.Context, email string) error
	IncrAttempts(ctx context.Context, email string) (int64, error)
	Lock(ctx context.Context, email string) error
	IsLocked(ctx context.Context, email string) (bool, error)
	SetResendLimit(ctx context.Context, email string) error
	CanResend(ctx context.Context, email string) (bool, error)
}

type SessionStore interface {
	Set(ctx context.Context, jti, userID []byte, ttl time.Duration) error
	Get(ctx context.Context, jti []byte) (string, error)
	Revoke(ctx context.Context, jti []byte) error
}

type UserCache interface {
	WarmUser(ctx context.Context, userID []byte, payload string) error
	GetUser(ctx context.Context, userID []byte) (string, error)
	GetUsersWithMisses(ctx context.Context, userIDs [][]byte) (map[string]*model.User, [][]byte, error)
	WarmUsers(ctx context.Context, users map[string]*model.User) error
}

type PresenceStore interface {
	IsOnline(ctx context.Context, userID []byte) (bool, error)
	BulkIsOnline(ctx context.Context, userIDs [][]byte) (map[string]bool, error)
}

type RoomCache interface {
	BatchIncrUnread(ctx context.Context, userIDs [][]byte, convID []byte) error
	SetUnread(ctx context.Context, userID, convID []byte, count int64) error
	DeleteUnread(ctx context.Context, userID, convID []byte) error
	GetUnreads(ctx context.Context, userID []byte, convIDs [][]byte) (map[string]int64, error)
	GetMembers(ctx context.Context, convID []byte) ([][]byte, error)
	WarmMember(ctx context.Context, convID []byte, userIDs [][]byte) error
	RefreshTTL(ctx context.Context, convID []byte) error
	AddMember(ctx context.Context, convID, userID []byte) error
	RemoveMember(ctx context.Context, convID, userID []byte) error
	InvalidateMembers(ctx context.Context, convID []byte) error
	IsMember(ctx context.Context, convID, userID []byte) (isMember bool, cacheHit bool, err error)
}

type TokenLastUsedThrottle interface {
	ShouldUpdateTokenLastUsed(ctx context.Context, jti []byte, ttl time.Duration) (bool, error)
}

type MessageSequence interface {
	Next(ctx context.Context, convID []byte) (uint64, error)
}

type JobEnqueuer interface {
	EnqueueSendOTPEmail(ctx context.Context, payload SendOTPEmailJob) error
	EnqueueConversationLastActivity(ctx context.Context, payload ConversationLastActivityJob) error
	EnqueueConversationSystemMessage(ctx context.Context, payload ConversationSystemMessageJob) error
	EnqueueAuthTokenLastUsed(ctx context.Context, payload AuthTokenLastUsedJob) error
}

type SendOTPEmailJob struct {
	Email        string
	OTP          string
	IPAddress    string
	ExpiresInMin int
}

type ConversationLastActivityJob struct {
	ConversationID []byte
	MessageID      []byte
	MessageText    *string
	ActivityAt     time.Time
}

type ConversationSystemMessageJob struct {
	ConversationID []byte
	MessageID      []byte
	SenderID       []byte
	Content        string
	Seq            uint64
	ActivityAt     time.Time
}

type AuthTokenLastUsedJob struct {
	JTI    []byte
	UsedAt time.Time
}

type EventType string

const (
	EventConvCreated   EventType = "conversation.created"
	EventMemberAdded   EventType = "member.added"
	EventMemberRemoved EventType = "member.removed"

	EventNewMessage  EventType = "message.new"
	EventReadMessage EventType = "message.read"
	EventEditMessage EventType = "message.edited"
	EventDelMessage  EventType = "message.deleted"

	EventToggleReaction EventType = "reaction.toggle"
)

type Event struct {
	Type    EventType
	ConvID  string
	Payload json.RawMessage
}

type EventPublisher interface {
	PublishConversation(ctx context.Context, convID []byte, event Event) error
	PublishUserSystem(ctx context.Context, userID []byte, payload json.RawMessage) error
}

func serviceLogger(log *zap.Logger) *zap.Logger {
	if log != nil {
		return log
	}
	return zap.NewNop()
}
