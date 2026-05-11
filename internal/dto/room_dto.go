package dto

type CreateDMRequest struct {
	TargetUID []byte `json:"target_user_id" validate:"required,len=32"`
}

type CreateGroupRequest struct {
	Name       string   `json:"name" validate:"required,min=1,max=100"`
	Type       int8     `json:"type" validate:"required,oneof=2 3"` // 2: Group, 3: Channel
	MemberUIDs [][]byte `json:"member_user_ids" validate:"required,min=1,max=499,dive,len=32"`
}

type AddMembersRequest struct {
	UserID []byte `json:"user_id" validate:"required,len=32"`
}
