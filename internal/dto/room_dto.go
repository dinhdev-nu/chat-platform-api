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
