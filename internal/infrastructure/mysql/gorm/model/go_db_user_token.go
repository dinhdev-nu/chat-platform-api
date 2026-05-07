package model

import (
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

const TableNameGoDBUserToken = "user_tokens"

type UserToken struct {
	ID         uint64    `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID     []byte    `gorm:"column:user_id;type:binary(16);not null;index:idx_token_user;index:idx_token_last_used,priority:1;uniqueIndex:uq_token_device,priority:1;constraint:fk_token_user,OnDelete:CASCADE" json:"user_id"`
	JTI        []byte    `gorm:"column:jti;type:binary(16);not null;uniqueIndex:uq_token_jti" json:"jti"`
	DeviceID   []byte    `gorm:"column:device_id;type:binary(16);not null;uniqueIndex:uq_token_device,priority:2" json:"device_id"`
	DeviceName *string   `gorm:"column:device_name;type:varchar(200);default:null" json:"device_name,omitempty"`
	IPAddress  *string   `gorm:"column:ip_address;type:varchar(45);default:null" json:"ip_address,omitempty"`
	ExpiresAt  time.Time `gorm:"column:expires_at;type:datetime;not null;index:idx_token_expires" json:"expires_at"`
	LastUsedAt time.Time `gorm:"column:last_used_at;type:datetime;not null;default:CURRENT_TIMESTAMP;autoUpdateTime;index:idx_token_last_used,priority:2" json:"last_used_at"`
	CreatedAt  time.Time `gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;autoCreateTime" json:"created_at"`
}

func (UserToken) TableName() string {
	return TableNameGoDBUserToken
}

func (t *UserToken) ToDomain() *model.UserToken {
	return &model.UserToken{
		ID:         t.ID,
		UserID:     t.UserID,
		JTI:        t.JTI,
		DeviceID:   t.DeviceID,
		DeviceName: t.DeviceName,
		IPAddress:  t.IPAddress,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
		CreatedAt:  t.CreatedAt,
	}
}

func UserTokenFromDomain(t *model.UserToken) *UserToken {
	return &UserToken{
		ID:         t.ID,
		UserID:     t.UserID,
		JTI:        t.JTI,
		DeviceID:   t.DeviceID,
		DeviceName: t.DeviceName,
		IPAddress:  t.IPAddress,
		ExpiresAt:  t.ExpiresAt,
		LastUsedAt: t.LastUsedAt,
		CreatedAt:  t.CreatedAt,
	}
}
