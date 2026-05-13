package websocket

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

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
	presenceTTL    = 30 * time.Second
	typingTTL      = 3 * time.Second
)

// Client đại diện 1 kết nối WS của 1 user
type Client struct {
	uid    []byte
	uidHex string

	conn *gorillaws.Conn
	send chan []byte

	hub *Hub
	rm  *RoomManager
	rdb *redis.Client
	log *zap.Logger

	convMu sync.RWMutex
	convs  map[string]struct{} // hex conv_id
}

func NewClient(
	uid []byte,
	conn *gorillaws.Conn,
	hub *Hub,
	rm *RoomManager,
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
		conn:   conn,
		send:   make(chan []byte, sendBufferSize),
		hub:    hub,
		rm:     rm,
		rdb:    rdb,
		log:    log,
		convs:  convSet,
	}
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

// readPump đọc message từ WS connection và xử lý chúng
func (c *Client) readPump() {
	defer func() {
		c.hub.Unregister(c)
		c.conn.Close()
		// Cleanup presence
		_ = c.rdb.Del(context.Background(), presenceKey(c.uidHex)).Err()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait)) // Thiết lập deadline ban đầu
	c.conn.SetPongHandler(func(string) error {           // Reset deadline mỗi khi nhận được pong
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
	case InboundPing:
		c.onPing()
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

func (c *Client) onPing() {
	ctx := context.Background()
	_ = c.rdb.Expire(ctx, presenceKey(c.uidHex), presenceTTL).Err()

	pong, _ := json.Marshal(OutboundEvent{Type: OutboundPong})
	select {
	case c.send <- pong:
	default: // channel buffered, -> bỏ qua pong
	}
}

func (c *Client) onTyping(payload json.RawMessage) {
	var p TypingPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}

	if !c.hasConv(p.ConvID) {
		c.log.Warn("ws typing for non-viewing conv",
			zap.String("uid", c.uidHex),
			zap.String("conv_id", p.ConvID),
		)
		return
	}

	ctx := context.Background()
	_ = c.rdb.Set(ctx, typingKey(p.ConvID, c.uidHex), 1, typingTTL).Err()
	payload, _ = json.Marshal(map[string]interface{}{
		"event":   "typing",
		"user_id": c.uidHex,
		"conv_id": p.ConvID,
	})
	_ = c.rdb.Publish(ctx, notifyChannelHex(p.ConvID), payload).Err()
}

func (c *Client) onViewing(payload json.RawMessage) {
	var p ViewingPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}
	if !c.hasConv(p.ConvID) {
		return
	}
	cid, err := hex.DecodeString(p.ConvID)
	if err != nil {
		return
	}
	c.rm.SetViewing(c.uid, cid)
}

func (c *Client) onLeft(payload json.RawMessage) {
	var p LeftPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return
	}
	if !c.hasConv(p.ConvID) {
		return
	}
	cid, err := hex.DecodeString(p.ConvID)
	if err != nil {
		return
	}
	c.rm.ClearViewing(cid, c.uid)
}

func (c *Client) onRead(payload json.RawMessage) {
}
