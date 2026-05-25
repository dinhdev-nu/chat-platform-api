package wire

import (
	"context"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/internal/websocket"
	"github.com/dinhdev-nu/chat-platform-api/internal/wire/provider"
)

type Container struct {
	AuthHandler      *handler.AuthHandler
	UserHandler      *handler.UserHandler
	RoomHandler      *handler.RoomHandler
	MessageHandler   *handler.MessageHandler
	WebSocketHandler *websocket.Handler

	AuthService *service.AuthService
}

func NewContainer() *Container {

	// infrastructure
	jwt := provider.NewJWTManager()
	mailQueue := provider.NewSendEmailQueueHandler()
	roomManager := websocket.NewRoomManager()
	roomViewer := roomManager
	hub := websocket.NewHub(g.RedisClient, roomManager, g.Logger)
	go hub.Run(context.Background())

	// repositories
	userRepo := provider.NewUserRepository()
	userTokenRepo := provider.NewUserTokenRepository()
	roomRepo := provider.NewRoomRepository()
	messageRepo := provider.NewMessageRepository()

	// services
	authService := provider.NewAuthService(userRepo, userTokenRepo, jwt, mailQueue)
	userService := provider.NewUserService(userRepo)
	roomService := provider.NewRoomService(userRepo, roomRepo, messageRepo)
	messageService := provider.NewMessageService(roomRepo, messageRepo, userRepo, roomViewer)

	// handlers
	authHandler := provider.NewAuthHandler(authService)
	userHandler := provider.NewUserHandler(userService)
	roomHandler := provider.NewRoomHandler(roomService)
	messageHandler := provider.NewMessageHandler(messageService)
	wsHandler := provider.NewWebSocketHandler(hub, roomManager, roomRepo, userRepo, g.Logger)

	return &Container{
		AuthHandler:      authHandler,
		UserHandler:      userHandler,
		RoomHandler:      roomHandler,
		MessageHandler:   messageHandler,
		WebSocketHandler: wsHandler,

		AuthService: authService,
	}
}
