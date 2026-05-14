package model

import (
	"time"
)

// ContactStatus định nghĩa trạng thái kết bạn
type ContactStatus int8

const (
	ContactStatusPending  ContactStatus = 1 // Chờ xác nhận
	ContactStatusAccepted ContactStatus = 2 // Đã kết bạn
	ContactStatusBlocked  ContactStatus = 3 // Đã chặn
)

const TableNameGoDBUserContact = "user_contacts"

// UserContact ánh xạ bảng user_contacts
type UserContact struct {
	ID        uint64        `gorm:"column:id;primaryKey;autoIncrement"                             json:"id"`
	UserID    []byte        `gorm:"column:user_id;type:binary(16);not null;index:uq_contact_pair,unique,priority:1;index:idx_contact_status,priority:1" json:"user_id"`
	ContactID []byte        `gorm:"column:contact_id;type:binary(16);not null;index:uq_contact_pair,unique,priority:2;index:idx_contact_id"            json:"contact_id"`
	Status    ContactStatus `gorm:"column:status;type:tinyint;not null;default:1;index:idx_contact_status,priority:2"                                  json:"status"`
	CreatedAt time.Time     `gorm:"column:created_at;not null;autoCreateTime"                      json:"created_at"`
	UpdatedAt time.Time     `gorm:"column:updated_at;not null;autoUpdateTime"                      json:"updated_at"`

	// Associations
	User    *User `gorm:"foreignKey:UserID;references:ID;constraint:OnDelete:CASCADE"    json:"user,omitempty"`
	Contact *User `gorm:"foreignKey:ContactID;references:ID;constraint:OnDelete:CASCADE" json:"contact,omitempty"`
}

// TableName chỉ định tên bảng tường minh
func (UserContact) TableName() string {
	return TableNameGoDBUserContact
}
