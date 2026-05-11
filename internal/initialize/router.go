package initialize

import (
	"net/http"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	"github.com/dinhdev-nu/chat-platform-api/internal/router"
	"github.com/dinhdev-nu/chat-platform-api/internal/wire"
	"github.com/gin-gonic/gin"
)

func InitRouter() *gin.Engine {
	var r *gin.Engine

	switch g.Config.Server.Mode {
	case "production":
		gin.SetMode(gin.ReleaseMode)
	case "test":
		gin.SetMode(gin.TestMode)
	default:
		gin.SetMode(gin.DebugMode)
	}

	r = gin.New()

	r.Use(
		m.Logger(),
		gin.Recovery(),
		m.ErrorHandler(),
	)

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	container := wire.NewContainer()
	v1 := r.Group("/api/v1")

	am := middleware.AuthMiddleware(container.AuthService)
	v1Protected := v1.Group("")
	v1Protected.Use(am)
	{
		router.RegisterAuthRoutes(v1, v1Protected, container.AuthHandler)
		router.RegisterUserRoutes(v1, v1Protected, container.UserHandler)
		router.RegisterRoomRouters(v1, v1Protected, container.RoomHandler)
		router.RegisterMessageRouters(v1, v1Protected, container.MessageHandler)
	}

	return r
}
