package initialize

import (
	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/gin-gonic/gin"
)

func InitRouter() *gin.Engine {
	switch g.Config.Server.Mode {
	case "production":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}

	return gin.New()
}
