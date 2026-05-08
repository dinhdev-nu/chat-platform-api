package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/pkg/mailer"
	tmpl "github.com/dinhdev-nu/chat-platform-api/pkg/mailer/template"
	"go.uber.org/zap"
)

type sendEmailHandler struct {
	mailer  mailer.Mailer
	logger  *zap.Logger
	appName string
}

func NewSendEmailHandler(m mailer.Mailer, l *zap.Logger, appName string) *sendEmailHandler {
	return &sendEmailHandler{
		mailer:  m,
		logger:  l,
		appName: appName,
	}
}

func (h *sendEmailHandler) Type() string {
	return queue.JobSendOTPEmail
}

func (h *sendEmailHandler) Handle(ctx context.Context, job queue.Job) error {
	var payload queue.SendOTPEmailPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		h.logger.Error("send_email: invalid payload", zap.String("jobID", job.ID), zap.Error(err))
		return nil // ACK
	}

	html, err := tmpl.RenderOTPEmail(
		tmpl.OTPData{
			AppName: h.appName,
			OTP:     payload.OTP,
		},
	)
	if err != nil {
		return fmt.Errorf("send_email: render template for %s: %w", payload.Email, err)
	}

	if err := h.mailer.Send(ctx, &mailer.Message{
		To:      payload.Email,
		Subject: fmt.Sprintf("[%s] Mã xác thực OTP của bạn", h.appName),
		HTML:    html,
	}); err != nil {
		return fmt.Errorf("send_email: send mail to %s: %w", payload.Email, err)
	}

	h.logger.Info("send_email: email sent successfully", zap.String("jobID", job.ID), zap.String("email", payload.Email))
	return nil
}
