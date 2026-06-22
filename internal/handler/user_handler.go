package handler

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type UserHandler struct {
	userService *s.UserService
}

func NewUserHandler(us *s.UserService) *UserHandler {
	return &UserHandler{userService: us}
}

func (h *UserHandler) Update(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}

	patch, err := buildUserProfileUpdate(c)
	if err != nil {
		_ = c.Error(err)
		return
	}

	updatedUser, err := h.userService.UpdateUser(c.Request.Context(), user.ID, patch)
	if err != nil {
		_ = c.Error(err)
		return
	}

	res := dto.UserResponseFromModel(updatedUser)
	r.OK(c, &res, "User updated successfully")
}

func buildUserProfileUpdate(c *gin.Context) (*model.UserProfileUpdate, error) {
	var req dto.UpdateUserRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		return nil, ar.ValidationError(err.Error())
	}

	var raw map[string]json.RawMessage
	if err := c.ShouldBindBodyWith(&raw, binding.JSON); err != nil {
		return nil, ar.ValidationError(err.Error())
	}

	_, hasName := raw["name"]
	rawAvatar, hasAvatar := raw["avatarUrl"]
	rawBio, hasBio := raw["bio"]
	if !hasName && !hasAvatar && !hasBio {
		return nil, ar.ValidationError("At least one field must be provided")
	}

	patch := &model.UserProfileUpdate{
		HasName:   hasName,
		HasAvatar: hasAvatar,
		HasBio:    hasBio,
	}

	if hasName {
		if req.Name == nil {
			return nil, ar.ValidationError("name cannot be empty")
		}
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ar.ValidationError("name cannot be empty")
		}
		patch.Name = &name
	}

	if hasAvatar {
		if isJSONNull(rawAvatar) || req.AvatarURL == nil {
			return nil, ar.ValidationError("avatarUrl cannot be empty")
		}
		avatar := strings.TrimSpace(*req.AvatarURL)
		if avatar == "" {
			return nil, ar.ValidationError("avatarUrl cannot be empty")
		}
		patch.AvatarURL = &avatar
	}

	if hasBio {
		if isJSONNull(rawBio) || req.Bio == nil {
			return nil, ar.ValidationError("bio cannot be empty")
		}
		bio := strings.TrimSpace(*req.Bio)
		if bio == "" {
			return nil, ar.ValidationError("bio cannot be empty")
		}
		patch.Bio = &bio
	}

	return patch, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func (h *UserHandler) Me(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}
	res := dto.UserResponseFromModel(user)
	r.OK(c, &res, "User info retrieved successfully")
}

func (h *UserHandler) Search(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}
	q := c.Query("q")
	cursor := c.Query("cursor")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		_ = c.Error(ar.ValidationError("Invalid limit parameter"))
		return
	}
	var cursorPtr *string
	if cursor != "" {
		cursorPtr = &cursor
	}
	res, err := h.userService.Search(c.Request.Context(), user.ID, q, cursorPtr, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	nextCursor := ""
	if res.NextCursor != nil {
		nextCursor = *res.NextCursor
	}
	r.Paginated(c, &res.Items, &r.Pagination{
		NextCursor: nextCursor,
		HasMore:    res.HasMore,
		Limit:      limit,
	}, "Search results retrieved successfully")
}

func (h *UserHandler) SendContactRequest(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}
	var req dto.SendContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ar.ValidationError(err.Error()))
		return
	}
	status, err := h.userService.SendContactRequest(c.Request.Context(), user.ID, req.TargetUserID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	res := dto.SendContactRequestResponse{Status: string(status)}
	if status == model.ContactRequestResultAccepted {
		r.OK(c, &res, "Contact request accepted")
		return
	}
	r.Created(c, &res, "Contact request sent successfully")
}

func (h *UserHandler) AcceptContactRequest(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}

	var req dto.AcceptContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ar.ValidationError(err.Error()))
		return
	}
	err := h.userService.AcceptContactRequest(c.Request.Context(), user.ID, req.SenderUserID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.NoContent(c)
}
func (h *UserHandler) GetContacts(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}

	cursor := c.Query("cursor")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		_ = c.Error(ar.ValidationError("Invalid limit parameter"))
		return
	}
	var cursorPtr *string
	if cursor != "" {
		cursorPtr = &cursor
	}

	res, err := h.userService.GetContacts(c.Request.Context(), user.ID, cursorPtr, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	nextCursor := ""
	if res.NextCursor != nil {
		nextCursor = *res.NextCursor
	}
	r.Paginated(c, &res.Items, &r.Pagination{
		NextCursor: nextCursor,
		HasMore:    res.HasMore,
		Limit:      limit,
	}, "Contacts retrieved successfully")
}

func (h *UserHandler) GetIncomingRequests(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}
	cursor := c.Query("cursor")
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if err != nil {
		_ = c.Error(ar.ValidationError("Invalid limit parameter"))
		return
	}
	var cursorPtr *string
	if cursor != "" {
		cursorPtr = &cursor
	}
	res, err := h.userService.GetIncomingContactRequests(c.Request.Context(), user.ID, cursorPtr, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	nextCursor := ""
	if res.NextCursor != nil {
		nextCursor = *res.NextCursor
	}
	r.Paginated(c, &res.Items, &r.Pagination{
		NextCursor: nextCursor,
		HasMore:    res.HasMore,
		Limit:      limit,
	}, "Incoming contact requests retrieved successfully")
}
