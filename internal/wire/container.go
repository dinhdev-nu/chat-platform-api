package wire

import (
	"context"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis/cache"
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
	Hub         *websocket.Hub
}

func NewContainer(ctx context.Context) *Container {

	// infrastructure
	jwt := provider.NewJWTManager()
	roomManager := websocket.NewRoomManager(g.RedisClient)
	roomViewer := roomManager

	// repositories
	userRepo := provider.NewUserRepository()
	userTokenRepo := provider.NewUserTokenRepository()
	roomRepo := provider.NewRoomRepository()
	messageRepo := provider.NewMessageRepository()
	roomCache := cache.NewRoomCache(g.RedisClient, roomRepo, g.Logger)
	hub := websocket.NewHub(ctx, g.RedisClient, roomManager, roomCache, g.Logger)
	go hub.Run()

	sequences := redis.NewSequenceStore(g.RedisClient, messageRepo)

	// services
	authService := provider.NewAuthService(service.AuthDependencies{
		Users:         userRepo,
		Tokens:        userTokenRepo,
		JWT:           jwt,
		OTP:           g.OTPStore,
		Sessions:      g.Session,
		UserCache:     g.Session,
		Jobs:          g.Stream,
		UsageThrottle: redis.NewTokenUsageStore(g.RedisClient),
		SessionTTL:    time.Duration(g.Config.Jwt.ExpireTime) * time.Second,
		Logger:        g.Logger,
	})
	userService := provider.NewUserService(service.UserDependencies{
		Users:     userRepo,
		UserCache: g.Session,
		Presence:  g.Presence,
		Controls:  g.PubSub,
		Logger:    g.Logger,
	})
	roomService := provider.NewRoomService(service.RoomDependencies{
		Users:     userRepo,
		Rooms:     roomRepo,
		Messages:  messageRepo,
		Cache:     roomCache,
		Sequences: sequences,
		Events:    g.PubSub,
		Controls:  g.PubSub,
		Presence:  g.Presence,
		Jobs:      g.Stream,
		Logger:    g.Logger,
	})
	messageService := provider.NewMessageService(service.MessageDependencies{
		Rooms:     roomRepo,
		Messages:  messageRepo,
		Users:     userRepo,
		Viewer:    roomViewer,
		Cache:     roomCache,
		UserCache: g.Session,
		Sequences: sequences,
		Events:    g.PubSub,
		Jobs:      g.Stream,
		Logger:    g.Logger,
	})

	// handlers
	authHandler := provider.NewAuthHandler(authService)
	userHandler := provider.NewUserHandler(userService)
	roomHandler := provider.NewRoomHandler(roomService)
	messageHandler := provider.NewMessageHandler(messageService)
	wsHandler := provider.NewWebSocketHandler(hub, roomManager, messageService, roomRepo, g.Logger)

	return &Container{
		AuthHandler:      authHandler,
		UserHandler:      userHandler,
		RoomHandler:      roomHandler,
		MessageHandler:   messageHandler,
		WebSocketHandler: wsHandler,

		AuthService: authService,
		Hub:         hub,
	}
}
