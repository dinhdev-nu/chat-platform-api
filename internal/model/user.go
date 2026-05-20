package model

import (
	"encoding/hex"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"gorm.io/datatypes"
)

type UserStatus int8
type OAuthProvider string

const (
	UserStatusActive      UserStatus = 1
	UserStatusSuspended   UserStatus = 2
	UserStatusDeactivated UserStatus = 3
)
const (
	OAuthProviderGoogle   OAuthProvider = "google"
	OAuthProviderFacebook OAuthProvider = "facebook"
	OAuthProviderGitHub   OAuthProvider = "github"
	OAuthProviderApple    OAuthProvider = "apple"
)

type User struct {
	ID         []byte
	Username   string
	Email      string
	AvatarURL  *string
	Bio        *string
	Status     UserStatus
	LastSeenAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

func (u *User) IsSuspended() bool {
	return u.Status == UserStatusSuspended
}

func (u *User) ToUserResponse() dto.UserResponse {
	return dto.UserResponse{
		ID:        hex.EncodeToString(u.ID),
		Email:     u.Email,
		Name:      u.Username,
		AvatarURL: u.AvatarURL,
		Bio:       u.Bio,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

type UserToken struct {
	ID         uint64
	UserID     []byte
	JTI        []byte
	DeviceID   []byte
	DeviceName *string
	IPAddress  *string
	ExpiresAt  time.Time
	LastUsedAt time.Time
	CreatedAt  time.Time
}

func (t *UserToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

type OAuthAccount struct {
	ID          uint64
	UserID      []byte
	Provider    OAuthProvider
	ProviderUID string
	RawProfile  datatypes.JSON
	CreatedAt   time.Time
	User        *User
}

type ContactStatus int8

const (
	ContactStatusPending  ContactStatus = 1 // Chờ xác nhận
	ContactStatusAccepted ContactStatus = 2 // Đã kết bạn
	ContactStatusBlocked  ContactStatus = 3 // Đã chặn
)

const TableNameGoDBUserContact = "user_contacts"

// UserContact ánh xạ bảng user_contacts
type UserContact struct {
	ID        uint64
	UserID    []byte
	ContactID []byte
	Status    ContactStatus
	CreatedAt time.Time
	UpdatedAt time.Time

	// Associations
	User    *User
	Contact *User
}

type SearchUser struct {
	ID             string         `json:"id"`
	Username       string         `json:"username"`
	AvatarURL      *string        `json:"avatar_url"`
	Bio            *string        `json:"bio"`
	LastSeenAt     *time.Time     `json:"last_seen_at"`
	OutgoingStatus *ContactStatus `json:"outgoing_status,omitempty"`
	IncomingStatus *ContactStatus `json:"incoming_status,omitempty"`
}
