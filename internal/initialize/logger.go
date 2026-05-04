package initialize

import (
	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/pkg/logger"
)

func InitLogger() {
	mode := g.Config.Server.Mode
	c := g.Config.Logger

	g.Logger = logger.New(mode, c)
}
