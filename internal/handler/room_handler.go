package handler

import (
	"context"
	"strconv"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	"github.com/dinhdev-nu/chat-platform-api/internal/presenter"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
)

type roomService interface {
	CreateDM(ctx context.Context, currentUID, targetUserID []byte) (*model.Conversation, bool, error)
	CreateGroup(ctx context.Context, currentUID []byte, req dto.CreateGroupRequest) (*model.Conversation, error)
	ListConversations(ctx context.Context, uid []byte, cursor *string, limit int) (*s.ResultPage[*model.ConversationListRow], error)
	AddMember(ctx context.Context, convUID, actorUID, targetUID []byte, actorName string) error
	RemoveMember(ctx context.Context, convID, actorUID, targetUID []byte, actorName string) error
}

type RoomHandler struct {
	rs roomService
}

func NewRoomHandler(rs roomService) *RoomHandler {
	return &RoomHandler{rs: rs}
}

func (h *RoomHandler) CreateDM(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	var req dto.CreateDMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	conv, exists, err := h.rs.CreateDM(c.Request.Context(), user.ID, req.TargetUID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	if exists {
		res := presenter.CreateRoom(conv)
		r.OK(c, &res, "DM conversation already exists")
		return
	}
	res := presenter.CreateRoom(conv)
	r.Created(c, &res, "DM conversation created successfully")
}

func (h *RoomHandler) CreateGroup(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	var req dto.CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	conv, err := h.rs.CreateGroup(c.Request.Context(), user.ID, req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	res := presenter.CreateRoom(conv)
	r.Created(c, &res, "Group conversation created successfully")
}

func (h *RoomHandler) ListConversations(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	cursor := c.Query("cursor")
	limitStr := c.DefaultQuery("limit", "20")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid limit parameter"))
		return
	}
	convs, err := h.rs.ListConversations(c.Request.Context(), user.ID, &cursor, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	// Map model list to DTO list
	items := make([]dto.ConversationListItem, 0, len(convs.Items))
	for _, row := range convs.Items {
		items = append(items, presenter.ConversationRow(row))
	}
	r.Paginated(c, &items, &r.Pagination{
		Limit:      limit,
		HasMore:    convs.HasMore,
		NextCursor: *convs.NextCursor,
	}, "Conversations retrieved successfully")
}

func (h *RoomHandler) AddMember(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	roomID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid room ID"))
		return
	}
	var req dto.AddMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	err = h.rs.AddMember(c.Request.Context(), roomID, user.ID, req.UserID, user.Username)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.NoContent(c)
}

func (h *RoomHandler) RemoveMember(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	roomID, err := crypto.ParseHexToBytes(c.Param("id"))
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid room ID"))
		return
	}
	targetUserID, err := crypto.ParseHexToBytes(c.Param("user_id"))
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid user ID"))
		return
	}
	err = h.rs.RemoveMember(c.Request.Context(), roomID, user.ID, targetUserID, user.Username)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.NoContent(c)
}
