package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"go.uber.org/zap"
)

type ConversationLastActivityHandler struct {
	roomRepo r.RoomRepository
	logger   *zap.Logger
}

func NewConversationLastActivityHandler(roomRepo r.RoomRepository, logger *zap.Logger) queue.Handler {
	return &ConversationLastActivityHandler{
		roomRepo: roomRepo,
		logger:   logger,
	}
}

func (h *ConversationLastActivityHandler) Type() string {
	return queue.JobUpdateConversationLastActivity
}

func (h *ConversationLastActivityHandler) Handle(ctx context.Context, job queue.Job) error {
	var payload queue.ConversationLastActivityPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		h.logger.Error("conversation.last_activity: invalid payload", zap.String("jobID", job.ID), zap.Error(err))
		return nil
	}
	if len(payload.ConversationID) != 16 || len(payload.MessageID) != 16 || payload.ActivityAt.IsZero() {
		h.logger.Error("conversation.last_activity: invalid IDs",
			zap.String("jobID", job.ID),
			zap.Int("conversationIDLen", len(payload.ConversationID)),
			zap.Int("messageIDLen", len(payload.MessageID)),
		)
		return nil
	}

	if err := h.roomRepo.UpdateConversationLastActivity(ctx, payload.ConversationID, payload.MessageID, payload.MessageText, payload.ActivityAt); err != nil {
		return fmt.Errorf("conversation.last_activity: update %s/%s: %w",
			hex.EncodeToString(payload.ConversationID),
			hex.EncodeToString(payload.MessageID),
			err,
		)
	}
	return nil
}

type ConversationSystemMessageHandler struct {
	msgRepo  r.MessageRepository
	roomRepo r.RoomRepository
	logger   *zap.Logger
}

func NewConversationSystemMessageHandler(
	msgRepo r.MessageRepository,
	roomRepo r.RoomRepository,
	logger *zap.Logger,
) queue.Handler {
	return &ConversationSystemMessageHandler{
		msgRepo:  msgRepo,
		roomRepo: roomRepo,
		logger:   logger,
	}
}

func (h *ConversationSystemMessageHandler) Type() string {
	return queue.JobCreateConversationSystemMessage
}

func (h *ConversationSystemMessageHandler) Handle(ctx context.Context, job queue.Job) error {
	var payload queue.ConversationSystemMessagePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		h.logger.Error("conversation.system_message: invalid payload", zap.String("jobID", job.ID), zap.Error(err))
		return nil
	}
	if len(payload.ConversationID) != 16 || len(payload.MessageID) != 16 || len(payload.SenderID) != 16 {
		h.logger.Error("conversation.system_message: invalid IDs",
			zap.String("jobID", job.ID),
			zap.Int("conversationIDLen", len(payload.ConversationID)),
			zap.Int("messageIDLen", len(payload.MessageID)),
			zap.Int("senderIDLen", len(payload.SenderID)),
		)
		return nil
	}
	if strings.TrimSpace(payload.Content) == "" || payload.Seq == 0 || payload.ActivityAt.IsZero() {
		h.logger.Error("conversation.system_message: invalid content or seq",
			zap.String("jobID", job.ID),
			zap.Uint64("seq", payload.Seq),
		)
		return nil
	}

	content := payload.Content
	msg := &model.Message{
		ID:             payload.MessageID,
		ConversationID: payload.ConversationID,
		SenderID:       payload.SenderID,
		Type:           model.MessageTypeSystem,
		Content:        &content,
		Seq:            payload.Seq,
	}
	if err := h.msgRepo.InsertSystemMessage(ctx, msg); err != nil {
		if isDuplicateEntry(err) {
			h.logger.Info("conversation.system_message: message already inserted",
				zap.String("messageID", hex.EncodeToString(payload.MessageID)),
			)
		} else {
			return fmt.Errorf("conversation.system_message: insert %s: %w",
				hex.EncodeToString(payload.MessageID),
				err,
			)
		}
	}

	if err := h.roomRepo.UpdateConversationLastActivity(ctx, payload.ConversationID, payload.MessageID, &content, payload.ActivityAt); err != nil {
		return fmt.Errorf("conversation.system_message: update last activity %s: %w",
			hex.EncodeToString(payload.ConversationID),
			err,
		)
	}
	return nil
}

func isDuplicateEntry(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "duplicate entry")
}
