package model

import "time"

type ConversationType int8
type MemberRole int8

const (
	ConvTypeDirect  ConversationType = 1
	ConvTypeGroup   ConversationType = 2
	ConvTypeChannel ConversationType = 3

	RoleOwner  MemberRole = 1
	RoleAdmin  MemberRole = 2
	RoleMember MemberRole = 3
)

type Conversation struct {
	ID              []byte
	Type            ConversationType
	Name            *string
	AvatarURL       *string
	Description     *string
	CreatedBy       []byte // nil if creator deleted account
	LastMessageID   []byte // nil if no messages yet
	LastMessageText *string
	LastActivityAt  *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type ConversationMember struct {
	ID             int64
	ConversationID []byte
	UserID         []byte
	Role           MemberRole
	IsMuted        bool
	LastReadAt     *time.Time
	JoinedAt       time.Time
}

type ConversationListRow struct {
	Conversation
	Role        MemberRole
	IsMuted     bool
	UnreadCount int64
}

func (c Conversation) IsDirect() bool      { return c.Type == ConvTypeDirect }
func (c Conversation) IsGroup() bool       { return c.Type == ConvTypeGroup }
func (m ConversationMember) IsOwner() bool { return m.Role == RoleOwner }
func (m ConversationMember) IsAdmin() bool { return m.Role == RoleAdmin }
func (m ConversationMember) CanManage() bool {
	return m.Role == RoleOwner || m.Role == RoleAdmin
}
