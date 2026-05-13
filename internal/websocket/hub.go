package websocket

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type Hub struct {
	clientsMu sync.RWMutex
	clients   map[string]*Client // uid -> client

	channelsMu sync.RWMutex
	channels   map[string]map[string]*Client // redis channel -> uid -> client

	rdb    *redis.Client
	pubsub *redis.PubSub
	rm     *RoomManager
	log    *zap.Logger
}

func NewHub(rdb *redis.Client, rm *RoomManager, log *zap.Logger) *Hub {
	return &Hub{
		clients:  make(map[string]*Client),
		channels: make(map[string]map[string]*Client),
		rdb:      rdb,
		pubsub:   rdb.Subscribe(context.Background()), // emty subscription
		rm:       rm,
		log:      log,
	}
}

func (h *Hub) Run(ctx context.Context) {
	redisCh := h.pubsub.Channel(redis.WithChannelSize(1000)) // buffer to prevent blocking

	go func() {
		for {
			select {
			case msg, ok := <-redisCh:
				if !ok {
					return
				}
				if strings.HasPrefix(msg.Channel, "sys:") {
					// internal system Hub - Hub sử dụng channel này để nhận sự kiện subscribe/unsubscribe từ client
					h.handleSysEvent(msg.Channel, []byte(msg.Payload))
				} else {
					// event Hub - Hub sử dụng channel này để nhận sự kiện từ Redis và dispatch đến client
					h.dispatchAndIntercept(msg.Channel, []byte(msg.Payload))
				}
			case <-ctx.Done():
				_ = h.pubsub.Close()
				return
			}
		}
	}()
}

// Register thêm client vào Hub khi WS connect.
// Subscribe Redis channels cho tất cả conv + sys:{uid} của user.
func (h *Hub) Register(client *Client, convIDs [][]byte) {
	newChannels := make([]string, 0, len(client.convs)+1)
	//client mapp
	h.clientsMu.Lock()
	h.clients[client.uidHex] = client
	h.clientsMu.Unlock()
	// channels mapp
	h.channelsMu.Lock()
	for _, cid := range convIDs {
		ch := notifyChannel(cid)
		if _, ok := h.channels[ch]; !ok {
			h.channels[ch] = make(map[string]*Client)
			newChannels = append(newChannels, ch)
		}
		h.channels[ch][client.uidHex] = client
	}
	sysCh := sysChannel(client.uidHex)
	h.channels[sysCh] = map[string]*Client{client.uidHex: client}
	newChannels = append(newChannels, sysCh)
	h.channelsMu.Unlock()
	// Subscribe Redis channels nếu có channel mới
	if len(newChannels) > 0 {
		if err := h.pubsub.Subscribe(context.Background(), newChannels...); err != nil {
			h.log.Warn("pubsub subscribe failed", zap.Strings("channels", newChannels), zap.Error(err))
		}
	}
}

// Unregister xóa client khỏi Hub khi WS disconnect.
// Unsubscribe Redis channels nếu không còn client nào subscribe.
func (h *Hub) Unregister(client *Client) {
	h.clientsMu.Lock()
	if _, ok := h.clients[client.uidHex]; !ok {
		h.channelsMu.Lock()
		return // đã unregister rồi, tránh xóa nhiều lần
	}
	delete(h.clients, client.uidHex)
	h.channelsMu.Unlock()

	emptyChannels := make([]string, 0)
	h.channelsMu.Lock()
	for ch, clients := range h.channels {
		delete(clients, client.uidHex)
		if len(clients) == 0 {
			delete(h.channels, ch)
			emptyChannels = append(emptyChannels, ch)
		}
	}
	h.channelsMu.Unlock()
	close(client.send)        // Đóng channel send để dừng goroutine writePump
	h.rm.ClearAll(client.uid) // Xóa tất cả trạng thái viewing của user

	// Unsubscribe Redis channels nếu có channel nào trống
	if len(emptyChannels) > 0 {
		if err := h.pubsub.Unsubscribe(context.Background(), emptyChannels...); err != nil {
			h.log.Warn("pubsub unsubscribe failed", zap.Strings("channels", emptyChannels), zap.Error(err))
		}
	}
}

func (h *Hub) dispatchAndIntercept(channel string, payload []byte) {
	h.dispatchToClients(channel, payload)
}

func (h *Hub) dispatchToClients(channel string, payload []byte) {
	h.channelsMu.RLock()
	clients, ok := h.channels[channel]
	if !ok { // Không có client nào subscribe channel này
		h.channelsMu.RUnlock()
		return
	}
	targets := make([]*Client, 0, len(clients))
	for _, c := range clients {
		targets = append(targets, c)
	}
	h.channelsMu.RUnlock()

	// Gửi payload đến tất cả client subscribe channel này
	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
			// Buffer đầy → client đọc quá chậm → ngắt kết nối.
			h.log.Warn("client send buffer full, disconnecting", zap.String("uid", c.uidHex))
			h.Unregister(c)
		}
	}
}

// Đây là cơ chế Hub tự biết cần subscribe/unsubscribe mà không cần service ra lệnh.
// Service publish "member.added" → Hub intercept → xử lý subscription autonomously.
func (h *Hub) interceptMembership(payload []byte) {
	var evt DomainEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	if evt.UserID == "" || evt.ConvID == "" {
		return
	}
	var sysType string
	switch evt.Event {
	case DomainMemberAdded:
		sysType = SysConvSubscribe
	case DomainMemberRemoved:
		sysType = SysConvUnsubscribe
	default:
		return
	}

	cmd, err := json.Marshal(sysEvent{
		Type:   sysType,
		ConvID: evt.ConvID,
	})
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.rdb.Publish(ctx, sysChannel(evt.UserID), cmd).Err(); err != nil {
			h.log.Warn("sys publish failed", zap.String("channel", sysChannel(evt.UserID)), zap.String("conv_id", evt.ConvID), zap.Error(err))
		}
	}()
}

func (h *Hub) handleSysEvent(chanel string, payload []byte) {
	uidhex := strings.TrimPrefix(chanel, "sys:")
	h.clientsMu.RLock()
	client, online := h.clients[uidhex]
	h.clientsMu.RUnlock()

	if !online {
		return
	}

	var cmd sysEvent
	if err := json.Unmarshal(payload, &cmd); err != nil || cmd.ConvID == "" {
		return
	}

	cidBytes, err := hex.DecodeString(cmd.ConvID)
	if err != nil {
		return
	}
	switch cmd.Type {
	case SysConvSubscribe:
		h.subscribeLocalClient(client, cidBytes)
	case SysConvUnsubscribe:
		h.unsubscribeLocalClient(client, cidBytes)
	}
}

func (h *Hub) subscribeLocalClient(client *Client, convID []byte) {
	ch := notifyChannel(convID)
	cidHex := hex.EncodeToString(convID)

	needSubscribe := false
	h.channelsMu.Lock()
	if _, ok := h.channels[ch]; !ok {
		h.channels[ch] = make(map[string]*Client)
		needSubscribe = true
	}
	h.channels[ch][client.uidHex] = client
	h.channelsMu.Unlock()

	client.addConv(cidHex)

	// Subscribe redis channel nếu chưa có ai subscribe
	if needSubscribe {
		if err := h.pubsub.Subscribe(context.Background(), ch); err != nil {
			h.log.Warn("failed to subscribe to redis channel", zap.String("channel", ch), zap.Error(err))
		}
	}
}

func (h *Hub) unsubscribeLocalClient(client *Client, convID []byte) {
	ch := notifyChannel(convID)
	cidHex := hex.EncodeToString(convID)

	client.removeConv(cidHex)
	channelEmpty := false
	h.channelsMu.Lock()
	if clients, ok := h.channels[ch]; ok {
		delete(clients, client.uidHex)
		if len(clients) == 0 {
			delete(h.channels, ch)
			channelEmpty = true
		}
	}

	h.channelsMu.Unlock()

	if channelEmpty {
		if err := h.pubsub.Unsubscribe(context.Background(), ch); err != nil {
			h.log.Warn("failed to unsubscribe from redis channel", zap.String("channel", ch), zap.Error(err))
		}
	}
}
