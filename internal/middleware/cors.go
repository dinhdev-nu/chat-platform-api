package middleware

import (
	"slices"
	"strconv"
	"strings"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	allowedCredentials := "false"
	if g.Config.Cors.AllowCredentials {
		allowedCredentials = "true"
	}
	maxAge := "3600"
	if g.Config.Cors.CorsMaxAge > 0 {
		maxAge = strconv.Itoa(g.Config.Cors.CorsMaxAge)
	}

	return func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")

		if slices.Contains(g.Config.Cors.AllowedOrigins, origin) {
			c.Header("Access-Control-Allow-Origin", origin)
		}

		c.Header("Access-Control-Allow-Methods", strings.Join(g.Config.Cors.AllowedMethods, ", "))
		c.Header("Access-Control-Allow-Headers", strings.Join(g.Config.Cors.AllowedHeaders, ", "))
		c.Header("Access-Control-Expose-Headers", strings.Join(g.Config.Cors.ExposeHeaders, ", "))
		c.Header("Access-Control-Allow-Credentials", allowedCredentials)
		c.Header("Access-Control-Max-Age", maxAge) // 12 giờ

		// Nếu là preflight request, trả về 204 No Content
		if c.Request.Method == "OPTIONS" {
			r.NoContent(c)
			return
		}

		c.Next()
	}
}
