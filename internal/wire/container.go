package wire

import (
	"context"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	iredis "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
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
	roomManager := websocket.NewRoomManager()
	roomViewer := roomManager
	hub := websocket.NewHub(ctx, g.RedisClient, roomManager, g.Logger)
	go hub.Run()

	// repositories
	userRepo := provider.NewUserRepository()
	userTokenRepo := provider.NewUserTokenRepository()
	roomRepo := provider.NewRoomRepository()
	messageRepo := provider.NewMessageRepository()

	// application adapters
	roomCache := cache.NewRoomCache(g.RedisClient)
	jobEnqueuer := queue.NewStreamJobEnqueuer(g.Stream)
	eventPublisher := iredis.NewEventPublisher(g.RedisClient, g.PubSub)
	messageSequence := iredis.NewMessageSequence(g.RedisClient, messageRepo)
	tokenLastUsedThrottle := iredis.NewTokenLastUsedThrottle(g.RedisClient)

	// services
	authService := provider.NewAuthService(userRepo, userTokenRepo, jwt, service.AuthServiceDeps{
		OTPStore:              g.OTPStore,
		Session:               g.Session,
		UserCache:             g.Session,
		JobEnqueuer:           jobEnqueuer,
		TokenLastUsedThrottle: tokenLastUsedThrottle,
		TokenTTL:              time.Duration(g.Config.Jwt.ExpireTime) * time.Second,
		Logger:                g.Logger,
	})
	userService := provider.NewUserService(userRepo, service.UserServiceDeps{
		UserCache:      g.Session,
		Presence:       g.Presence,
		EventPublisher: eventPublisher,
		Logger:         g.Logger,
	})
	roomService := provider.NewRoomService(userRepo, roomRepo, messageRepo, service.RoomServiceDeps{
		RoomCache:       roomCache,
		Presence:        g.Presence,
		EventPublisher:  eventPublisher,
		JobEnqueuer:     jobEnqueuer,
		MessageSequence: messageSequence,
		Logger:          g.Logger,
	})
	messageService := provider.NewMessageService(roomRepo, messageRepo, userRepo, roomViewer, service.MessageServiceDeps{
		RoomCache:       roomCache,
		UserCache:       g.Session,
		EventPublisher:  eventPublisher,
		JobEnqueuer:     jobEnqueuer,
		MessageSequence: messageSequence,
		Logger:          g.Logger,
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
