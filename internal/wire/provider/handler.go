package provider

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
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
