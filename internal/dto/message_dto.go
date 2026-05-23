package dto

import "github.com/dinhdev-nu/chat-platform-api/pkg/types"

type SendMessageRequest struct {
	Content     string              `json:"content" binding:"omitempty,max=65536"`
	Type        int8                `json:"type" binding:"required,min=1,max=5"`
	ParentID    types.HexID         `json:"parent_id,omitempty" binding:"omitempty"`
	Attachments []AttachmentRequest `json:"attachments,omitempty" binding:"omitempty,max=10,dive"`
}
type EditMessageRequest struct {
	Content string `json:"content" binding:"required,min=1,max=65536"`
}

type ToggleReactionRequest struct {
	Emoji string `json:"emoji" binding:"required,min=1,max=10"`
}

type AttachmentRequest struct {
	FileURL       string `json:"file_url"        binding:"required,max=512"`
	FileName      string `json:"file_name"       binding:"required,max=255"`
	MimeType      string `json:"mime_type"       binding:"required,max=100"`
	FileSizeBytes int64  `json:"file_size_bytes" binding:"required,min=1"`
	Width         *int   `json:"width,omitempty"`
	Height        *int   `json:"height,omitempty"`
	DurationSec   *int   `json:"duration_sec,omitempty"`
}

type MarkAsReadRequest struct {
	LastReadMsgID types.HexID `json:"last_read_msg_id" binding:"required"`
}

// Response DTOs
type AttachmentResponse struct {
	ID            string `json:"id"`
	MessageID     string `json:"message_id,omitempty"`
	FileName      string `json:"file_name"`
	FileURL       string `json:"file_url"`
	MimeType      string `json:"mime_type"`
	FileSizeBytes int64  `json:"file_size_bytes"`
	Width         *int   `json:"width,omitempty"`
	Height        *int   `json:"height,omitempty"`
	DurationSec   *int   `json:"duration_sec,omitempty"`
	CreatedAt     string `json:"created_at"`
}

type MessageReactionResponse struct {
	ID        uint64 `json:"id"`
	MessageID string `json:"message_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Emoji     string `json:"emoji"`
	CreatedAt string `json:"created_at"`
}

type MessageResponse struct {
	ID               string                    `json:"id"`
	ConversationID   string                    `json:"conversation_id"`
	SenderID         string                    `json:"sender_id"`
	ParentID         *string                   `json:"parent_id,omitempty"`
	Type             int8                      `json:"type"`
	Content          *string                   `json:"content,omitempty"`
	ContentEncrypted bool                      `json:"content_encrypted"`
	Iv               *string                   `json:"iv,omitempty"`
	Seq              uint64                    `json:"seq"`
	IsEdited         bool                      `json:"is_edited"`
	IsDeleted        bool                      `json:"is_deleted"`
	DeletedAt        *string                   `json:"deleted_at,omitempty"`
	CreatedAt        string                    `json:"created_at"`
	UpdatedAt        string                    `json:"updated_at"`
	Attachments      []AttachmentResponse      `json:"attachments,omitempty"`
	Reactions        []MessageReactionResponse `json:"reactions,omitempty"`
}
