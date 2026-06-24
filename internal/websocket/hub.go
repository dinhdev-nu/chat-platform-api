package websocket

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type Hub struct {
	ctx  context.Context
	done chan struct{}

	stopping atomic.Bool

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

func NewHub(ctx context.Context, rdb *redis.Client, rm *RoomManager, log *zap.Logger) *Hub {
	return &Hub{
		ctx:      ctx,
		done:     make(chan struct{}),
		clients:  make(map[string]map[string]*Client),
		channels: make(map[string]map[string]struct{}),
		rdb:      rdb,
		pubsub:   rdb.Subscribe(ctx, presenceEventChannel),
		rm:       rm,
		log:      log,
	}
}

func (h *Hub) Run() {
	defer close(h.done)

	redisCh := h.pubsub.Channel(redis.WithChannelSize(1000)) // buffer to prevent blocking

	for {
		select {
		case msg, ok := <-redisCh:
			if !ok {
				h.shutdown()
				return
			}
			if msg.Channel == presenceEventChannel {
				h.handlePresenceEvent([]byte(msg.Payload))
			} else if strings.HasPrefix(msg.Channel, "sys:") {
				// internal system Hub - Hub sử dụng channel này để nhận sự kiện subscribe/unsubscribe từ client
				h.handleSysEvent(msg.Channel, []byte(msg.Payload))
			} else {
				// event Hub - Hub sử dụng channel này để nhận sự kiện từ Redis và dispatch đến client
				h.dispatchAndIntercept(msg.Channel, []byte(msg.Payload))
			}
		case <-h.ctx.Done():
			h.shutdown()
			return
		}
	}
}

func (h *Hub) Wait(ctx context.Context) error {
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) shutdown() {
	h.stopping.Store(true)

	h.clientsMu.RLock()
	clients := make([]*Client, 0)
	for _, sessions := range h.clients {
		for _, client := range sessions {
			clients = append(clients, client)
		}
	}
	h.clientsMu.RUnlock()

	for _, client := range clients {
		if err := client.conn.Close(); err != nil {
			h.log.Debug("failed to close WebSocket during shutdown",
				zap.String("uid", client.uidHex),
				zap.Error(err),
			)
		}
	}

	if err := h.pubsub.Close(); err != nil {
		h.log.Warn("failed to close Redis pubsub", zap.Error(err))
	}
}

// Register adds a WebSocket client and subscribes its Redis channels.
func (h *Hub) Register(client *Client, convIDs [][]byte) bool {
	newChannels := make([]string, 0, len(client.convs)+1)
	// client map
	h.clientsMu.Lock()
	if h.stopping.Load() || h.ctx.Err() != nil {
		h.clientsMu.Unlock()
		return false
	}
	if _, ok := h.clients[client.uidHex]; !ok {
		h.clients[client.uidHex] = make(map[string]*Client)
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
		ctx, cancel := h.redisContext()
		err := h.pubsub.Subscribe(ctx, newChannels...)
		cancel()
		if err != nil {
			h.log.Warn("pubsub subscribe failed", zap.Strings("channels", newChannels), zap.Error(err))
		}
	}
	return true
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
	if !stillOnline && !h.stopping.Load() && h.ctx.Err() == nil {
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

	h.rm.ClearSession(client.uid, client.ConnID)

	// Clear local viewing state for this user after the last local session leaves.
	// Redis viewing entries are tracked per connection and are already removed by ClearSession.
	if !stillOnline {
		h.rm.ClearAll(client.uid)
	}

	wentOffline := false
	if !h.stopping.Load() && h.ctx.Err() == nil {
		ctx, cancel := h.redisContext()
		if g.Presence != nil {
			var err error
			wentOffline, err = g.Presence.SetSessionOffline(ctx, client.uid, client.ConnID)
			if err != nil {
				h.log.Warn("failed to clear user presence",
					zap.String("uid", client.uidHex),
					zap.Error(err),
				)
			}
		} else if !stillOnline {
			if err := h.rdb.Del(ctx, presenceKey(client.uidHex)).Err(); err != nil {
				h.log.Warn("failed to clear fallback user presence",
					zap.String("uid", client.uidHex),
					zap.Error(err),
				)
			}
			wentOffline = true
		}
		cancel()
	}

	if wentOffline {
		h.publishPresence(client.uidHex, convIDs, false)
	}

	// Unsubscribe Redis channels that are now empty
	if len(emptyChannels) > 0 && !h.stopping.Load() && h.ctx.Err() == nil {
		ctx, cancel := h.redisContext()
		err := h.pubsub.Unsubscribe(ctx, emptyChannels...)
		cancel()
		if err != nil {
			h.log.Warn("pubsub unsubscribe failed", zap.Strings("channels", emptyChannels), zap.Error(err))
		}
	}
}

func (h *Hub) dispatchAndIntercept(channel string, payload []byte) {
	// Intercept domain membership events (member.added/member.removed)
	h.interceptMembership(payload)

	// Then dispatch the client-facing event payload to subscribed clients.
	h.dispatchToClients(channel, clientPayloadForPayload(payload))
}

func (h *Hub) dispatchToClients(channel string, payload []byte) {
	skipUID := skipUIDForPayload(payload)

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
		if uid == skipUID {
			continue
		}
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

func skipUIDForPayload(payload []byte) string {
	var evt struct {
		Event   string          `json:"event"`
		UserID  string          `json:"user_id"`
		Payload json.RawMessage `json:"Payload"`
	}
	if err := json.Unmarshal(payload, &evt); err != nil {
		return ""
	}
	if evt.Event == OutboundTyping {
		return strings.ToLower(strings.TrimSpace(evt.UserID))
	}
	if evt.Event == OutboundMemberAdded {
		return strings.ToLower(strings.TrimSpace(evt.UserID))
	}
	if evt.Event == OutboundMemberRemoved {
		return strings.ToLower(strings.TrimSpace(evt.UserID))
	}

	if len(evt.Payload) == 0 {
		return ""
	}
	var nested struct {
		Event  string `json:"event"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(evt.Payload, &nested); err != nil {
		return ""
	}
	if nested.Event == OutboundTyping {
		return strings.ToLower(strings.TrimSpace(nested.UserID))
	}
	if nested.Event == OutboundMemberAdded {
		return strings.ToLower(strings.TrimSpace(nested.UserID))
	}
	if nested.Event == OutboundMemberRemoved {
		return strings.ToLower(strings.TrimSpace(nested.UserID))
	}
	return ""
}

func clientPayloadForPayload(payload []byte) []byte {
	var wrapped struct {
		Payload json.RawMessage `json:"Payload"`
	}
	if err := json.Unmarshal(payload, &wrapped); err == nil && len(wrapped.Payload) > 0 {
		return wrapped.Payload
	}

	var wrappedLower struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(payload, &wrappedLower); err == nil && len(wrappedLower.Payload) > 0 {
		return wrappedLower.Payload
	}
	return payload
}

// Hub observes membership events and updates local subscriptions for online users.
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
		ctx, cancel := h.redisContext()
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
	if wrapped.Type == "" && wrapped.ConvID == "" && len(wrapped.Payload) == 0 {
		var lower struct {
			Type    string          `json:"type"`
			ConvID  string          `json:"conv_id"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(payload, &lower); err != nil {
			return DomainEvent{}, false
		}
		wrapped.Type = lower.Type
		wrapped.ConvID = lower.ConvID
		wrapped.Payload = lower.Payload
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
		cidBytes, ok := decodeSysConvID(cmd.ConvID)
		if !ok {
			return
		}
		for _, client := range sessions {
			h.subscribeLocalClient(client, cidBytes)
		}
		h.sendSysPayload(sessions, cmd.Payload)
	case SysConvUnsubscribe:
		cidBytes, ok := decodeSysConvID(cmd.ConvID)
		if !ok {
			return
		}
		for _, client := range sessions {
			h.unsubscribeLocalClient(client, cidBytes)
			h.rm.ClearViewing(cidBytes, client.uid, client.ConnID)
		}
		h.sendSysPayload(sessions, cmd.Payload)
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

func decodeSysConvID(convID string) ([]byte, bool) {
	cidHex := strings.ToLower(strings.TrimSpace(convID))
	cidBytes, err := hex.DecodeString(cidHex)
	return cidBytes, err == nil && len(cidBytes) == 16
}

func (h *Hub) sendSysPayload(sessions []*Client, payload json.RawMessage) {
	if len(payload) == 0 {
		return
	}
	h.sendToTargets(sessions, payload)
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

func (h *Hub) publishPresence(uidHex string, convIDs []string, isOnline bool) {
	if !isValidHexUID(uidHex) {
		return
	}

	raw, err := json.Marshal(presenceEvent{
		UserID:   uidHex,
		IsOnline: isOnline,
		ConvIDs:  convIDs,
	})
	if err != nil {
		return
	}

	ctx, cancel := h.redisContext()
	err = h.rdb.Publish(ctx, presenceEventChannel, raw).Err()
	cancel()
	if err != nil {
		h.log.Warn("presence publish failed",
			zap.String("uid", uidHex),
			zap.Bool("is_online", isOnline),
			zap.Error(err),
		)
		h.broadcastPresence(uidHex, convIDs, isOnline)
	}
}

func (h *Hub) handlePresenceEvent(payload []byte) {
	var evt presenceEvent
	if err := json.Unmarshal(payload, &evt); err != nil {
		return
	}
	evt.UserID = strings.ToLower(strings.TrimSpace(evt.UserID))
	if !isValidHexUID(evt.UserID) {
		return
	}
	h.broadcastPresence(evt.UserID, evt.ConvIDs, evt.IsOnline)
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
	ctx, cancel := h.redisContext()
	defer cancel()

	if g.Presence != nil {
		online, err := g.Presence.IsOnline(ctx, uid)
		if err != nil {
			h.log.Warn("failed to read user presence", zap.String("uid", uidHex), zap.Error(err))
		}
		return err == nil && online
	}
	exists, err := h.rdb.Exists(ctx, presenceKey(uidHex)).Result()
	if err != nil {
		h.log.Warn("failed to read fallback user presence", zap.String("uid", uidHex), zap.Error(err))
	}
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
		ctx, cancel := h.redisContext()
		err := h.pubsub.Subscribe(ctx, ch)
		cancel()
		if err != nil {
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
		ctx, cancel := h.redisContext()
		err := h.pubsub.Unsubscribe(ctx, ch)
		cancel()
		if err != nil {
			h.log.Warn("failed to unsubscribe from redis channel", zap.String("channel", ch), zap.Error(err))
		}
	}
}
