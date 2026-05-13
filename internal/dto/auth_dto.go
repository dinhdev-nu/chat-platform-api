package dto

import "time"

type SendOTPRequest struct {
	Email string `json:"email" binding:"required,email,max=255"`
}

type SendOTPResponse struct {
	Message   string `json:"message"`
	Email     string `json:"email"`
	ExpiredIn int    `json:"expiresIn"`
}

type VerifyOTPRequest struct {
	Email      string `json:"email" binding:"required,email,max=255"`
	OTP        string `json:"otp" binding:"required,len=6,numeric"`
	DeviceID   string `json:"deviceId" binding:"required,uuid"`
	DeviceName string `json:"deviceName" binding:"omitempty,max=255"`
}

type LoginResponse struct {
	AccessToken string       `json:"accessToken"`
	ExpiresAt   time.Time    `json:"expiresAt"`
	User        UserResponse `json:"user"`
}

type UserResponse struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatarUrl,omitempty"`
	Bio       *string `json:"bio,omitempty"`
	CreatedAt string  `json:"createdAt"`
}
