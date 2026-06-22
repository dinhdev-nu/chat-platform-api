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
	deps s.AuthServiceDeps,
) *s.AuthService {
	return s.NewAuthService(ur, tr, jm, deps)
}

func NewUserService(ur r.UserRepository, deps s.UserServiceDeps) *s.UserService {
	return s.NewUserService(ur, deps)
}

func NewRoomService(ur r.UserRepository, rr r.RoomRepository, mr r.MessageRepository, deps s.RoomServiceDeps) *s.RoomService {
	return s.NewRoomService(ur, rr, mr, deps)
}

func NewMessageService(
	rr r.RoomRepository,
	mg r.MessageRepository,
	ur r.UserRepository,
	rv s.RoomViewer,
	deps s.MessageServiceDeps,
) *s.MessageService {
	return s.NewMessageService(rr, mg, ur, rv, deps)
}
