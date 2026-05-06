package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/config"
	gor "github.com/redis/go-redis/v9"
)

func NewClient(cfg config.RedisConfig) (*gor.Client, error) {
	client := gor.NewClient(
		&gor.Options{
			Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
			Password: cfg.Password,
			DB:       cfg.Database,

			PoolSize:     cfg.PoolSize,
			MinIdleConns: cfg.MinIdleConns,
			MaxIdleConns: cfg.PoolSize / 2,

			DialTimeout:  5 * time.Second,
			ReadTimeout:  3 * time.Second,
			WriteTimeout: 3 * time.Second,
			PoolTimeout:  4 * time.Second,

			MaxRetries:      3,
			MinRetryBackoff: 100 * time.Millisecond,
			MaxRetryBackoff: 500 * time.Millisecond,
		},
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel() // Tạo 5s để ping Redis nếu không sẽ bị timeout

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return client, nil
}
