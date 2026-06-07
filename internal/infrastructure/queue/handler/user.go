package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"go.uber.org/zap"
)

type UserLastSeenHandler struct {
	userRepo r.UserRepository
	logger   *zap.Logger
}

func NewUserLastSeenHandler(userRepo r.UserRepository, logger *zap.Logger) queue.Handler {
	return &UserLastSeenHandler{
		userRepo: userRepo,
		logger:   logger,
	}
}

func (h *UserLastSeenHandler) Type() string {
	return queue.JobUpdateUserLastSeen
}

func (h *UserLastSeenHandler) Handle(ctx context.Context, job queue.Job) error {
	var payload queue.UserLastSeenPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		h.logger.Error("user.last_seen: invalid payload", zap.String("jobID", job.ID), zap.Error(err))
		return nil
	}
	if len(payload.UserID) != 16 || payload.SeenAt.IsZero() {
		h.logger.Error("user.last_seen: invalid payload fields",
			zap.String("jobID", job.ID),
			zap.Int("userIDLen", len(payload.UserID)),
			zap.Time("seenAt", payload.SeenAt),
		)
		return nil
	}
	if h.userRepo == nil {
		return fmt.Errorf("user.last_seen: user repo is nil")
	}

	if err := h.userRepo.UpdateLastSeenAt(ctx, payload.UserID, payload.SeenAt); err != nil {
		return fmt.Errorf("user.last_seen: update %s: %w", hex.EncodeToString(payload.UserID), err)
	}
	return nil
}
