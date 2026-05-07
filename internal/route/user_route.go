package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterUserRoutes(
	r *gin.Engine,
	uh *h.UserHandler,
	as *s.AuthService,
) {
	userGroup := r.Group("/user").Use(m.AuthMiddleware(as))
	{
		userGroup.GET("/me", uh.Me)
		userGroup.PUT("/me", uh.Update)
	}
}
