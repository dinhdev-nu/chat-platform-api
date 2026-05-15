package handler

import (
	"strconv"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
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

	var req dto.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ar.ValidationError(err.Error()))
		return
	}
	if req.Name == "" && req.AvatarURL == nil && req.Bio == nil {
		_ = c.Error(ar.ValidationError("At least one field must be provided"))
		return
	}

	user, err := h.userService.UpdateUser(c.Request.Context(), user.ID, req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	res := user.ToUserResponse()
	r.OK(c, &res, "User updated successfully")
}

func (h *UserHandler) Me(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}
	res := user.ToUserResponse()
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
	cursor = c.Query("cursor")
	var cursorPtr *string
	if cursor != "" {
		cursorPtr = &cursor
	}
	res, err := h.userService.Search(c.Request.Context(), user.ID, q, cursorPtr, limit)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.Paginated(c, &res.Items, &r.Pagination{
		NextCursor: *res.NextCursor,
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
	err := h.userService.SendContactRequest(c.Request.Context(), user.ID, req.TargetUserID)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.NoContent(c)
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
	r.Paginated(c, &res.Items, &r.Pagination{
		NextCursor: *res.NextCursor,
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
	r.Paginated(c, &res.Items, &r.Pagination{
		NextCursor: *res.NextCursor,
		HasMore:    res.HasMore,
		Limit:      limit,
	}, "Incoming contact requests retrieved successfully")
}
