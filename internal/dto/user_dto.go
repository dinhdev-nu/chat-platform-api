package dto

import "github.com/dinhdev-nu/chat-platform-api/pkg/types"

type UpdateUserRequest struct {
	Name      string  `json:"name" binding:"omitempty,max=255"`
	AvatarURL *string `json:"avatarUrl" binding:"omitempty,url,max=255"`
	Bio       *string `json:"bio" binding:"omitempty,max=500"`
}

type SendContactRequest struct {
	TargetUserID types.HexID `json:"targetUserId" binding:"required"`
}

type AcceptContactRequest struct {
	SenderUserID types.HexID `json:"senderUserId" binding:"required"`
}
