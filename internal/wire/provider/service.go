package provider

import s "github.com/dinhdev-nu/chat-platform-api/internal/service"

func NewAuthService(d s.AuthDependencies) *s.AuthService          { return s.NewAuthService(d) }
func NewUserService(d s.UserDependencies) *s.UserService          { return s.NewUserService(d) }
func NewRoomService(d s.RoomDependencies) *s.RoomService          { return s.NewRoomService(d) }
func NewMessageService(d s.MessageDependencies) *s.MessageService { return s.NewMessageService(d) }
