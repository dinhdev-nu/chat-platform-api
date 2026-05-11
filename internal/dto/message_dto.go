package dto

type SendMessageRequest struct {
	Content     string              `json:"content"`
	Type        int8                `json:"type" validate:"required,min=1,max=5"`
	ParentID    []byte              `json:"parent_id,omitempty" validate:"omitempty,len=32"`
	Attachments []AttachmentRequest `json:"attachments,omitempty" validate:"dive"`
}
type EditMessageRequest struct {
	Content string `json:"content,omitempty"`
}

type TogglePinRequest struct {
	Emoji string `json:"content" binding:"required,min=1,max=65536"`
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
	LastReadMsgID []byte `json:"last_read_msg_id" validate:"required,len=32"`
}
