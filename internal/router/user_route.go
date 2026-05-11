package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterUserRoutes(
	r *gin.RouterGroup,
	rp *gin.RouterGroup,
	uh *h.UserHandler,
) {
	userGroup := rp.Group("/user")
	{
		userGroup.GET("/me", uh.Me)
		userGroup.PUT("/me", uh.Update)
	}
}
