package redis

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	notifyChannel = "notify:%s" // s = conversation ID

	typingKey = "typing:%s:%s" // s = conversation ID, user ID
	typingTTL = 3 * time.Second

	unreadKey = "unread:%s:%s" // s = user ID, conversation ID

	seqKey = "seq:%s" // s = conversation ID
)

type EventType string

const (
	EventConvCreated   EventType = "conversation.created"
	EventMemberAdded   EventType = "member.added"
	EventMemberRemoved EventType = "member.removed"

	EventNewMessage  EventType = "message.new"
	EventReadMessage EventType = "message.read"
	EventEditMessage EventType = "message.edited"
	EventDelMessage  EventType = "message.deleted"

	EventToggleReaction EventType = "reaction.toggle"
)

type Event struct {
	Type    EventType
	ConvID  string
	Payload json.RawMessage
}

type PubSubBroker struct {
	client *redis.Client
}

func NewPubSubBroker(client *redis.Client) *PubSubBroker {
	return &PubSubBroker{client: client}
}

func (b *PubSubBroker) Publish(ctx context.Context, convID []byte, event Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	channel := fmt.Sprintf(notifyChannel, hex.EncodeToString(convID))
	return b.client.Publish(ctx, channel, data).Err()
}

func (b *PubSubBroker) Subscribe(ctx context.Context, convID []byte) (<-chan *redis.Message, func(), error) {
	channel := fmt.Sprintf(notifyChannel, hex.EncodeToString(convID))
	pubsub := b.client.Subscribe(ctx, channel)

	// Đảm bảo kênh đã được subscribe trước khi trả về
	_, err := pubsub.Receive(ctx)
	if err != nil {
		return nil, nil, err
	}

	return pubsub.Channel(), func() { pubsub.Close() }, nil
}

func (b *PubSubBroker) SetTyping(ctx context.Context, convID, userID []byte) error {
	key := fmt.Sprintf(
		typingKey,
		hex.EncodeToString(convID),
		hex.EncodeToString(userID),
	)
	return b.client.Set(ctx, key, 1, typingTTL).Err()
}

func (b *PubSubBroker) GetTypingUsers(ctx context.Context, convID []byte) ([]string, error) {
	pattern := fmt.Sprintf(typingKey, hex.EncodeToString(convID), "*")
	keys, err := b.client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		parts := strings.Split(key, ":")
		if len(parts) == 3 {
			ids = append(ids, parts[2]) // Lấy phần user ID
		}
	}
	return ids, nil
}

func (b *PubSubBroker) IncrUnread(ctx context.Context, convID, userID []byte) error {
	key := fmt.Sprintf(
		unreadKey,
		hex.EncodeToString(userID),
		hex.EncodeToString(convID),
	)
	return b.client.Incr(ctx, key).Err()
}

func (b *PubSubBroker) ResetUnread(ctx context.Context, userID, convID []byte) error {
	key := fmt.Sprintf(
		unreadKey,
		hex.EncodeToString(userID),
		hex.EncodeToString(convID),
	)
	return b.client.Del(ctx, key).Err()
}

func (b *PubSubBroker) GetUnread(ctx context.Context, userID, convID []byte) (int64, error) {
	key := fmt.Sprintf(
		unreadKey,
		hex.EncodeToString(userID),
		hex.EncodeToString(convID),
	)
	val, err := b.client.Get(ctx, key).Int64()
	if err != nil {
		if err == redis.Nil {
			return 0, nil // Không có key nào, tức là chưa có tin nhắn nào chưa đọc
		}
	}
	return val, err
}

func (b *PubSubBroker) GetNextSeq(ctx context.Context, convID []byte) (int64, error) {
	key := fmt.Sprintf(seqKey, hex.EncodeToString(convID))
	seq, err := b.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	return seq, nil
}

func (b *PubSubBroker) InitSeq(ctx context.Context, convID []byte, seq int64) error {
	key := fmt.Sprintf(seqKey, hex.EncodeToString(convID))
	return b.client.SetNX(ctx, key, seq, 0).Err()
}

func (b *PubSubBroker) DeleteSeq(ctx context.Context, convID []byte) error {
	key := fmt.Sprintf(seqKey, hex.EncodeToString(convID))
	return b.client.Del(ctx, key).Err()
}

func (b *PubSubBroker) ToJsonRawMessage(payload map[string]any) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
