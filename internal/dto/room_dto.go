package dto

import "github.com/dinhdev-nu/chat-platform-api/pkg/types"

type CreateDMRequest struct {
	TargetUID types.HexID `json:"target_user_id" validate:"required"`
}

type CreateGroupRequest struct {
	Name       string        `json:"name" validate:"required,min=1,max=100"`
	Type       int8          `json:"type" validate:"required,oneof=2 3"` // 2: Group, 3: Channel
	MemberUIDs []types.HexID `json:"member_user_ids" validate:"required,min=1,max=499,dive"`
}

type AddMembersRequest struct {
	UserID types.HexID `json:"user_id" validate:"required"`
}

type CreateRoomResponse struct {
	ID             string  `json:"id"`
	Type           int8    `json:"type"`
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	AvatarURL      *string `json:"avatar_url,omitempty"`
	CreateBy       *string `json:"created_by,omitempty"`
	LastMessageID  *string `json:"last_message_id,omitempty"`
	LastActivityAt *string `json:"last_activity_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

type ConversationListItem struct {
	ID             string  `json:"id"`
	Type           int8    `json:"type"` // 1: DM, 2: Group, 3: Channel
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	AvatarURL      *string `json:"avatar_url,omitempty"`
	CreateBy       *string `json:"created_by,omitempty"`
	LastMessageID  *string `json:"last_message_id,omitempty"`
	LastActivityAt *string `json:"last_activity_at,omitempty"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`

	Role        int8  `json:"role"`
	IsMuted     bool  `json:"is_muted"`
	UnreadCount int64 `json:"unread_count"`
}
