package dto

import (
	"encoding/hex"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

func UserResponseFromModel(user *model.User) UserResponse {
	if user == nil {
		return UserResponse{}
	}
	return UserResponse{
		ID:        hex.EncodeToString(user.ID),
		Email:     user.Email,
		Name:      user.Username,
		AvatarURL: user.AvatarURL,
		Bio:       user.Bio,
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
	}
}
