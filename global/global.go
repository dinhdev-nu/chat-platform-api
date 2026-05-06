package global

import (
	"github.com/dinhdev-nu/chat-platform-api/config"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	gored "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	Config *config.Config
	Logger *zap.Logger

	MySQLDB *gorm.DB

	RedisClient *gored.Client
	Session     *redis.SessionStore
	Presence    *redis.PresenceStore
	OTPStore    *redis.OTPStore
	PubSub      *redis.PubSubBroker
)
