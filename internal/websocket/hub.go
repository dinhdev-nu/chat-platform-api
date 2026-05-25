package websocket

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type Hub struct {
	clientsMu sync.RWMutex
	// uidHex -> connID -> *Client
	clients map[string]map[string]*Client

	channelsMu sync.RWMutex
	// channel -> uid -> present
	channels map[string]map[string]struct{}

	rdb    *redis.Client
	pubsub *redis.PubSub
	rm     *RoomManager
	log    *zap.Logger
}

func NewHub(rdb *redis.Client, rm *RoomManager, log *zap.Logger) *Hub {
	return &Hub{
		clients:  make(map[string]map[string]*Client),
		channels: make(map[string]map[string]struct{}),
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
	firstSession := false
	//client mapp
	h.clientsMu.Lock()
	if _, ok := h.clients[client.uidHex]; !ok {
		h.clients[client.uidHex] = make(map[string]*Client)
		firstSession = true
	}
	h.clients[client.uidHex][client.ConnID] = client
	h.clientsMu.Unlock()
	// channels mapp
	h.channelsMu.Lock()
	for _, cid := range convIDs {
		ch := notifyChannel(cid)
		if _, ok := h.channels[ch]; !ok {
			h.channels[ch] = make(map[string]struct{})
			newChannels = append(newChannels, ch)
		}
		h.channels[ch][client.uidHex] = struct{}{}

		client.addConv(hex.EncodeToString(cid))
	}
	sysCh := sysChannel(client.uidHex)
	if _, ok := h.channels[sysCh]; !ok {
		h.channels[sysCh] = make(map[string]struct{})
		newChannels = append(newChannels, sysCh)
	}
	h.channels[sysCh][client.uidHex] = struct{}{}
	h.channelsMu.Unlock()
	// Subscribe Redis channels nếu có channel mới
	if len(newChannels) > 0 {
		if err := h.pubsub.Subscribe(context.Background(), newChannels...); err != nil {
			h.log.Warn("pubsub subscribe failed", zap.Strings("channels", newChannels), zap.Error(err))
		}
	}
	if firstSession {
		h.broadcastPresence(client.uidHex, client.snapshotConvs(), true)
	}
}

// Unregister xóa client khỏi Hub khi WS disconnect.
// Unsubscribe Redis channels nếu không còn client nào subscribe.
func (h *Hub) Unregister(client *Client) {
	// Remove this session (conn) from clients map. If user has no more sessions,
	// remove uid entries from channels and schedule redis unsubscribe.
	convIDs := client.snapshotConvs()

	h.clientsMu.Lock()
	sessions, ok := h.clients[client.uidHex]
	if !ok {
		h.clientsMu.Unlock()
		return // already unregistered
	}

	// remove this connection
	delete(sessions, client.ConnID)
	// track whether user still has any sessions
	stillOnline := len(sessions) > 0
	if !stillOnline {
		delete(h.clients, client.uidHex)
	}
	h.clientsMu.Unlock()

	// If user went offline (no sessions), remove uid from channels map
	emptyChannels := make([]string, 0)
	if !stillOnline {
		h.channelsMu.Lock()
		for ch, uids := range h.channels {
			delete(uids, client.uidHex)
			if len(uids) == 0 {
				delete(h.channels, ch)
				emptyChannels = append(emptyChannels, ch)
			}
		}
		h.channelsMu.Unlock()
	}

	// Close send channel (idempotent via closeSend)
	client.closeSend()

	// Clear viewing state for this specific user (if offline)
	if !stillOnline {
		h.rm.ClearAll(client.uid)
		// cleanup presence via centralized PresenceStore
		if g.Presence != nil {
			_ = g.Presence.SetOffline(context.Background(), client.uid)
		} else {
			_ = h.rdb.Del(context.Background(), presenceKey(client.uidHex)).Err()
		}
		h.broadcastPresence(client.uidHex, convIDs, false)
	}

	// Unsubscribe Redis channels that are now empty
	if len(emptyChannels) > 0 {
		if err := h.pubsub.Unsubscribe(context.Background(), emptyChannels...); err != nil {
			h.log.Warn("pubsub unsubscribe failed", zap.Strings("channels", emptyChannels), zap.Error(err))
		}
	}
}

func (h *Hub) dispatchAndIntercept(channel string, payload []byte) {
	// Intercept domain membership events (member.added/member.removed)
	h.interceptMembership(payload)

	// Then dispatch the original payload to subscribed clients.
	h.dispatchToClients(channel, payload)
}

func (h *Hub) dispatchToClients(channel string, payload []byte) {
	h.channelsMu.RLock()
	uids, ok := h.channels[channel]
	if !ok { // No subscribers
		h.channelsMu.RUnlock()
		return
	}
	// collect targets across all sessions for each uid
	targets := make([]*Client, 0)
	h.clientsMu.RLock()
	for uid := range uids {
		if sessions, ok := h.clients[uid]; ok {
			for _, c := range sessions {
				targets = append(targets, c)
			}
		}
	}
	h.clientsMu.RUnlock()
	h.channelsMu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- payload:
		default:
			h.log.Warn("client send buffer full, disconnecting", zap.String("uid", c.uidHex))
			h.Unregister(c)
		}
	}
}

// Đây là cơ chế Hub tự biết cần subscribe/unsubscribe mà không cần service ra lệnh.
// Service publish "member.added" → Hub intercept → xử lý subscription autonomously.
func (h *Hub) interceptMembership(payload []byte) {
	evt, ok := decodeDomainEvent(payload)
	if !ok {
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

func decodeDomainEvent(payload []byte) (DomainEvent, bool) {
	var evt DomainEvent
	if err := json.Unmarshal(payload, &evt); err == nil && evt.Event != "" {
		return evt, true
	}

	var wrapped struct {
		Type    string          `json:"Type"`
		ConvID  string          `json:"ConvID"`
		Payload json.RawMessage `json:"Payload"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return DomainEvent{}, false
	}
	if len(wrapped.Payload) > 0 {
		if err := json.Unmarshal(wrapped.Payload, &evt); err == nil && evt.Event != "" {
			return evt, true
		}
	}

	if wrapped.Type == "" || wrapped.ConvID == "" {
		return DomainEvent{}, false
	}
	return DomainEvent{
		Event:  wrapped.Type,
		ConvID: wrapped.ConvID,
	}, true
}

func (h *Hub) handleSysEvent(chanel string, payload []byte) {
	uidhex := strings.TrimPrefix(chanel, "sys:")
	sessions := h.sessionsForUser(uidhex)

	if len(sessions) == 0 {
		return
	}

	var cmd sysEvent
	if err := json.Unmarshal(payload, &cmd); err != nil {
		return
	}

	switch cmd.Type {
	case SysConvSubscribe:
		cidBytes, err := hex.DecodeString(cmd.ConvID)
		if err != nil {
			return
		}
		for _, client := range sessions {
			h.subscribeLocalClient(client, cidBytes)
		}
	case SysConvUnsubscribe:
		cidBytes, err := hex.DecodeString(cmd.ConvID)
		if err != nil {
			return
		}
		for _, client := range sessions {
			h.unsubscribeLocalClient(client, cidBytes)
		}
	case SysContactsSet:
		for _, client := range sessions {
			client.setContacts(cmd.UserIDs)
		}
	case SysContactsAdd:
		for _, client := range sessions {
			client.addContact(cmd.UserID)
		}
		h.sendPresenceSnapshot(sessions, cmd.UserID)
	}
}

func (h *Hub) sessionsForUser(uidHex string) []*Client {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()

	sessionMap, ok := h.clients[uidHex]
	if !ok {
		return nil
	}
	sessions := make([]*Client, 0, len(sessionMap))
	for _, client := range sessionMap {
		sessions = append(sessions, client)
	}
	return sessions
}

func (h *Hub) broadcastPresence(uidHex string, convIDs []string, isOnline bool) {
	h.broadcastPresenceToContacts(uidHex, isOnline)
	h.broadcastPresenceToConvs(uidHex, convIDs, isOnline)
}

func (h *Hub) broadcastPresenceToContacts(uidHex string, isOnline bool) {
	raw := presencePayload(uidHex, "", isOnline)
	if len(raw) == 0 {
		return
	}

	targets := make([]*Client, 0)
	h.clientsMu.RLock()
	for targetUID, sessions := range h.clients {
		if targetUID == uidHex {
			continue
		}
		for _, client := range sessions {
			if client.hasContact(uidHex) {
				targets = append(targets, client)
			}
		}
	}
	h.clientsMu.RUnlock()

	h.sendToTargets(targets, raw)
}

func (h *Hub) broadcastPresenceToConvs(uidHex string, convIDs []string, isOnline bool) {
	for _, cidHex := range convIDs {
		if !isValidHexUID(cidHex) {
			continue
		}

		raw := presencePayload(uidHex, cidHex, isOnline)
		if len(raw) == 0 {
			continue
		}
		h.sendToTargets(h.convPresenceTargets(uidHex, cidHex), raw)
	}
}

func (h *Hub) convPresenceTargets(uidHex, cidHex string) []*Client {
	ch := notifyChannelHex(cidHex)
	targetUIDs := make([]string, 0)

	h.channelsMu.RLock()
	if uids, ok := h.channels[ch]; ok {
		for targetUID := range uids {
			if targetUID != uidHex {
				targetUIDs = append(targetUIDs, targetUID)
			}
		}
	}
	h.channelsMu.RUnlock()

	targets := make([]*Client, 0, len(targetUIDs))
	h.clientsMu.RLock()
	for _, targetUID := range targetUIDs {
		for _, client := range h.clients[targetUID] {
			targets = append(targets, client)
		}
	}
	h.clientsMu.RUnlock()
	return targets
}

func (h *Hub) sendPresenceSnapshot(targets []*Client, uidHex string) {
	if !isValidHexUID(uidHex) {
		return
	}

	raw := presencePayload(uidHex, "", h.isOnline(uidHex))
	h.sendToTargets(targets, raw)
}

func (h *Hub) isOnline(uidHex string) bool {
	uid, err := hex.DecodeString(uidHex)
	if err != nil {
		return false
	}
	if g.Presence != nil {
		online, err := g.Presence.IsOnline(context.Background(), uid)
		return err == nil && online
	}
	exists, err := h.rdb.Exists(context.Background(), presenceKey(uidHex)).Result()
	return err == nil && exists > 0
}

func presencePayload(uidHex, cidHex string, isOnline bool) []byte {
	payload := map[string]any{
		"event":     OutboundPresence,
		"user_id":   uidHex,
		"is_online": isOnline,
	}
	if cidHex != "" {
		payload["conv_id"] = cidHex
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return raw
}

func (h *Hub) sendToTargets(targets []*Client, payload []byte) {
	for _, client := range targets {
		select {
		case client.send <- payload:
		default:
			h.log.Warn("client send buffer full, disconnecting", zap.String("uid", client.uidHex))
			h.Unregister(client)
		}
	}
}

func (h *Hub) subscribeLocalClient(client *Client, convID []byte) {
	ch := notifyChannel(convID)
	cidHex := hex.EncodeToString(convID)

	needSubscribe := false
	h.channelsMu.Lock()
	if _, ok := h.channels[ch]; !ok {
		h.channels[ch] = make(map[string]struct{})
		needSubscribe = true
	}
	h.channels[ch][client.uidHex] = struct{}{}
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
	if uids, ok := h.channels[ch]; ok {
		delete(uids, client.uidHex)
		if len(uids) == 0 {
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
