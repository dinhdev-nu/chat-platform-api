package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterAuthRoutes(
	r *gin.Engine,
	as *s.AuthService,
	ah *h.AuthHandler,
) {
	authGroup := r.Group("/auth")
	{
		authGroup.POST("/send-otp", ah.SendOTP)
		authGroup.POST("/verify-otp", ah.VerifyOTP)

		protected := authGroup.Group("").Use(m.AuthMiddleware(as))
		{
			protected.POST("/logout", ah.Logout)
		}
	}

}
