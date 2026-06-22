package queue

import (
	"context"

	"github.com/dinhdev-nu/chat-platform-api/internal/service"
)

type enqueueStream interface {
	EnqueueJob(ctx context.Context, jobType string, payload any) error
}

type StreamJobEnqueuer struct {
	stream enqueueStream
}

func NewStreamJobEnqueuer(stream enqueueStream) *StreamJobEnqueuer {
	if stream == nil {
		return nil
	}
	return &StreamJobEnqueuer{stream: stream}
}

func (e *StreamJobEnqueuer) EnqueueSendOTPEmail(ctx context.Context, payload service.SendOTPEmailJob) error {
	return e.stream.EnqueueJob(ctx, JobSendOTPEmail, SendOTPEmailPayload{
		Email:        payload.Email,
		OTP:          payload.OTP,
		IPAddress:    payload.IPAddress,
		ExpiresInMin: payload.ExpiresInMin,
	})
}

func (e *StreamJobEnqueuer) EnqueueConversationLastActivity(
	ctx context.Context,
	payload service.ConversationLastActivityJob,
) error {
	return e.stream.EnqueueJob(ctx, JobUpdateConversationLastActivity, ConversationLastActivityPayload{
		ConversationID: payload.ConversationID,
		MessageID:      payload.MessageID,
		MessageText:    payload.MessageText,
		ActivityAt:     payload.ActivityAt,
	})
}

func (e *StreamJobEnqueuer) EnqueueConversationSystemMessage(
	ctx context.Context,
	payload service.ConversationSystemMessageJob,
) error {
	return e.stream.EnqueueJob(ctx, JobCreateConversationSystemMessage, ConversationSystemMessagePayload{
		ConversationID: payload.ConversationID,
		MessageID:      payload.MessageID,
		SenderID:       payload.SenderID,
		Content:        payload.Content,
		Seq:            payload.Seq,
		ActivityAt:     payload.ActivityAt,
	})
}

func (e *StreamJobEnqueuer) EnqueueAuthTokenLastUsed(ctx context.Context, payload service.AuthTokenLastUsedJob) error {
	return e.stream.EnqueueJob(ctx, JobUpdateAuthTokenLastUsed, AuthTokenLastUsedPayload{
		JTI:    payload.JTI,
		UsedAt: payload.UsedAt,
	})
}
