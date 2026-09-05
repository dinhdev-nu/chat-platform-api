package redis

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

type maxSequenceReader interface {
	GetMaxSeq(context.Context, []byte) (int64, error)
}
type SequenceStore struct {
	client   *redis.Client
	messages maxSequenceReader
}

func NewSequenceStore(client *redis.Client, messages maxSequenceReader) *SequenceStore {
	return &SequenceStore{client: client, messages: messages}
}
func (s *SequenceStore) Next(ctx context.Context, convID []byte) (uint64, error) {
	convHex := strings.ToLower(hex.EncodeToString(convID))
	seqKey := fmt.Sprintf("seq:%s", convHex)
	lookKey := fmt.Sprintf("look:seq_init:%s", convHex)

	exists, err := s.client.Exists(ctx, seqKey).Result()
	if err != nil {
		return 0, err
	}
	if exists == 0 {
		// Key ko tồn tại, khởi tạo từ DB
		acquired, err := s.client.SetNX(ctx, lookKey, 1, 5*time.Second).Result()
		if err != nil {
			return 0, err
		}

		if acquired {
			defer s.client.Del(ctx, lookKey) // Release lock sau khi khởi tạo xong

			maxSeq, err := s.messages.GetMaxSeq(ctx, convID)
			if err != nil {
				return 0, err
			}

			if err := s.client.Set(ctx, seqKey, maxSeq, 0).Err(); err != nil {
				return 0, err
			}
		} else {
			for i := 0; i < 10; i++ { // Wait for initialization with ten fixed-delay attempts
				time.Sleep(10 * time.Millisecond)
				ex, err := s.client.Exists(ctx, seqKey).Result()
				if err != nil {
					return 0, err
				}
				if ex > 0 {
					break
				}
			}
		}
	}

	cmd := s.client.Incr(ctx, seqKey)
	val, err := cmd.Result()
	if err != nil {
		return 0, err
	}
	if val < 0 {
		return 0, fmt.Errorf("invalid negative message sequence: %d", val)
	}
	return cmd.Uint64()
}
