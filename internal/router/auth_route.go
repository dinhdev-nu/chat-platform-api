package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterAuthRoutes(
	r *gin.RouterGroup,
	rp *gin.RouterGroup,
	ah *h.AuthHandler,
) {
	authGroup := r.Group("/auth")
	{
		authGroup.POST("/send-otp", ah.SendOTP)
		authGroup.POST("/verify-otp", ah.VerifyOTP)

	}
	authGroupProtected := rp.Group("/auth")
	{
		authGroupProtected.POST("/logout", ah.Logout)
	}

}
