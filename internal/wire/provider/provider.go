package provider

import (
	"github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/internal/websocket"
	"github.com/dinhdev-nu/chat-platform-api/pkg/jwt"
)

func NewJWTManager() *jwt.JWTManager {
	return jwt.NewJWTManager(&global.Config.Jwt)
}

func NewRoomViewer() service.RoomViewer {
	return websocket.NewRoomManager()
}
