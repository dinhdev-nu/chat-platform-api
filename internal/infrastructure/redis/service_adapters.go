package redis

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/internal/service"
	goredis "github.com/redis/go-redis/v9"
)

type EventPublisher struct {
	client *goredis.Client
	broker *PubSubBroker
}

func NewEventPublisher(client *goredis.Client, broker *PubSubBroker) *EventPublisher {
	if client == nil && broker == nil {
		return nil
	}
	return &EventPublisher{client: client, broker: broker}
}

func (p *EventPublisher) PublishConversation(ctx context.Context, convID []byte, event service.Event) error {
	if p.broker == nil {
		return fmt.Errorf("redis pubsub broker unavailable")
	}
	return p.broker.Publish(ctx, convID, Event{
		Type:    EventType(event.Type),
		ConvID:  event.ConvID,
		Payload: event.Payload,
	})
}

func (p *EventPublisher) PublishUserSystem(ctx context.Context, userID []byte, payload json.RawMessage) error {
	if p.client == nil {
		return fmt.Errorf("redis client unavailable")
	}
	return p.client.Publish(ctx, "sys:"+hex.EncodeToString(userID), payload).Err()
}

type TokenLastUsedThrottle struct {
	client *goredis.Client
}

func NewTokenLastUsedThrottle(client *goredis.Client) *TokenLastUsedThrottle {
	if client == nil {
		return nil
	}
	return &TokenLastUsedThrottle{client: client}
}

func (t *TokenLastUsedThrottle) ShouldUpdateTokenLastUsed(ctx context.Context, jti []byte, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("token:last_used:%s", hex.EncodeToString(jti))
	return t.client.SetNX(ctx, key, 1, ttl).Result()
}

type MessageSequence struct {
	client  *goredis.Client
	msgRepo repository.MessageRepository
}

func NewMessageSequence(client *goredis.Client, msgRepo repository.MessageRepository) *MessageSequence {
	if client == nil || msgRepo == nil {
		return nil
	}
	return &MessageSequence{client: client, msgRepo: msgRepo}
}

func (s *MessageSequence) Next(ctx context.Context, convID []byte) (uint64, error) {
	convHex := strings.ToLower(hex.EncodeToString(convID))
	seqKey := fmt.Sprintf("seq:%s", convHex)
	lockKey := fmt.Sprintf("look:seq_init:%s", convHex)

	exists, err := s.client.Exists(ctx, seqKey).Result()
	if err != nil {
		return 0, err
	}
	if exists == 0 {
		acquired, err := s.client.SetNX(ctx, lockKey, 1, 5*time.Second).Result()
		if err != nil {
			return 0, err
		}

		if acquired {
			defer func() {
				_ = s.client.Del(ctx, lockKey).Err()
			}()

			maxSeq, err := s.msgRepo.GetMaxSeq(ctx, convID)
			if err != nil {
				return 0, err
			}
			if err := s.client.Set(ctx, seqKey, maxSeq, 0).Err(); err != nil {
				return 0, err
			}
		} else {
			for range 10 {
				time.Sleep(10 * time.Millisecond)
				exists, err := s.client.Exists(ctx, seqKey).Result()
				if err != nil {
					return 0, err
				}
				if exists > 0 {
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
