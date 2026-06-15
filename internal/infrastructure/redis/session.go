package redis

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
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
		if errors.Is(err, redis.Nil) {
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
		if errors.Is(err, redis.Nil) {
			return "", nil // Cache miss
		}
		return "", err
	}
	return val, nil
}

func (s *SessionStore) GetUsers(ctx context.Context, userIDs [][]byte) (map[string]*model.User, error) {
	users, _, err := s.GetUsersWithMisses(ctx, userIDs)
	return users, err
}

func (s *SessionStore) GetUsersWithMisses(ctx context.Context, userIDs [][]byte) (map[string]*model.User, [][]byte, error) {
	users := make(map[string]*model.User, len(userIDs))
	if len(userIDs) == 0 {
		return users, nil, nil
	}

	// Deduplicate — caller có thể đã dedup, nhưng defensive check vẫn cần
	seen := make(map[string]struct{}, len(userIDs))
	uniqueIDs := make([][]byte, 0, len(userIDs))
	for _, id := range userIDs {
		if len(id) == 0 {
			continue
		}
		k := string(id)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return users, nil, nil
	}

	keys := make([]string, len(uniqueIDs))
	for i, id := range uniqueIDs {
		keys[i] = fmt.Sprintf(cacheUser, hex.EncodeToString(id))
	}

	results, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, nil, err
	}

	// Capacity hint: worst case tất cả đều miss
	misses := make([][]byte, 0, len(uniqueIDs))
	for i, res := range results {
		if res == nil {
			misses = append(misses, uniqueIDs[i])
			continue
		}

		payload, ok := res.(string)
		if !ok {
			misses = append(misses, uniqueIDs[i])
			continue
		}

		var user model.User
		if err := json.Unmarshal([]byte(payload), &user); err != nil {
			misses = append(misses, uniqueIDs[i])
			continue
		}
		if len(user.ID) == 0 {
			user.ID = uniqueIDs[i]
		}
		users[string(uniqueIDs[i])] = &user
	}

	return users, misses, nil
}

func (s *SessionStore) WarmUsers(ctx context.Context, users map[string]*model.User) error {
	if len(users) == 0 {
		return nil
	}

	pipe := s.client.Pipeline()
	hasCommand := false

	for _, user := range users {
		if user == nil || len(user.ID) == 0 {
			continue
		}
		payload, err := json.Marshal(user)
		if err != nil {
			continue
		}
		key := fmt.Sprintf(cacheUser, hex.EncodeToString(user.ID))
		pipe.Set(ctx, key, payload, cacheUserStatusTTL)
		hasCommand = true
	}

	if !hasCommand {
		return nil
	}

	_, err := pipe.Exec(ctx)
	return err
}

func (s *SessionStore) RevokeUser(ctx context.Context, userID []byte) error {
	key := fmt.Sprintf(cacheUser, hex.EncodeToString(userID))
	return s.client.Del(ctx, key).Err()
}
