package global

import (
	"github.com/dinhdev-nu/chat-platform-api/config"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	Config  *config.Config
	Logger  *zap.Logger
	MySQLDB *gorm.DB
)
