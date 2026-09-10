package cache

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type MembershipSource interface {
	GetConversationMemberIDs(context.Context, []byte) ([][]byte, error)
	WithMembershipLock(context.Context, []byte, func(repository.ConversationMembers) error) error
}

type RoomCache struct {
	client  *redis.Client
	members MembershipSource
	logger  *zap.Logger
}

func NewRoomCache(client *redis.Client, members MembershipSource, logger *zap.Logger) *RoomCache {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &RoomCache{client: client, members: members, logger: logger}
}

const (
	// A new namespace prevents legacy SADD warmers from affecting authorization.
	memberCacheKey             = "conv:members:v2:%s"
	memberCacheTTL             = 5 * time.Minute
	membershipOperationTimeout = 5 * time.Second
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

var readMembersScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], '_ready') ~= '1' then return {} end
return redis.call('HKEYS', KEYS[1])
`)

func (c *RoomCache) cachedMembers(ctx context.Context, convID []byte) ([][]byte, bool, error) {
	values, err := readMembersScript.Run(ctx, c.client, []string{memberKey(convID)}).StringSlice()
	if err != nil || len(values) == 0 {
		return nil, false, err
	}
	ids := make([][]byte, 0, len(values)-1)
	for _, value := range values {
		if value == "_ready" {
			continue
		}
		id, err := hex.DecodeString(value)
		if err != nil || len(id) != 16 {
			return nil, false, fmt.Errorf("invalid cached member ID")
		}
		ids = append(ids, id)
	}
	return ids, true, nil
}

func (c *RoomCache) GetMembers(ctx context.Context, convID []byte) ([][]byte, error) {
	ids, hit, err := c.cachedMembers(ctx, convID)
	if err != nil {
		return c.members.GetConversationMemberIDs(ctx, convID)
	}
	if hit {
		return ids, nil
	}
	return c.loadMembers(ctx, convID)
}

// A fill may finish after its DB connection/lock was lost. Its token prevents
// publishing that old snapshot after a membership write invalidated the key.
var storeMembersScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], '_loading') ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
redis.call('HSET', KEYS[1], '_ready', '1')
for i = 3, #ARGV do redis.call('HSET', KEYS[1], ARGV[i], '1') end
redis.call('EXPIRE', KEYS[1], ARGV[2])
return 1
`)

func (c *RoomCache) loadMembers(parent context.Context, convID []byte) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(parent, membershipOperationTimeout)
	defer cancel()
	var ids [][]byte
	err := c.members.WithMembershipLock(ctx, convID, func(source repository.ConversationMembers) error {
		cached, hit, cacheErr := c.cachedMembers(ctx, convID)
		if cacheErr == nil && hit {
			ids = cached
			return nil
		}
		key, token := memberKey(convID), uuid.NewString()
		if cacheErr == nil {
			_, cacheErr = c.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Del(ctx, key)
				pipe.HSet(ctx, key, "_loading", token)
				pipe.Expire(ctx, key, memberCacheTTL)
				return nil
			})
		}
		var err error
		ids, err = source.GetConversationMemberIDs(ctx, convID)
		if err != nil {
			return err
		}
		if cacheErr == nil {
			args := []any{token, int(memberCacheTTL.Seconds())}
			for _, id := range ids {
				args = append(args, hex.EncodeToString(id))
			}
			cacheErr = storeMembersScript.Run(ctx, c.client, []string{key}, args...).Err()
		}
		if cacheErr != nil {
			c.logger.Warn("failed to cache conversation members", zap.Error(cacheErr))
		}
		return nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ids, err
}

// Invalidate before persistence while holding the same lock as cache fills.
// If Redis cannot invalidate, do not change DB membership and leave stale grants.
func (c *RoomCache) UpdateMembership(parent context.Context, convID []byte, apply func(context.Context, repository.ConversationMembers) error) error {
	ctx, cancel := context.WithTimeout(parent, membershipOperationTimeout)
	defer cancel()
	return c.members.WithMembershipLock(ctx, convID, func(source repository.ConversationMembers) error {
		if err := c.client.Del(ctx, memberKey(convID)).Err(); err != nil {
			return fmt.Errorf("invalidate membership before update: %w", err)
		}
		return apply(ctx, source)
	})
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

var isMemberScript = redis.NewScript(`
if redis.call('HGET', KEYS[1], '_ready') ~= '1' then return -1 end
return redis.call('HEXISTS', KEYS[1], ARGV[1])
`)

func (c *RoomCache) IsMember(ctx context.Context, convID, userID []byte) (bool, error) {
	result, err := isMemberScript.Run(ctx, c.client, []string{memberKey(convID)}, hex.EncodeToString(userID)).Int()
	if err == nil && result >= 0 {
		return result == 1, nil
	}
	var ids [][]byte
	if err != nil {
		ids, err = c.members.GetConversationMemberIDs(ctx, convID)
	} else {
		ids, err = c.loadMembers(ctx, convID)
	}
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if string(id) == string(userID) {
			return true, nil
		}
	}
	return false, nil
}

func memberKey(convID []byte) string {
	return fmt.Sprintf(memberCacheKey, hex.EncodeToString(convID))
}
