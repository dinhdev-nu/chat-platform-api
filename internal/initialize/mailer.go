package initialize

import (
	"fmt"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/pkg/mailer/smtp"
)

func InitMailer() {
	m, err := smtp.New(g.Config.Mail)
	if err != nil {
		panic(fmt.Errorf("failed to initialize mailer: %w", err))
	}

	g.Mailer = m
	fmt.Println("Mailer initialized successfully")
}
