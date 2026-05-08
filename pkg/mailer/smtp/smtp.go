package smtp

import (
	"context"
	"fmt"
	"time"

	c "github.com/dinhdev-nu/chat-platform-api/config"
	"github.com/dinhdev-nu/chat-platform-api/pkg/mailer"
	gomail "github.com/wneessen/go-mail"
)

type smtpMailer struct {
	cfg    c.MailConfig
	client *gomail.Client
}

func New(cfg c.MailConfig) (mailer.Mailer, error) {
	tlsPolicy := gomail.TLSMandatory
	if cfg.Port == 587 {
		tlsPolicy = gomail.TLSOpportunistic
	}

	client, err := gomail.NewClient(cfg.Host,
		gomail.WithPort(cfg.Port),
		gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
		gomail.WithUsername(cfg.From),
		gomail.WithPassword(cfg.Password),
		gomail.WithTLSPolicy(tlsPolicy),
		gomail.WithTimeout(15*time.Second), // ← go-mail support native
	)
	if err != nil {
		return nil, fmt.Errorf("smtp new client: %w", err)
	}

	return &smtpMailer{
		cfg:    cfg,
		client: client,
	}, nil
}

func (m *smtpMailer) Send(ctx context.Context, msg *mailer.Message) error {
	mail := gomail.NewMsg()

	if err := mail.FromFormat(m.cfg.SenderName, m.cfg.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := mail.To(msg.To); err != nil {
		return fmt.Errorf("smtp mail to: %w", err)
	}

	mail.Subject(msg.Subject)
	mail.SetBodyString(gomail.TypeTextHTML, msg.HTML)

	if err := m.client.DialAndSendWithContext(ctx, mail); err != nil {
		return fmt.Errorf("smtp send to %s : %w", msg.To, err)
	}

	return nil
}
