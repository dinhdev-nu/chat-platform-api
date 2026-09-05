package presenter

import (
	"encoding/hex"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

func User(u *model.User) dto.UserResponse {
	return dto.UserResponse{
		ID:        hex.EncodeToString(u.ID),
		Email:     u.Email,
		Name:      u.Username,
		AvatarURL: u.AvatarURL,
		Bio:       u.Bio,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}
