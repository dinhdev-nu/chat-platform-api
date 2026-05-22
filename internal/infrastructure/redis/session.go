package redis

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	sessionKey = "session:%s" // s = jti

	cacheUser          = "user:%s" // s = userID
	cacheUserStatusTTL = 10 * time.Minute
)

type SessionStore struct {
	client *redis.Client
}

func NewSessionStore(client *redis.Client) *SessionStore {
	return &SessionStore{client: client}
}

func (s *SessionStore) Set(ctx context.Context, jti, userID []byte, ttl time.Duration) error {
	key := fmt.Sprintf(sessionKey, hex.EncodeToString(jti))
	val := hex.EncodeToString(userID)

	return s.client.Set(ctx, key, val, ttl).Err()
}

func (s *SessionStore) Get(ctx context.Context, jti []byte) (string, error) {
	key := fmt.Sprintf(sessionKey, hex.EncodeToString(jti))
	val, err := s.client.Get(ctx, key).Result()

	if err != nil {
		if err == redis.Nil {
			return "", nil // Session không tồn tại
		}
	}
	return val, err
}

func (s *SessionStore) Revoke(ctx context.Context, jti []byte) error {
	key := fmt.Sprintf(sessionKey, hex.EncodeToString(jti))
	return s.client.Del(ctx, key).Err()
}

func (s *SessionStore) WarmUser(ctx context.Context, userID []byte, payload string) error {
	key := fmt.Sprintf(cacheUser, hex.EncodeToString(userID))
	return s.client.Set(ctx, key, payload, cacheUserStatusTTL).Err()
}

func (s *SessionStore) GetUser(ctx context.Context, userID []byte) (string, error) {
	key := fmt.Sprintf(cacheUser, hex.EncodeToString(userID))
	val, err := s.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil // Cache miss
		}
		return "", err
	}
	return val, nil
}

func (s *SessionStore) RevokeUser(ctx context.Context, userID []byte) error {
	key := fmt.Sprintf(cacheUser, hex.EncodeToString(userID))
	return s.client.Del(ctx, key).Err()
}
