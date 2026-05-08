package queue

import (
	"context"
	"time"
)

const MaxRetries = 3

const (
	JobSendOTPEmail = "email.send_otp"
)

var StreamByJobType = map[string]string{
	JobSendOTPEmail: "stream:email",
}

var GroupByStream = map[string]string{
	"stream:email": "email-workers",
}

var MaxLenByStream = map[string]int64{
	"stream:email": 10_000,
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
