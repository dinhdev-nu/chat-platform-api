package cache

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

type RoomCache struct {
	client *redis.Client
}

func NewRoomCache(client *redis.Client) *RoomCache {
	if client == nil {
		return nil
	}
	return &RoomCache{client: client}
}

func (c *RoomCache) BatchIncrUnread(ctx context.Context, userIDs [][]byte, convID []byte) error {
	pipe := c.client.Pipeline()
	for _, userID := range userIDs {
		pipe.Incr(ctx, UnreadKey(userID, convID))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RoomCache) SetUnread(ctx context.Context, userID, convID []byte, count int64) error {
	return c.client.Set(ctx, UnreadKey(userID, convID), count, 0).Err()
}

func (c *RoomCache) DeleteUnread(ctx context.Context, userID, convID []byte) error {
	return c.client.Del(ctx, UnreadKey(userID, convID)).Err()
}

func (c *RoomCache) GetUnreads(ctx context.Context, userID []byte, convIDs [][]byte) (map[string]int64, error) {
	if len(convIDs) == 0 {
		return map[string]int64{}, nil
	}

	keys := make([]string, len(convIDs))
	cidHexes := make([]string, len(convIDs))
	for i, convID := range convIDs {
		convHex := hex.EncodeToString(convID)
		cidHexes[i] = convHex
		keys[i] = UnreadKey(userID, convID)
	}

	vals, err := c.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	result := make(map[string]int64, len(convIDs))
	for i, value := range vals {
		if value == nil {
			continue
		}
		s, ok := value.(string)
		if !ok {
			continue
		}
		count, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse unread count for conversation %s: %w", cidHexes[i], err)
		}
		result[cidHexes[i]] = count
	}
	return result, nil
}

func (c *RoomCache) GetMembers(ctx context.Context, convID []byte) ([][]byte, error) {
	members, err := c.client.SMembers(ctx, memberKey(convID)).Result()
	if err != nil {
		return nil, err
	}
	if len(members) == 0 {
		return nil, nil
	}

	result := make([][]byte, 0, len(members))
	for _, member := range members {
		id, err := hex.DecodeString(member)
		if err != nil {
			continue
		}
		result = append(result, id)
	}
	return result, nil
}

func (c *RoomCache) WarmMember(ctx context.Context, convID []byte, userIDs [][]byte) error {
	if len(userIDs) == 0 {
		return nil
	}

	key := memberKey(convID)
	pipe := c.client.Pipeline()
	for _, userID := range userIDs {
		pipe.SAdd(ctx, key, hex.EncodeToString(userID))
	}
	pipe.Expire(ctx, key, memberCacheTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RoomCache) RefreshTTL(ctx context.Context, convID []byte) error {
	return c.client.Expire(ctx, memberKey(convID), memberCacheTTL).Err()
}

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

func (c *RoomCache) InvalidateMembers(ctx context.Context, convID []byte) error {
	return c.client.Del(ctx, memberKey(convID)).Err()
}

func (c *RoomCache) IsMember(ctx context.Context, convID, userID []byte) (bool, bool, error) {
	key := memberKey(convID)
	uidHex := hex.EncodeToString(userID)

	pipe := c.client.Pipeline()
	existsCmd := pipe.Exists(ctx, key)
	isMemberCmd := pipe.SIsMember(ctx, key, uidHex)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, false, err
	}

	if existsCmd.Val() == 0 {
		return false, false, nil
	}
	return isMemberCmd.Val(), true, nil
}
