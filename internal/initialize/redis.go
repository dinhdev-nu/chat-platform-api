package initialize

import (
	"fmt"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
)

func InitRedis() {
	cfg := g.Config.Redis

	client, err := redis.NewClient(cfg)
	if err != nil {
		panic(fmt.Errorf("failed to initialize Redis: %w", err))
	}

	g.RedisClient = client
	g.Session = redis.NewSessionStore(client)
	g.Presence = redis.NewPresenceStore(client)
	g.OTPStore = redis.NewOTPStore(client)
	g.PubSub = redis.NewPubSubBroker(client)

	fmt.Println("Redis initialized successfully")
}
