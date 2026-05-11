package middleware

import (
	"strings"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"github.com/gin-gonic/gin"
)

const (
	Authorization string = "Authorization"
	TokenQuery    string = "token"

	ContextUserKey string = "user"
	ContextJTIKey  string = "jti"
)

func AuthMiddleware(as *s.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractTokenFromHeader(c)
		if token == "" {
			_ = c.Error(ar.New(ar.ErrTokenMissing, "Authorization token required"))
			c.Abort()
			return
		}

		user, jti, err := as.ValidateToken(c.Request.Context(), token)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		c.Set(ContextUserKey, user)
		c.Set(ContextJTIKey, jti)

		c.Next()
	}
}

func extractTokenFromHeader(c *gin.Context) string {
	token := c.GetHeader(Authorization)
	if strings.HasPrefix(token, "Bearer ") {
		return strings.TrimPrefix(token, "Bearer ")
	}

	return c.Query(TokenQuery)
}

func GetCurrentUser(c *gin.Context) (*model.User, bool) {
	val, exists := c.Get(ContextUserKey)
	if !exists {
		return nil, false
	}
	user, ok := val.(*model.User)
	return user, ok
}

func GetCurrentJTI(c *gin.Context) ([]byte, bool) {
	val, exists := c.Get(ContextJTIKey)
	if !exists {
		return nil, false
	}
	jti, ok := val.([]byte)
	return jti, ok
}
