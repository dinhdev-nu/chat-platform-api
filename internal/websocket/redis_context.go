package websocket

import (
	"context"
	"time"
)

const redisOperationTimeout = 3 * time.Second

func (h *Hub) redisContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(h.ctx, redisOperationTimeout)
}
