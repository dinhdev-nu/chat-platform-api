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

type AuthTokenLastUsedHandler struct {
	tokenRepo r.UserTokenRepository
	logger    *zap.Logger
}

func NewAuthTokenLastUsedHandler(tokenRepo r.UserTokenRepository, logger *zap.Logger) queue.Handler {
	return &AuthTokenLastUsedHandler{
		tokenRepo: tokenRepo,
		logger:    logger,
	}
}

func (h *AuthTokenLastUsedHandler) Type() string {
	return queue.JobUpdateAuthTokenLastUsed
}

func (h *AuthTokenLastUsedHandler) Handle(ctx context.Context, job queue.Job) error {
	var payload queue.AuthTokenLastUsedPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		h.logger.Error("auth.token_last_used: invalid payload", zap.String("jobID", job.ID), zap.Error(err))
		return nil
	}
	if len(payload.JTI) != 16 || payload.UsedAt.IsZero() {
		h.logger.Error("auth.token_last_used: invalid payload fields",
			zap.String("jobID", job.ID),
			zap.Int("jtiLen", len(payload.JTI)),
			zap.Time("usedAt", payload.UsedAt),
		)
		return nil
	}
	if h.tokenRepo == nil {
		return fmt.Errorf("auth.token_last_used: token repo is nil")
	}

	if err := h.tokenRepo.UpdateLastUsed(ctx, payload.JTI, payload.UsedAt); err != nil {
		return fmt.Errorf("auth.token_last_used: update %s: %w", hex.EncodeToString(payload.JTI), err)
	}
	return nil
}
