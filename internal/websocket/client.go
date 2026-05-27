package websocket

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/google/uuid"
	gorillaws "github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = 50 * time.Second
	maxMessageSize = 4 * 1024
	sendBufferSize = 256
	// presenceTTL centralized in internal/infrastructure/redis/presence.go
	typingTTL = 3 * time.Second
)

// Client đại diện 1 kết nối WS của 1 user
type Client struct {
	uid    []byte
	uidHex string

	ConnID    string
	closeOnce sync.Once

	conn *gorillaws.Conn
	send chan []byte

	hub *Hub
	rm  *RoomManager
	mr  messageReadMarker
	rdb *redis.Client
	log *zap.Logger

	convMu sync.RWMutex
	convs  map[string]struct{} // hex conv_id

	contMu sync.RWMutex        // presences của contact
	conts  map[string]struct{} // hex uid
}

func NewClient(
	uid []byte,
	conn *gorillaws.Conn,
	hub *Hub,
	rm *RoomManager,
	mr messageReadMarker,
	rdb *redis.Client,
	convIDs [][]byte,
	log *zap.Logger,
) *Client {
	convSet := make(map[string]struct{})
	for _, convID := range convIDs {
		convSet[hex.EncodeToString(convID)] = struct{}{}
	}
	return &Client{
		uid:    uid,
		uidHex: hex.EncodeToString(uid),
		ConnID: uuid.NewString(),
		conn:   conn,
		send:   make(chan []byte, sendBufferSize),
		hub:    hub,
		rm:     rm,
		mr:     mr,
		rdb:    rdb,
		log:    log,
		convs:  convSet,
		conts:  make(map[string]struct{}),
	}
}

func (c *Client) closeSend() {
	c.closeOnce.Do(func() { close(c.send) })
}

func (c *Client) addConv(cidHex string) {
	c.convMu.Lock()
	defer c.convMu.Unlock()
	c.convs[cidHex] = struct{}{}
}

func (c *Client) removeConv(cidHex string) {
	c.convMu.Lock()
	defer c.convMu.Unlock()
	delete(c.convs, cidHex)
}

func (c *Client) hasConv(cidHex string) bool {
	c.convMu.RLock()
	_, ok := c.convs[cidHex]
	c.convMu.RUnlock()
	return ok
}

func (c *Client) snapshotConvs() []string {
	c.convMu.RLock()
	defer c.convMu.RUnlock()

	convIDs := make([]string, 0, len(c.convs))
	for cidHex := range c.convs {
		convIDs = append(convIDs, cidHex)
	}
	return convIDs
}

func (c *Client) setContacts(uidHexes []string) {
	conts := make(map[string]struct{}, len(uidHexes))
	for _, uidHex := range uidHexes {
		if isValidHexUID(uidHex) && uidHex != c.uidHex {
			conts[uidHex] = struct{}{}
		}
	}

	c.contMu.Lock()
	c.conts = conts
	c.contMu.Unlock()
}

func (c *Client) addContact(uidHex string) {
	if !isValidHexUID(uidHex) || uidHex == c.uidHex {
		return
	}

	c.contMu.Lock()
	if c.conts == nil {
		c.conts = make(map[string]struct{})
	}
	c.conts[uidHex] = struct{}{}
	c.contMu.Unlock()
}

func (c *Client) hasContact(uidHex string) bool {
	c.contMu.RLock()
	_, ok := c.conts[uidHex]
	c.contMu.RUnlock()
	return ok
}

func isValidHexUID(uidHex string) bool {
	uid, err := hex.DecodeString(uidHex)
	return err == nil && len(uid) == 16
}

func normalizeHexID(raw string) (string, []byte, bool) {
	idHex := strings.ToLower(strings.TrimSpace(raw))
	id, err := hex.DecodeString(idHex)
	return idHex, id, err == nil && len(id) == 16
}

// readPump đọc message từ WS connection và xử lý chúng
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait)) // Thiết lập deadline ban đầu
	c.conn.SetPongHandler(func(string) error {           // Reset deadline và refresh presence mỗi khi nhận được pong
		// Refresh presence TTL so presence is tied to protocol-level pong
		if g.Presence != nil {
			_ = g.Presence.Heartbeat(context.Background(), c.uid)
		} else {
			_ = c.rdb.Expire(context.Background(), presenceKey(c.uidHex), 3*pingPeriod).Err()
		}
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			// Chỉ log lỗi nếu không phải do client đóng kết nối một cách bình thường
			if gorillaws.IsUnexpectedCloseError(err,
				gorillaws.CloseGoingAway,
				gorillaws.CloseAbnormalClosure,
			) {
				c.log.Warn("ws unexpected close",
					zap.String("uid", c.uidHex),
					zap.Error(err),
				)
			}
			return // trigger defer → Unregister
		}
		c.handleInbound(data) // Xử lý message nhận được
	}
}

// writePump gửi message từ hub đến WS connection của client
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub đã đóng channel, gửi close message và dừng
				_ = c.conn.WriteMessage(gorillaws.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(gorillaws.TextMessage, msg); err != nil {
				return
			}

			for n := len(c.send); n > 0; n-- { // Gửi nhanh tất cả message còn trong window write
				if err := c.conn.WriteMessage(gorillaws.TextMessage, <-c.send); err != nil {
					return
				}
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(gorillaws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// InboundEvent là struct chung để parse message nhận được từ client
func (c *Client) handleInbound(data []byte) {
	var evt InboundEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return // Ignore message không hợp lệ
	}
	switch evt.Type {
	case InboundTyping:
		c.onTyping(evt.Payload)
	case InboundViewing:
		c.onViewing(evt.Payload)
	case InboundLeft:
		c.onLeft(evt.Payload)
	case InboundRead:
		c.onRead(evt.Payload)
	default:
		c.log.Warn("ws unknown inbound event",
			zap.String("uid", c.uidHex),
			zap.String("type", evt.Type),
		)
	}
}

func (c *Client) onTyping(payload json.RawMessage) {
	var p TypingPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	convHex, _, ok := normalizeHexID(p.ConvID)
	if !ok || !c.hasConv(convHex) {
		c.log.Warn("ws typing for non-viewing conv",
			zap.String("uid", c.uidHex),
			zap.String("conv_id", p.ConvID),
		)
		return
	}

	ctx := context.Background()
	if err := c.rdb.Set(ctx, typingKey(convHex, c.uidHex), 1, typingTTL).Err(); err != nil {
		c.log.Warn("ws typing ttl set failed",
			zap.String("uid", c.uidHex),
			zap.String("conv_id", convHex),
			zap.Error(err),
		)
		return
	}
	typingPayload, err := json.Marshal(map[string]string{
		"event":   "typing",
		"user_id": c.uidHex,
		"conv_id": convHex,
	})
	if err != nil {
		return
	}
	if err := c.rdb.Publish(ctx, notifyChannelHex(convHex), typingPayload).Err(); err != nil {
		c.log.Warn("ws typing publish failed",
			zap.String("uid", c.uidHex),
			zap.String("conv_id", convHex),
			zap.Error(err),
		)
	}
}

func (c *Client) onViewing(payload json.RawMessage) {
	var p ViewingPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}
	convHex, cid, ok := normalizeHexID(p.ConvID)
	if !ok || !c.hasConv(convHex) {
		return
	}
	c.rm.SetViewing(cid, c.uid, c.ConnID)
}

func (c *Client) onLeft(payload json.RawMessage) {
	var p LeftPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}
	convHex, cid, ok := normalizeHexID(p.ConvID)
	if !ok || !c.hasConv(convHex) {
		return
	}
	c.rm.ClearViewing(cid, c.uid, c.ConnID)
}

func (c *Client) onRead(payload json.RawMessage) {
	var p ReadPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	convHex := strings.ToLower(strings.TrimSpace(p.ConvID))
	if !c.hasConv(convHex) {
		c.log.Warn("ws read for non-member conv",
			zap.String("uid", c.uidHex),
			zap.String("conv_id", p.ConvID),
		)
		return
	}

	_, convID, ok := normalizeHexID(convHex)
	if !ok {
		c.log.Warn("ws read invalid conv_id",
			zap.String("uid", c.uidHex),
			zap.String("conv_id", p.ConvID),
		)
		return
	}

	lastReadMsgHex := strings.ToLower(strings.TrimSpace(p.LastReadMsgID))
	_, lastReadMsgID, ok := normalizeHexID(lastReadMsgHex)
	if !ok {
		c.log.Warn("ws read invalid last_read_msg_id",
			zap.String("uid", c.uidHex),
			zap.String("msg_id", lastReadMsgHex),
		)
		return
	}

	if c.mr == nil {
		c.log.Warn("ws read marker unavailable", zap.String("uid", c.uidHex))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.mr.MarkAsRead(ctx, convID, c.uid, lastReadMsgID); err != nil {
		c.log.Warn("ws mark as read failed",
			zap.String("uid", c.uidHex),
			zap.String("conv_id", convHex),
			zap.String("msg_id", lastReadMsgHex),
			zap.Error(err),
		)
	}
}
