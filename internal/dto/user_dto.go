package dto

import "github.com/dinhdev-nu/chat-platform-api/pkg/types"

type UpdateUserRequest struct {
	Name      *string `json:"name" binding:"omitempty,max=50"`
	AvatarURL *string `json:"avatarUrl" binding:"omitempty,max=512"`
	Bio       *string `json:"bio" binding:"omitempty,max=300"`
}

type SendContactRequest struct {
	TargetUserID types.HexID `json:"targetUserId" binding:"required"`
}

type SendContactRequestResponse struct {
	Status string `json:"status"`
}

type AcceptContactRequest struct {
	SenderUserID types.HexID `json:"senderUserId" binding:"required"`
}
