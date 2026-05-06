package redis

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	presenceKey string        = "presence:%s" // s = user ID
	presenceTTL time.Duration = 5 * time.Minute
)

type PresenceStore struct {
	client *redis.Client
}

func NewPresenceStore(client *redis.Client) *PresenceStore {
	return &PresenceStore{client: client}
}

func (p *PresenceStore) SetOnline(ctx context.Context, userID []byte) error {
	key := fmt.Sprintf(presenceKey, hex.EncodeToString(userID))
	return p.client.Set(ctx, key, 1, presenceTTL).Err()
}

func (p *PresenceStore) SetOffline(ctx context.Context, userID []byte) error {
	key := fmt.Sprintf(presenceKey, hex.EncodeToString(userID))
	return p.client.Del(ctx, key).Err()
}

func (p *PresenceStore) IsOnline(ctx context.Context, userID []byte) (bool, error) {
	key := fmt.Sprintf(presenceKey, hex.EncodeToString(userID))
	exists, err := p.client.Exists(ctx, key).Result()
	return exists > 0, err
}

func (p *PresenceStore) Heartbeat(ctx context.Context, userID []byte) error {
	key := fmt.Sprintf(presenceKey, hex.EncodeToString(userID))
	return p.client.Expire(ctx, key, presenceTTL).Err()
}

func (p *PresenceStore) BulkIsOnline(ctx context.Context, userIDs [][]byte) (map[string]bool, error) {
	pipe := p.client.Pipeline()
	cmds := make(map[string]*redis.IntCmd)

	for _, id := range userIDs {
		hexID := hex.EncodeToString(id)
		key := fmt.Sprintf(presenceKey, hexID)
		cmds[hexID] = pipe.Exists(ctx, key)
	}

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	result := make(map[string]bool, len(cmds))
	for hexID, cmd := range cmds {
		result[hexID] = cmd.Val() > 0
	}

	return result, nil
}
