package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterRoomRouters(
	r *gin.Engine,
	rh *h.RoomHandler,
	as *s.AuthService,
) {
	r.Use(m.AuthMiddleware(as))

	conv := r.Group("/conversations")
	{
		conv.POST("/direct", rh.CreateDM)
		conv.POST("/group", rh.CreateGroup)
		conv.GET("", rh.ListConversations)
		conv.POST("/:id/members", rh.AddMember)
		conv.DELETE("/:id/members/:user_id", rh.RemoveMember)
	}
}
