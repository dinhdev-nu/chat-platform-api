package websocket

import (
	"context"
	"encoding/hex"
	"net/http"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/gin-gonic/gin"
	gorillaws "github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = gorillaws.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Production nên kiểm tra origin để tránh CSRF
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")

		switch origin {
		case "http://localhost:3000", "https://chat-app-client-phi.vercel.app":
			return true
		default:
			return true
		}
	},
}

type Handler struct {
	hub *Hub
	rm  *RoomManager
	mr  messageReadMarker
	rr  r.RoomRepository
	log *zap.Logger
}

type messageReadMarker interface {
	MarkAsRead(ctx context.Context, convID, userID, lastReadMsgID []byte) error
}

func NewHandler(hub *Hub, rm *RoomManager, mr messageReadMarker, rr r.RoomRepository, log *zap.Logger) *Handler {
	return &Handler{
		hub: hub,
		rm:  rm,
		mr:  mr,
		rr:  rr,
		log: log,
	}
}

func (h *Handler) ServeWS(c *gin.Context) {
	// Middleware auth đã đảm bảo uid luôn tồn tại
	user, exists := getCurrentUser(c)
	if !exists {
		http.Error(c.Writer, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := c.Request.Context()
	w := c.Writer
	r := c.Request
	// Load conversation IDs để join room sau khi kết nối WS thành công
	convIDs, err := h.rr.GetUserConversationIDs(ctx, user.ID)
	if err != nil {
		h.log.Error("failed to load user convs", zap.Error(err))
		http.Error(c.Writer, "internal error", http.StatusInternalServerError)
		return
	}
	// Upgrade HTTP connection lên WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Error("Failed to upgrade to WebSocket", zap.Error(err))
		return
	}
	// SET presence via centralized PresenceStore
	presenceCtx, cancelPresence := context.WithTimeout(ctx, redisOperationTimeout)

	if g.Presence != nil {
		if err := g.Presence.SetOnline(presenceCtx, user.ID); err != nil {
			h.log.Warn("failed to set user online",
				zap.String("user_id", hex.EncodeToString(user.ID)),
				zap.Error(err),
			)
		}
	} else {
		// fallback to previous behaviour if Presence store not initialized
		uidHex := hex.EncodeToString(user.ID)
		if err := h.hub.rdb.Set(
			presenceCtx,
			presenceKey(uidHex), 1,
			3*pingPeriod,
		).Err(); err != nil {
			h.log.Warn("failed to set fallback user online",
				zap.String("user_id", uidHex),
				zap.Error(err),
			)
		}
	}
	// Tạo client mới và đăng ký vào hub
	cancelPresence()

	client := NewClient(user.ID, conn, h.hub, h.rm, h.mr, h.hub.rdb, convIDs, h.log)
	if !h.hub.Register(client, convIDs) {
		if err := conn.Close(); err != nil {
			h.log.Debug("failed to close WebSocket during shutdown", zap.Error(err))
		}
		return
	}

	go client.writePump()
	client.readPump()

	go h.enqueueUserLastSeen(user.ID, time.Now())
}

func (h *Handler) enqueueUserLastSeen(userID []byte, seenAt time.Time) {
	userIDCopy := append([]byte(nil), userID...)
	payload := queue.UserLastSeenPayload{
		UserID: userIDCopy,
		SeenAt: seenAt,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if g.Stream == nil {
		h.log.Warn("ws last_seen: stream unavailable, dropping best-effort job",
			zap.String("user_id", hex.EncodeToString(userIDCopy)),
		)
		return
	}
	if err := g.Stream.EnqueueJob(ctx, queue.JobUpdateUserLastSeen, payload); err != nil {
		h.log.Warn("ws last_seen: enqueue failed, dropping best-effort job",
			zap.String("user_id", hex.EncodeToString(userIDCopy)),
			zap.Error(err),
		)
	}
}

func getCurrentUser(c *gin.Context) (*model.User, bool) {
	val, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	user, ok := val.(*model.User)
	return user, ok
}
