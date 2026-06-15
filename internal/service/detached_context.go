package service

import (
	"context"
	"time"
)

const (
	cacheTaskTimeout     = 3 * time.Second
	streamEnqueueTimeout = 3 * time.Second
	sideEffectTimeout    = 5 * time.Second
	systemMessageTimeout = 10 * time.Second
)

// detachedContext preserves context values while isolating work from request cancellation.
func detachedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}
