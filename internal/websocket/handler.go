package websocket

import (
	"context"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/gorm/model"
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
			return false
		}
	},
}

type Handler struct {
	hub *Hub
	rm  *RoomManager
	rr  r.RoomRepository
	ur  r.UserRepository
	log *zap.Logger
}

func NewHandler(hub *Hub, rm *RoomManager, rr r.RoomRepository, ur r.UserRepository, log *zap.Logger) *Handler {
	return &Handler{
		hub: hub,
		rm:  rm,
		rr:  rr,
		ur:  ur,
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
	// SET presence
	uidHex := hex.EncodeToString(user.ID)
	_ = h.hub.rdb.Set(
		context.Background(),
		presenceKey(uidHex), 1,
		presenceTTL,
	).Err()
	// Tạo client mới và đăng ký vào hub
	client := NewClient(user.ID, conn, h.hub, h.rm, h.hub.rdb, convIDs, h.log)
	h.hub.Register(client, convIDs)

	go client.writePump()
	client.readPump()

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = h.ur.UpdateLastSeenAt(bgCtx, user.ID)
	}()
}

func getCurrentUser(c *gin.Context) (*model.User, bool) {
	val, exists := c.Get("user")
	if !exists {
		return nil, false
	}
	user, ok := val.(*model.User)
	return user, ok
}
