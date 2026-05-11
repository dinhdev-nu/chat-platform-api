package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterRoomRouters(
	r *gin.RouterGroup,
	rp *gin.RouterGroup,
	rh *h.RoomHandler,
) {

	conv := rp.Group("/conversations")
	{
		conv.POST("/direct", rh.CreateDM)
		conv.POST("/group", rh.CreateGroup)
		conv.GET("", rh.ListConversations)
		conv.POST("/:id/members", rh.AddMember)
		conv.DELETE("/:id/members/:user_id", rh.RemoveMember)
	}
}
