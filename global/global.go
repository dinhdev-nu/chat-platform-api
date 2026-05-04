package global

import (
	"github.com/dinhdev-nu/chat-platform-api/config"
	"go.uber.org/zap"
)

var (
	Config *config.Config
	Logger *zap.Logger
)
