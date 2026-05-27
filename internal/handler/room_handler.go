package handler

import (
	"encoding/hex"
	"strconv"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
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
		res := convToCreateDTO(conv)
		r.OK(c, &res, "DM conversation already exists")
		return
	}
	res := convToCreateDTO(conv)
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
	res := convToCreateDTO(conv)
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
		items = append(items, convRowToListDTO(row))
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

func convToCreateDTO(c *model.Conversation) dto.CreateRoomResponse {
	var name, desc, avatar, createdBy, lastMsg, lastAct *string

	// Name: nil or pointer
	if c.Name != nil {
		name = c.Name
	}

	// Description: nil or pointer
	if c.Description != nil {
		desc = c.Description
	}

	// AvatarURL: nil or pointer
	if c.AvatarURL != nil {
		avatar = c.AvatarURL
	}

	// CreatedBy: nil if empty, else pointer to hex string
	if len(c.CreatedBy) > 0 {
		cbStr := hex.EncodeToString(c.CreatedBy)
		createdBy = &cbStr
	}

	// LastMessageID: nil if empty, else pointer to hex string
	if len(c.LastMessageID) > 0 {
		lmStr := hex.EncodeToString(c.LastMessageID)
		lastMsg = &lmStr
	}

	// LastActivityAt: nil or pointer to RFC3339 string
	if c.LastActivityAt != nil {
		laStr := c.LastActivityAt.Format(time.RFC3339)
		lastAct = &laStr
	}

	return dto.CreateRoomResponse{
		ID:              hex.EncodeToString(c.ID),
		Type:            int8(c.Type),
		Name:            name,
		Description:     desc,
		AvatarURL:       avatar,
		CreateBy:        createdBy,
		LastMessageID:   lastMsg,
		LastMessageText: c.LastMessageText,
		LastActivityAt:  lastAct,
		CreatedAt:       c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       c.UpdatedAt.Format(time.RFC3339),
	}
}

func convRowToListDTO(rw *model.ConversationListRow) dto.ConversationListItem {
	c := rw.Conversation
	base := convToCreateDTO(&c)
	return dto.ConversationListItem{
		ID:                base.ID,
		Type:              base.Type,
		Name:              base.Name,
		Description:       base.Description,
		AvatarURL:         base.AvatarURL,
		CreateBy:          base.CreateBy,
		LastMessageID:     base.LastMessageID,
		LastMessageText:   base.LastMessageText,
		LastActivityAt:    base.LastActivityAt,
		CreatedAt:         base.CreatedAt,
		UpdatedAt:         base.UpdatedAt,
		Role:              int8(rw.Role),
		IsMuted:           rw.IsMuted,
		UnreadCount:       rw.UnreadCount,
		MemberOnlineCount: rw.MemberOnlineCount,
		IsOnline:          rw.IsOnline,
	}
}
