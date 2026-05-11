package handler

import (
	"strconv"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
)

type RoomHandler struct {
	rs *s.RoomService
}

func NewRoomHandler(rs *s.RoomService) *RoomHandler {
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
		r.OK(c, conv, "DM conversation already exists")
		return
	}
	r.Created(c, conv, "DM conversation created successfully")
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
	r.Created(c, conv, "Group conversation created successfully")
}

func (h *RoomHandler) ListConversations(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ae.Unauthorized("Unauthorized"))
		return
	}
	cursor := c.Query("cursor")
	limitStr := c.DefaultQuery("limit", "20")
	limit, _ := strconv.Atoi(limitStr)
	convs, err := h.rs.ListConversations(c.Request.Context(), user.ID, &cursor, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.Paginated(c, convs, &r.Pagination{
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
	roomID, err := crypto.ParseHexToBytes(c.Query("id"))
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid room ID"))
		return
	}
	var req dto.AddMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ae.ValidationError(err.Error()))
		return
	}
	err = h.rs.AddMember(c.Request.Context(), roomID, user.ID, req.UserID)
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
	roomID, err := crypto.ParseHexToBytes(c.Query("id"))
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid room ID"))
		return
	}
	targetUserID, err := crypto.ParseHexToBytes(c.Query("user_id"))
	if err != nil {
		_ = c.Error(ae.ValidationError("Invalid user ID"))
		return
	}
	err = h.rs.RemoveMember(c.Request.Context(), roomID, user.ID, targetUserID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.NoContent(c)
}
