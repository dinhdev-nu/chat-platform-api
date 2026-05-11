package wire

import (
	"github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/internal/wire/provider"
)

type Container struct {
	AuthHandler    *handler.AuthHandler
	UserHandler    *handler.UserHandler
	RoomHandler    *handler.RoomHandler
	MessageHandler *handler.MessageHandler

	AuthService *service.AuthService
}

func NewContainer() *Container {

	// providers
	jwt := provider.NewJWTManager()
	mailQueue := provider.NewSendEmailQueueHandler()
	roomViewer := provider.NewRoomViewer()

	// repositories
	userRepo := provider.NewUserRepository()
	userTokenRepo := provider.NewUserTokenRepository()
	roomRepo := provider.NewRoomRepository()
	messageRepo := provider.NewMessageRepository()

	// services
	authService := provider.NewAuthService(userRepo, userTokenRepo, jwt, mailQueue)
	userService := provider.NewUserService(userRepo)
	roomService := provider.NewRoomService(userRepo, roomRepo, messageRepo)
	messageService := provider.NewMessageService(roomRepo, messageRepo, roomViewer)

	// handlers
	authHandler := provider.NewAuthHandler(authService)
	userHandler := provider.NewUserHandler(userService)
	roomHandler := provider.NewRoomHandler(roomService)
	messageHandler := provider.NewMessageHandler(messageService)

	return &Container{
		AuthHandler:    authHandler,
		UserHandler:    userHandler,
		RoomHandler:    roomHandler,
		MessageHandler: messageHandler,

		AuthService: authService,
	}
}
