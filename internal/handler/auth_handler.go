package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	m "github.com/dinhdev-nu/chat-platform-api/internal/middleware"
	ar "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	r "github.com/dinhdev-nu/chat-platform-api/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/ua-parser/uap-go/uaparser"
)

type authService interface {
	SendOTP(ctx context.Context, req dto.SendOTPRequest) (*dto.SendOTPResponse, error)
	VerifyOTP(ctx context.Context, req dto.VerifyOTPRequest, ip string) (*dto.LoginResponse, error)
	Logout(ctx context.Context, jti []byte) error
}

type AuthHandler struct {
	authService authService
}

func NewAuthHandler(as authService) *AuthHandler {
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
	if strings.TrimSpace(req.DeviceName) == "" {
		req.DeviceName = h.getDeviceName(c)
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
	fmt.Printf("Logout request with JTI: %s\n", jti)

	err := h.authService.Logout(c.Request.Context(), jti)
	if err != nil {
		_ = c.Error(err)
		return
	}

	r.NoContent(c)
}

func (h *AuthHandler) getDeviceName(c *gin.Context) string {
	ag := c.GetHeader("User-Agent")
	if ag == "" {
		return "Unknown Device"
	}

	client := uaparser.NewFromSaved().Parse(ag)

	device := client.Device.Family
	if device == "Other" {
		device = "PC"
	}

	return fmt.Sprintf("%s, %s (%s)", device, client.Os.Family, client.UserAgent.Family)
}
