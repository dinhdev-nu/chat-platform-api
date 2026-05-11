package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	"github.com/gin-gonic/gin"
)

func RegisterMessageRouters(
	r *gin.RouterGroup,
	rp *gin.RouterGroup,
	mh *h.MessageHandler,
) {

	// Protected routes
	conv := rp.Group("/conversations/:id")
	{
		conv.POST("/messages", mh.SendMessage)
		conv.GET("/messages", mh.ListMessages)
		conv.POST("/read", mh.MarkAsRead)
	}
	msgs := rp.Group("/messages")
	{
		msgs.PUT("/:id", mh.EditMessage)               // sửa tin nhắn (24h)
		msgs.DELETE("/:id", mh.DeleteMessage)          // xóa mềm
		msgs.POST("/:id/reactions", mh.ToggleReaction) // t
	}

}
