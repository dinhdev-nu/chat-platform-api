package provider

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/internal/websocket"
	"go.uber.org/zap"
)

func NewAuthHandler(as *s.AuthService) *h.AuthHandler {
	return h.NewAuthHandler(as)
}

func NewUserHandler(us *s.UserService) *h.UserHandler {
	return h.NewUserHandler(us)
}

func NewRoomHandler(rs *s.RoomService) *h.RoomHandler {
	return h.NewRoomHandler(rs)
}

func NewMessageHandler(ms *s.MessageService) *h.MessageHandler {
	return h.NewMessageHandler(ms)
}

func NewWebSocketHandler(hub *websocket.Hub, rm *websocket.RoomManager, ms *s.MessageService, rr r.RoomRepository, log *zap.Logger) *websocket.Handler {
	return websocket.NewHandler(hub, rm, ms, rr, log)
}
