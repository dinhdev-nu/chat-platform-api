package service

import (
	"context"
	"encoding/hex"
	"encoding/json"

	"go.uber.org/zap"
)

type ControlPublisher interface {
	PublishChannel(context.Context, string, []byte) error
}

func publishControlEvent(ctx context.Context, publisher ControlPublisher, logger *zap.Logger, userID []byte, event any) {
	if publisher == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		logger.Warn("failed to marshal control event", zap.Error(err))
		return
	}
	userHex := hex.EncodeToString(userID)
	go func() {
		publishCtx, cancel := detachedContext(ctx, sideEffectTimeout)
		defer cancel()
		if err := publisher.PublishChannel(publishCtx, "sys:"+userHex, payload); err != nil {
			logger.Warn("failed to publish control event",
				zap.String("user_id", userHex), zap.Error(err))
		}
	}()
}
