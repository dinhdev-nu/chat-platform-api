package provider

import (
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/pkg/jwt"
)

func NewAuthService(
	ur r.UserRepository,
	tr r.UserTokenRepository,
	jm *jwt.JWTManager,
) *s.AuthService {
	return s.NewAuthService(ur, tr, jm)
}

func NewUserService(ur r.UserRepository) *s.UserService {
	return s.NewUserService(ur)
}

func NewRoomService(ur r.UserRepository, rr r.RoomRepository, mr r.MessageRepository) *s.RoomService {
	return s.NewRoomService(ur, rr, mr)
}

func NewMessageService(rr r.RoomRepository, mg r.MessageRepository, ur r.UserRepository, rv s.RoomViewer) *s.MessageService {
	return s.NewMessageService(rr, mg, ur, rv)
}
