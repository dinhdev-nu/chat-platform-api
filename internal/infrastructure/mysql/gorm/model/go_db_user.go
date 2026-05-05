package model

import (
	"time"
)

type UserStatus int8

const (
	UserStatusActive      UserStatus = 1
	UserStatusSuspended   UserStatus = 2
	UserStatusDeactivated UserStatus = 3
)

const TableNameGoDBUser = "users"

type User struct {
	ID         []byte     `gorm:"column:id;type:binary(16);primaryKey;autoIncrement:false" json:"id"`
	Username   string     `gorm:"column:username;type:varchar(50);not null;uniqueIndex:uq_users_username" json:"username"`
	Email      string     `gorm:"column:email;type:varchar(255);not null;uniqueIndex:uq_users_email" json:"email"`
	AvatarURL  *string    `gorm:"column:avatar_url;type:varchar(512);default:null" json:"avatar_url,omitempty"`
	Bio        *string    `gorm:"column:bio;type:varchar(300);default:null" json:"bio,omitempty"`
	Status     UserStatus `gorm:"column:status;type:tinyint;not null;default:1;index:idx_users_status" json:"status"`
	LastSeenAt *time.Time `gorm:"column:last_seen_at;type:datetime;default:null" json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `gorm:"column:created_at;type:datetime;not null;autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;type:datetime;not null;autoUpdateTime" json:"updated_at"`
}

func (User) TableName() string {
	return TableNameGoDBUser
}
