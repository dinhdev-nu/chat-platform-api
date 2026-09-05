package cache

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type RoomCache struct{ client *redis.Client }

func NewRoomCache(client *redis.Client) *RoomCache { return &RoomCache{client: client} }

const (
	memberCacheKey = "conv:members:%s" // conv:members:{convID} -> set of memberIDs
	memberCacheTTL = 5 * time.Minute
)

func UnreadKey(userID, convID []byte) string {
	return fmt.Sprintf("unread:%s:%s",
		hex.EncodeToString(userID),
		hex.EncodeToString(convID),
	)
}

func (c *RoomCache) BatchIncrUnread(ctx context.Context, userIDs [][]byte, convID []byte) error {
	pipe := c.client.Pipeline()
	for _, mID := range userIDs {
		pipe.Incr(ctx, UnreadKey(mID, convID))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RoomCache) IncrUnread(ctx context.Context, userID, convID []byte) error {
	return c.client.Incr(ctx, UnreadKey(userID, convID)).Err()
}

func (c *RoomCache) GetMembers(ctx context.Context, convID []byte) ([][]byte, error) {
	key := memberKey(convID)
	members, err := c.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil // cache MISS
	}
	// Convert hex strings back to byte slices
	result := make([][]byte, 0, len(members))
	for _, m := range members {
		b, err := hex.DecodeString(m)
		if err != nil {
			continue
		}
		result = append(result, b)
	}
	return result, nil
}

func (c *RoomCache) WarmMember(ctx context.Context, convID []byte, userIDs [][]byte) error {
	if len(userIDs) == 0 {
		return nil
	}

	k := memberKey(convID)
	pipe := c.client.Pipeline()
	for _, mID := range userIDs {
		pipe.SAdd(ctx, k, hex.EncodeToString(mID))
	}
	pipe.Expire(ctx, k, memberCacheTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RoomCache) RefreshTTL(ctx context.Context, convID []byte) error {
	key := memberKey(convID)
	return c.client.Expire(ctx, key, memberCacheTTL).Err()
}

func (c *RoomCache) InvalidateMembers(ctx context.Context, convID []byte) error {
	return c.client.Del(ctx, memberKey(convID)).Err()
}

var addMemberScript = redis.NewScript(`
    local key = KEYS[1]
    local member = ARGV[1]
    local ttl = tonumber(ARGV[2])
    if redis.call("EXISTS", key) == 1 then
        redis.call("SADD", key, member)
        redis.call("EXPIRE", key, ttl)
        return 1
    end
    return 0
`)

var removeMemberScript = redis.NewScript(`
    local key = KEYS[1]
    local member = ARGV[1]
    local ttl = tonumber(ARGV[2])
    if redis.call("EXISTS", key) == 1 then
        redis.call("SREM", key, member)
        redis.call("EXPIRE", key, ttl)
        return 1
    end
    return 0
`)

func (c *RoomCache) AddMember(ctx context.Context, convID, userID []byte) error {
	return addMemberScript.Run(ctx, c.client,
		[]string{memberKey(convID)},
		hex.EncodeToString(userID),
		int(memberCacheTTL.Seconds()),
	).Err()
}

func (c *RoomCache) RemoveMember(ctx context.Context, convID, userID []byte) error {
	return removeMemberScript.Run(ctx, c.client,
		[]string{memberKey(convID)},
		hex.EncodeToString(userID),
		int(memberCacheTTL.Seconds()),
	).Err()
}

func (c *RoomCache) SetUnread(ctx context.Context, userID, convID []byte, count int64) error {
	key := UnreadKey(userID, convID)
	return c.client.Set(ctx, key, count, 0).Err()
}

func (c *RoomCache) DeleteUnread(ctx context.Context, userID, convID []byte) error {
	key := UnreadKey(userID, convID)
	return c.client.Del(ctx, key).Err()
}

func (c *RoomCache) GetUnreads(ctx context.Context, userID []byte, convIDs [][]byte) (map[string]int64, error) {
	if len(convIDs) == 0 {
		return map[string]int64{}, nil
	}

	keys := make([]string, len(convIDs))
	cidHexes := make([]string, len(convIDs))
	for i, cid := range convIDs {
		h := hex.EncodeToString(cid)
		cidHexes[i] = h
		keys[i] = UnreadKey(userID, cid)
	}
	vals, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(convIDs))
	for i, v := range vals {
		if v != nil {
			if s, ok := v.(string); ok {
				n, err := strconv.ParseInt(s, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("parse unread count for conversation %s: %w", cidHexes[i], err)
				}
				result[cidHexes[i]] = n
			}
		}
		// Nếu v == nil -> MISS -> Fallback DB
	}
	return result, nil
}

func (c *RoomCache) ResetUnread(ctx context.Context, userID []byte, convID []byte) error {
	return c.client.Set(ctx, UnreadKey(userID, convID), 0, 0).Err()
}

func (c *RoomCache) IsMember(ctx context.Context, convID, userID []byte) (isMember bool, cacheHit bool, err error) {
	key := memberKey(convID)
	uidHex := hex.EncodeToString(userID)

	// Pipeline EXISTS + SISMEMBER — 1 round-trip duy nhất.
	pipe := c.client.Pipeline()
	existsCmd := pipe.Exists(ctx, key)
	ismemberCmd := pipe.SIsMember(ctx, key, uidHex)
	if _, err = pipe.Exec(ctx); err != nil {
		return false, false, err
	}

	if existsCmd.Val() == 0 {
		return false, false, nil // cache MISS — key chưa warm
	}
	return ismemberCmd.Val(), true, nil // cache HIT — authoritative
}

func memberKey(convID []byte) string {
	return fmt.Sprintf(memberCacheKey, hex.EncodeToString(convID))
}
