package router

import (
	h "github.com/dinhdev-nu/chat-platform-api/internal/handler"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/gin-gonic/gin"
)

func RegisterMessageRouters(
	r *gin.Engine,
	mh *h.MessageHandler,
	as *s.AuthService,
) {
	r.Use(m.AuthMiddleware(as))
	conv := r.Group("/conversations/:id")
	{
		conv.POST("/messages", mh.SendMessage)
		conv.GET("/messages", mh.ListMessages)
		conv.POST("/read", mh.MarkAsRead)
	}
	msgs := r.Group("/messages")
	{
		msgs.PUT("/:id", mh.EditMessage)               // sửa tin nhắn (24h)
		msgs.DELETE("/:id", mh.DeleteMessage)          // xóa mềm
		msgs.POST("/:id/reactions", mh.ToggleReaction) // t
	}

}
