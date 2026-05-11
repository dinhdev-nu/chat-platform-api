package model

import "time"

type MessageType int8

const (
	MessageTypeText   MessageType = 1
	MessageTypeImage  MessageType = 2
	MessageTypeFile   MessageType = 3
	MessageTypeAudio  MessageType = 4
	MessageTypeVideo  MessageType = 5
	MessageTypeSystem MessageType = 6
)

type Message struct {
	ID               []byte
	ConversationID   []byte
	SenderID         []byte
	ParentID         []byte
	Type             MessageType
	Content          *string
	ContentEncrypted bool
	Iv               *string
	Seq              uint64
	IsEdited         bool
	IsDeleted        bool
	DeletedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (m Message) IsText() bool  { return m.Type == MessageTypeText }
func (m Message) IsImage() bool { return m.Type == MessageTypeImage }
func (m Message) IsFile() bool  { return m.Type == MessageTypeFile }
func (m Message) CanEdit(userID []byte) bool {
	return !m.IsDeleted && m.SenderID != nil && string(m.SenderID) == string(userID)
}
func (m Message) CanDelete(userID []byte) bool {
	return !m.IsDeleted && m.SenderID != nil && string(m.SenderID) == string(userID)
}

type Attachment struct {
	ID            []byte
	MessageID     []byte
	Filename      string
	FileURL       string
	MimeType      string
	FileSizeBytes int64
	Width         *int
	Height        *int
	DurationSec   *int
	CreatedAt     time.Time
}

type MessageReaction struct {
	ID        uint64
	MessageID []byte
	UserID    []byte
	Emoji     string
	CreatedAt time.Time
}

type MessageStatus struct {
	MessageID []byte
	UserID    []byte
	ReadAt    time.Time
}

type MessageWithMeta struct {
	*Message
	Attachments []*Attachment
	Reactions   []*MessageReaction
}
