package handler

import (
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

	res, err := h.userService.UpdateUser(c.Request.Context(), user.ID, req)
	if err != nil {
		_ = c.Error(err)
		return
	}

	r.OK(c, res, "User updated successfully")
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
