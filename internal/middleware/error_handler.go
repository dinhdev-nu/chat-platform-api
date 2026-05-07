package middleware

import (
	"github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func ErrorHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if len(c.Errors) == 0 {
			return
		}

		err := c.Errors.Last().Err

		if appErr, ok := errors.IsAppError(err); ok {
			if appErr.Err != nil {
				global.Logger.Error(
					"App error",
					zap.String("code", string(appErr.Code)),
					zap.String("message", appErr.Message), zap.Error(appErr),
				)
			}

			response.Error(c, appErr)
		}

		global.Logger.Error("Unknown error", zap.Error(err))
		response.Error(c, errors.Internal(err))
	}
}
