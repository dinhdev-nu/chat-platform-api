package model

import (
	"time"

	"gorm.io/datatypes"
)

type OAuthProvider string

const (
	OAuthProviderGoogle   OAuthProvider = "google"
	OAuthProviderFacebook OAuthProvider = "facebook"
	OAuthProviderGitHub   OAuthProvider = "github"
	OAuthProviderApple    OAuthProvider = "apple"
)

const TableNameGoDBOAuthAccount = "oauth_accounts"

type OAuthAccount struct {
	ID          uint64         `gorm:"column:id;type:bigint unsigned;primaryKey;autoIncrement" json:"id"`
	UserID      []byte         `gorm:"column:user_id;type:binary(16);not null;index:idx_oauth_user;constraint:fk_oauth_user,OnDelete:CASCADE" json:"user_id"`
	Provider    OAuthProvider  `gorm:"column:provider;type:varchar(50);not null;uniqueIndex:uq_oauth_provider_uid" json:"provider"`
	ProviderUID string         `gorm:"column:provider_uid;type:varchar(255);not null;uniqueIndex:uq_oauth_provider_uid" json:"provider_uid"`
	RawProfile  datatypes.JSON `gorm:"column:raw_profile;type:json;default:null" json:"raw_profile,omitempty"`
	CreatedAt   time.Time      `gorm:"column:created_at;type:datetime;not null;autoCreateTime" json:"created_at"`
	User        *User          `gorm:"foreignKey:UserID;references:ID" json:"-"`
}

func (OAuthAccount) TableName() string {
	return TableNameGoDBOAuthAccount
}
