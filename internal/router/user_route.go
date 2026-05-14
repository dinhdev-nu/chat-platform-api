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
	user := rp.Group("/users")
	{
		user.GET("/me", uh.Me)
		user.PUT("/me", uh.Update)
		user.GET("/search", uh.Search)
	}

	requests := rp.Group("/contacts/requests")
	{
		requests.POST("", uh.SendContactRequest)
		requests.GET("/incoming", uh.GetIncomingRequests)
		requests.PUT("/accept", uh.AcceptContactRequest)
	}

	contacts := rp.Group("/contacts")
	{
		contacts.GET("", uh.GetContacts)
	}
}
