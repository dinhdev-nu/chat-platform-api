package cache

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/redis/go-redis/v9"
)

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

func BatchIncrUnread(ctx context.Context, userIDs [][]byte, convID []byte) error {
	pipe := g.RedisClient.Pipeline()
	for _, mID := range userIDs {
		pipe.Incr(ctx, UnreadKey(mID, convID))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func IncrUnread(ctx context.Context, userID, convID []byte) error {
	return g.RedisClient.Incr(ctx, UnreadKey(userID, convID)).Err()
}

func GetMembers(ctx context.Context, convID []byte) ([][]byte, error) {
	key := memberKey(convID)
	members, err := g.RedisClient.SMembers(ctx, key).Result()
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

func WarmMember(ctx context.Context, convID []byte, userIDs [][]byte) error {
	if len(userIDs) == 0 {
		return nil
	}

	k := memberKey(convID)
	pipe := g.RedisClient.Pipeline()
	for _, mID := range userIDs {
		pipe.SAdd(ctx, k, hex.EncodeToString(mID))
	}
	pipe.Expire(ctx, k, memberCacheTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func RefreshTTL(ctx context.Context, convID []byte) error {
	key := memberKey(convID)
	return g.RedisClient.Expire(ctx, key, memberCacheTTL).Err()
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

func AddMember(ctx context.Context, convID, userID []byte) error {
	return addMemberScript.Run(ctx, g.RedisClient,
		[]string{memberKey(convID)},
		hex.EncodeToString(userID),
		int(memberCacheTTL.Seconds()),
	).Err()
}

func RemoveMember(ctx context.Context, convID, userID []byte) error {
	return removeMemberScript.Run(ctx, g.RedisClient,
		[]string{memberKey(convID)},
		hex.EncodeToString(userID),
		int(memberCacheTTL.Seconds()),
	).Err()
}

func SetUnread(ctx context.Context, userID, convID []byte, count int64) error {
	key := UnreadKey(userID, convID)
	return g.RedisClient.Set(ctx, key, count, 0).Err()
}

func DeleteUnread(ctx context.Context, userID, convID []byte) error {
	key := UnreadKey(userID, convID)
	return g.RedisClient.Del(ctx, key).Err()
}

func GetUnreads(ctx context.Context, userID []byte, convIDs [][]byte) (map[string]int64, error) {
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
	vals, err := g.RedisClient.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64, len(convIDs))
	for i, v := range vals {
		if v != nil {
			if s, ok := v.(string); ok {
				var n int64
				fmt.Sscanf(s, "%d", &n)
				result[cidHexes[i]] = n
			}
		}
		// Nếu v == nil -> MISS -> Fallback DB
	}
	return result, nil
}

func ResetUnread(ctx context.Context, userID []byte, convID []byte) error {
	return g.RedisClient.Set(ctx, UnreadKey(userID, convID), 0, 0).Err()
}

func IsMember(ctx context.Context, convID, userID []byte) (isMember bool, cacheHit bool, err error) {
	key := memberKey(convID)
	uidHex := hex.EncodeToString(userID)

	// Pipeline EXISTS + SISMEMBER — 1 round-trip duy nhất.
	pipe := g.RedisClient.Pipeline()
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
