package redis

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	presenceKey         string = "presence:%s"          // s = user ID
	presenceSessionsKey string = "presence:sessions:%s" // s = user ID
	// presenceTTL = 3 * pingPeriod (pingPeriod = 50s) => 150s
	presenceTTL time.Duration = 150 * time.Second
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
	sessionsKey := fmt.Sprintf(presenceSessionsKey, hex.EncodeToString(userID))
	return p.client.Del(ctx, key, sessionsKey).Err()
}

func (p *PresenceStore) SetSessionOnline(ctx context.Context, userID []byte, connID string) (bool, error) {
	uidHex := hex.EncodeToString(userID)
	nowMs := time.Now().UnixMilli()
	expiresAtMs := nowMs + presenceTTL.Milliseconds()

	transitioned, err := setPresenceSessionOnlineScript.Run(
		ctx,
		p.client,
		[]string{
			fmt.Sprintf(presenceSessionsKey, uidHex),
			fmt.Sprintf(presenceKey, uidHex),
		},
		connID,
		expiresAtMs,
		nowMs,
		int(presenceTTL.Seconds()),
	).Int()
	return transitioned == 1, err
}

func (p *PresenceStore) SetSessionOffline(ctx context.Context, userID []byte, connID string) (bool, error) {
	uidHex := hex.EncodeToString(userID)
	nowMs := time.Now().UnixMilli()

	transitioned, err := setPresenceSessionOfflineScript.Run(
		ctx,
		p.client,
		[]string{
			fmt.Sprintf(presenceSessionsKey, uidHex),
			fmt.Sprintf(presenceKey, uidHex),
		},
		connID,
		nowMs,
		int(presenceTTL.Seconds()),
	).Int()
	return transitioned == 1, err
}

func (p *PresenceStore) HeartbeatSession(ctx context.Context, userID []byte, connID string) error {
	uidHex := hex.EncodeToString(userID)
	nowMs := time.Now().UnixMilli()
	expiresAtMs := nowMs + presenceTTL.Milliseconds()

	return heartbeatPresenceSessionScript.Run(
		ctx,
		p.client,
		[]string{
			fmt.Sprintf(presenceSessionsKey, uidHex),
			fmt.Sprintf(presenceKey, uidHex),
		},
		connID,
		expiresAtMs,
		nowMs,
		int(presenceTTL.Seconds()),
	).Err()
}

func (p *PresenceStore) IsOnline(ctx context.Context, userID []byte) (bool, error) {
	uidHex := hex.EncodeToString(userID)
	nowMs := time.Now().UnixMilli()

	result, err := isPresenceOnlineScript.Run(
		ctx,
		p.client,
		[]string{
			fmt.Sprintf(presenceSessionsKey, uidHex),
			fmt.Sprintf(presenceKey, uidHex),
		},
		nowMs,
	).Int()
	return result == 1, err
}

func (p *PresenceStore) Heartbeat(ctx context.Context, userID []byte) error {
	key := fmt.Sprintf(presenceKey, hex.EncodeToString(userID))
	return p.client.Expire(ctx, key, presenceTTL).Err()
}

func (p *PresenceStore) BulkIsOnline(ctx context.Context, userIDs [][]byte) (map[string]bool, error) {
	pipe := p.client.Pipeline()
	nowMs := time.Now().UnixMilli()
	sessionCmds := make(map[string]*redis.IntCmd)
	legacyCmds := make(map[string]*redis.IntCmd)

	for _, id := range userIDs {
		hexID := hex.EncodeToString(id)
		sessionKey := fmt.Sprintf(presenceSessionsKey, hexID)
		pipe.ZRemRangeByScore(ctx, sessionKey, "-inf", fmt.Sprintf("%d", nowMs))
		sessionCmds[hexID] = pipe.ZCard(ctx, sessionKey)
		legacyCmds[hexID] = pipe.Exists(ctx, fmt.Sprintf(presenceKey, hexID))
	}

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	result := make(map[string]bool, len(sessionCmds))
	for hexID, cmd := range sessionCmds {
		result[hexID] = cmd.Val() > 0 || legacyCmds[hexID].Val() > 0
	}

	return result, nil
}

var setPresenceSessionOnlineScript = redis.NewScript(`
local sessions_key = KEYS[1]
local marker_key = KEYS[2]
local conn_id = ARGV[1]
local expires_at_ms = ARGV[2]
local now_ms = ARGV[3]
local ttl_seconds = tonumber(ARGV[4])

redis.call("ZREMRANGEBYSCORE", sessions_key, "-inf", now_ms)
local active_before = redis.call("ZCARD", sessions_key)
redis.call("ZADD", sessions_key, expires_at_ms, conn_id)
redis.call("EXPIRE", sessions_key, ttl_seconds)
redis.call("SET", marker_key, 1, "EX", ttl_seconds)

if active_before == 0 then
	return 1
end
return 0
`)

var setPresenceSessionOfflineScript = redis.NewScript(`
local sessions_key = KEYS[1]
local marker_key = KEYS[2]
local conn_id = ARGV[1]
local now_ms = ARGV[2]
local ttl_seconds = tonumber(ARGV[3])

redis.call("ZREMRANGEBYSCORE", sessions_key, "-inf", now_ms)
local active_before = redis.call("ZCARD", sessions_key)
redis.call("ZREM", sessions_key, conn_id)
local active_after = redis.call("ZCARD", sessions_key)

if active_after == 0 then
	redis.call("DEL", sessions_key, marker_key)
	if active_before > 0 then
		return 1
	end
	return 0
end

redis.call("EXPIRE", sessions_key, ttl_seconds)
redis.call("SET", marker_key, 1, "EX", ttl_seconds)
return 0
`)

var heartbeatPresenceSessionScript = redis.NewScript(`
local sessions_key = KEYS[1]
local marker_key = KEYS[2]
local conn_id = ARGV[1]
local expires_at_ms = ARGV[2]
local now_ms = ARGV[3]
local ttl_seconds = tonumber(ARGV[4])

redis.call("ZREMRANGEBYSCORE", sessions_key, "-inf", now_ms)
redis.call("ZADD", sessions_key, expires_at_ms, conn_id)
redis.call("EXPIRE", sessions_key, ttl_seconds)
redis.call("SET", marker_key, 1, "EX", ttl_seconds)
return 1
`)

var isPresenceOnlineScript = redis.NewScript(`
local sessions_key = KEYS[1]
local marker_key = KEYS[2]
local now_ms = ARGV[1]

redis.call("ZREMRANGEBYSCORE", sessions_key, "-inf", now_ms)
if redis.call("ZCARD", sessions_key) > 0 then
	return 1
end
if redis.call("EXISTS", marker_key) > 0 then
	return 1
end
return 0
`)
