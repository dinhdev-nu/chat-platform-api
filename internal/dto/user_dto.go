package dto

type UpdateUserRequest struct {
	Name      string  `json:"name" binding:"required,max=255"`
	AvatarURL *string `json:"avatarUrl" binding:"omitempty,url,max=255"`
	Bio       *string `json:"bio" binding:"omitempty,max=500"`
}
