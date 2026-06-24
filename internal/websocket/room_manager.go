package websocket

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	viewingTTL        = 150 * time.Second
	viewingKey        = "viewing:%s:%s"         // user ID, conversation ID
	viewingSessionKey = "viewing:session:%s:%s" // user ID, connection ID
)

type RoomManager struct {
	mu sync.RWMutex
	// viewing[userHex][convHex][connID] = active viewing session
	viewing map[string]map[string]map[string]struct{}
	rdb     *goredis.Client
}

func NewRoomManager(rdb *goredis.Client) *RoomManager {
	return &RoomManager{
		viewing: make(map[string]map[string]map[string]struct{}),
		rdb:     rdb,
	}
}

func (rm *RoomManager) IsViewing(userID, convID []byte) bool {
	uidHex := hex.EncodeToString(userID)
	cidHex := hex.EncodeToString(convID)

	if rm.isViewingLocal(uidHex, cidHex) {
		return true
	}

	if rm.rdb != nil {
		viewing, err := rm.isViewingRedis(uidHex, cidHex)
		if err == nil {
			return viewing
		}
	}

	return false
}

func (rm *RoomManager) isViewingLocal(uidHex, cidHex string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	convs, ok := rm.viewing[uidHex]
	if !ok {
		return false
	}
	sessions, viewing := convs[cidHex]
	return viewing && len(sessions) > 0
}

func (rm *RoomManager) SetViewing(convID, userID []byte, connID string) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()

	if _, ok := rm.viewing[uidHex]; !ok {
		rm.viewing[uidHex] = make(map[string]map[string]struct{})
	}
	if _, ok := rm.viewing[uidHex][cidHex]; !ok {
		rm.viewing[uidHex][cidHex] = make(map[string]struct{})
	}
	rm.viewing[uidHex][cidHex][connID] = struct{}{}
	rm.mu.Unlock()

	if rm.rdb != nil {
		_ = rm.setViewingRedis(uidHex, cidHex, connID)
	}
}

func (rm *RoomManager) ClearViewing(convID, userID []byte, connID string) {
	cidHex := hex.EncodeToString(convID)
	uidHex := hex.EncodeToString(userID)

	rm.mu.Lock()

	if convs, ok := rm.viewing[uidHex]; ok {
		if sessions, ok2 := convs[cidHex]; ok2 {
			delete(sessions, connID)
			if len(sessions) == 0 {
				delete(convs, cidHex)
			}
		}
		if len(convs) == 0 {
			delete(rm.viewing, uidHex)
		}
	}
	rm.mu.Unlock()

	if rm.rdb != nil {
		_ = rm.clearViewingRedis(uidHex, cidHex, connID)
	}
}

func (rm *RoomManager) ClearSession(userID []byte, connID string) {
	uidHex := hex.EncodeToString(userID)
	convHints := make([]string, 0)

	rm.mu.Lock()

	convs, ok := rm.viewing[uidHex]
	if !ok {
		rm.mu.Unlock()
		if rm.rdb != nil {
			_ = rm.clearSessionRedis(uidHex, connID, convHints)
		}
		return
	}
	for cidHex, sessions := range convs {
		convHints = append(convHints, cidHex)
		delete(sessions, connID)
		if len(sessions) == 0 {
			delete(convs, cidHex)
		}
	}
	if len(convs) == 0 {
		delete(rm.viewing, uidHex)
	}
	rm.mu.Unlock()

	if rm.rdb != nil {
		_ = rm.clearSessionRedis(uidHex, connID, convHints)
	}
}

func (rm *RoomManager) ClearAll(userID []byte) {
	uidHex := hex.EncodeToString(userID)
	rm.mu.Lock()
	defer rm.mu.Unlock()
	delete(rm.viewing, uidHex)
}

func (rm *RoomManager) HeartbeatSession(userID []byte, connID string) {
	if rm.rdb == nil {
		return
	}
	uidHex := hex.EncodeToString(userID)
	_ = rm.heartbeatSessionRedis(uidHex, connID)
}

func (rm *RoomManager) isViewingRedis(uidHex, cidHex string) (bool, error) {
	ctx, cancel := roomRedisContext()
	defer cancel()

	key := fmt.Sprintf(viewingKey, uidHex, cidHex)
	pipe := rm.rdb.Pipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", time.Now().UnixMilli()))
	cardCmd := pipe.ZCard(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, err
	}
	return cardCmd.Val() > 0, nil
}

func (rm *RoomManager) setViewingRedis(uidHex, cidHex, connID string) error {
	ctx, cancel := roomRedisContext()
	defer cancel()

	expiresAtMs := time.Now().Add(viewingTTL).UnixMilli()
	viewKey := fmt.Sprintf(viewingKey, uidHex, cidHex)
	sessionKey := fmt.Sprintf(viewingSessionKey, uidHex, connID)

	pipe := rm.rdb.Pipeline()
	pipe.ZAdd(ctx, viewKey, goredis.Z{Score: float64(expiresAtMs), Member: connID})
	pipe.Expire(ctx, viewKey, viewingTTL)
	pipe.SAdd(ctx, sessionKey, cidHex)
	pipe.Expire(ctx, sessionKey, viewingTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (rm *RoomManager) clearViewingRedis(uidHex, cidHex, connID string) error {
	ctx, cancel := roomRedisContext()
	defer cancel()

	nowMs := fmt.Sprintf("%d", time.Now().UnixMilli())
	pipe := rm.rdb.Pipeline()
	pipe.ZRem(ctx, fmt.Sprintf(viewingKey, uidHex, cidHex), connID)
	pipe.ZRemRangeByScore(ctx, fmt.Sprintf(viewingKey, uidHex, cidHex), "-inf", nowMs)
	pipe.SRem(ctx, fmt.Sprintf(viewingSessionKey, uidHex, connID), cidHex)
	_, err := pipe.Exec(ctx)
	return err
}

func (rm *RoomManager) clearSessionRedis(uidHex, connID string, convHints []string) error {
	ctx, cancel := roomRedisContext()
	defer cancel()

	sessionKey := fmt.Sprintf(viewingSessionKey, uidHex, connID)
	convSet := make(map[string]struct{}, len(convHints))
	for _, cidHex := range convHints {
		convSet[cidHex] = struct{}{}
	}

	convIDs, err := rm.rdb.SMembers(ctx, sessionKey).Result()
	if err != nil && len(convSet) == 0 {
		return err
	}
	for _, cidHex := range convIDs {
		convSet[cidHex] = struct{}{}
	}

	nowMs := fmt.Sprintf("%d", time.Now().UnixMilli())
	pipe := rm.rdb.Pipeline()
	for cidHex := range convSet {
		key := fmt.Sprintf(viewingKey, uidHex, cidHex)
		pipe.ZRem(ctx, key, connID)
		pipe.ZRemRangeByScore(ctx, key, "-inf", nowMs)
	}
	pipe.Del(ctx, sessionKey)
	_, err = pipe.Exec(ctx)
	return err
}

func (rm *RoomManager) heartbeatSessionRedis(uidHex, connID string) error {
	ctx, cancel := roomRedisContext()
	defer cancel()

	sessionKey := fmt.Sprintf(viewingSessionKey, uidHex, connID)
	convIDs, err := rm.rdb.SMembers(ctx, sessionKey).Result()
	if err != nil || len(convIDs) == 0 {
		return err
	}

	expiresAtMs := time.Now().Add(viewingTTL).UnixMilli()
	pipe := rm.rdb.Pipeline()
	pipe.Expire(ctx, sessionKey, viewingTTL)
	for _, cidHex := range convIDs {
		key := fmt.Sprintf(viewingKey, uidHex, cidHex)
		pipe.ZAdd(ctx, key, goredis.Z{Score: float64(expiresAtMs), Member: connID})
		pipe.Expire(ctx, key, viewingTTL)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func roomRedisContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), redisOperationTimeout)
}
