package handler

import (
	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	s "github.com/dinhdev-nu/chat-platform-api/internal/service"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService *s.AuthService
}

func NewAuthHandler(as *s.AuthService) *AuthHandler {
	return &AuthHandler{authService: as}
}

func (h *AuthHandler) SendOTP(c *gin.Context) {
	var req dto.SendOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ar.ValidationError(err.Error()))
		return
	}

	res, err := h.authService.SendOTP(c.Request.Context(), req)
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.OK(c, res, "OTP sent successfully")
}

func (h *AuthHandler) VerifyOTP(c *gin.Context) {
	var req dto.VerifyOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(ar.ValidationError(err.Error()))
		return
	}

	res, err := h.authService.VerifyOTP(c.Request.Context(), req, c.ClientIP())
	if err != nil {
		_ = c.Error(err)
		return
	}
	r.Created(c, res, "OTP verified successfully")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	jti, exists := m.GetCurrentJTI(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}

	err := h.authService.Logout(c.Request.Context(), jti)
	if err != nil {
		_ = c.Error(err)
		return
	}

	r.NoContent(c)
}

func (h *AuthHandler) Me(c *gin.Context) {
	user, exists := m.GetCurrentUser(c)
	if !exists {
		_ = c.Error(ar.Unauthorized("Unauthorized"))
		return
	}

	res := user.ToUserResponse()
	r.OK(c, &res, "User info retrieved successfully")
}
