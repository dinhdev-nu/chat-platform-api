package queue

import (
	"context"
	"time"
)

const MaxRetries = 3

const (
	JobSendOTPEmail                    = "email.send_otp"
	JobUpdateConversationLastActivity  = "conversation.last_activity.update"
	JobCreateConversationSystemMessage = "conversation.system_message.create"
	JobUpdateAuthTokenLastUsed         = "auth.token_last_used.update"
	JobUpdateUserLastSeen              = "user.last_seen.update"
)

var StreamByJobType = map[string]string{
	JobSendOTPEmail:                    "stream:email",
	JobUpdateConversationLastActivity:  "stream:conversation",
	JobCreateConversationSystemMessage: "stream:conversation",
	JobUpdateAuthTokenLastUsed:         "stream:auth",
	JobUpdateUserLastSeen:              "stream:user",
}

var GroupByStream = map[string]string{
	"stream:email":        "email-workers",
	"stream:conversation": "conversation-workers",
	"stream:auth":         "auth-workers",
	"stream:user":         "user-workers",
}

var MaxLenByStream = map[string]int64{
	"stream:email":        10_000,
	"stream:conversation": 50_000,
	"stream:auth":         10_000,
	"stream:user":         10_000,
}

const defaultMaxLen int64 = 10_000

func GetStreamByJobType(jobType string) string {
	if s, ok := StreamByJobType[jobType]; ok {
		return s
	}
	return "stream:default"
}

func GetMaxLenByStream(stream string) int64 {
	if l, ok := MaxLenByStream[stream]; ok {
		return l
	}
	return defaultMaxLen
}

type SendOTPEmailPayload struct {
	Email        string `json:"email"`
	OTP          string `json:"otp"`
	IPAddress    string `json:"ipAddress"`
	ExpiresInMin int    `json:"expiresInMin"`
}

type ConversationLastActivityPayload struct {
	ConversationID []byte    `json:"conversationId"`
	MessageID      []byte    `json:"messageId"`
	MessageText    *string   `json:"messageText,omitempty"`
	ActivityAt     time.Time `json:"activityAt"`
}

type ConversationSystemMessagePayload struct {
	ConversationID []byte    `json:"conversationId"`
	MessageID      []byte    `json:"messageId"`
	SenderID       []byte    `json:"senderId"`
	Content        string    `json:"content"`
	Seq            uint64    `json:"seq"`
	ActivityAt     time.Time `json:"activityAt"`
}

type AuthTokenLastUsedPayload struct {
	JTI    []byte    `json:"jti"`
	UsedAt time.Time `json:"usedAt"`
}

type UserLastSeenPayload struct {
	UserID []byte    `json:"userId"`
	SeenAt time.Time `json:"seenAt"`
}

// Job là envelope(vỏ) bọc paylaod bất kỳ
type Job struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Payload   []byte    `json:"payload"`
	Attempt   int       `json:"attempt"`
	CreatedAt time.Time `json:"createdAt"`
	RetryAt   time.Time `json:"retryAt"`
}

func (j Job) IsReady() bool {
	return j.RetryAt.IsZero() || !time.Now().Before(j.RetryAt)
}

type Handler interface {
	Type() string                              // Trả về job type
	Handle(ctx context.Context, job Job) error // Xử lý job, err -> retry, nil -> ACK(xóa job khỏi queue)
}
