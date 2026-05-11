package provider

import (
	"github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/websocket"
	"github.com/dinhdev-nu/chat-platform-api/pkg/jwt"
)

func NewJWTManager() *jwt.JWTManager {
	return jwt.NewJWTManager(&global.Config.Jwt)
}

func NewSendEmailQueueHandler() queue.Handler {
	return handler.NewSendEmailHandler(global.Mailer, global.Logger, global.Config.Mail.SenderName)
}

func NewRoomViewer() websocket.RoomViewer {
	return websocket.NewRoomViewer()
}
