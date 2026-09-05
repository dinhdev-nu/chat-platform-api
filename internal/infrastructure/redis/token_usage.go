package redis

import (
	"context"
	"encoding/hex"
	"time"

	"github.com/redis/go-redis/v9"
)

type TokenUsageStore struct{ client *redis.Client }

func NewTokenUsageStore(client *redis.Client) *TokenUsageStore {
	return &TokenUsageStore{client: client}
}
func (s *TokenUsageStore) ShouldRecord(ctx context.Context, jti []byte) (bool, error) {
	return s.client.SetNX(ctx, "token:last_used:"+hex.EncodeToString(jti), 1, 10*time.Minute).Result()
}
