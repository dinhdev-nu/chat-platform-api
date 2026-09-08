package presenter

import (
	"encoding/hex"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

// Message maps only the message fields. Collection omission is part of the HTTP contract.
func Message(msg *model.Message) dto.MessageResponse {
	if msg == nil {
		return dto.MessageResponse{}
	}
	return dto.MessageResponse{
		ID:               hex.EncodeToString(msg.ID),
		ConversationID:   hex.EncodeToString(msg.ConversationID),
		SenderID:         hex.EncodeToString(msg.SenderID),
		ParentID:         optionalHex(msg.ParentID),
		Type:             int8(msg.Type),
		Content:          msg.Content,
		ContentEncrypted: msg.ContentEncrypted,
		IV:               msg.IV,
		Seq:              msg.Seq,
		IsEdited:         msg.IsEdited,
		IsDeleted:        msg.IsDeleted,
		DeletedAt:        optionalTime(msg.DeletedAt),
		CreatedAt:        msg.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        msg.UpdatedAt.Format(time.RFC3339)}

}

func MessageWithMeta(mm *model.MessageWithMeta) dto.MessageResponse {
	if mm == nil || mm.Message == nil {
		return dto.MessageResponse{}
	}

	out := Message(mm.Message)
	out.SenderName = mm.SenderName
	out.SenderAvatarURL = mm.SenderAvatarURL

	out.Attachments = make([]dto.AttachmentResponse, 0, len(mm.Attachments))
	for _, att := range mm.Attachments {
		out.Attachments = append(out.Attachments, dto.AttachmentResponse{
			ID:            hex.EncodeToString(att.ID),
			MessageID:     hex.EncodeToString(att.MessageID),
			FileName:      att.Filename,
			FileURL:       att.FileURL,
			MIMEType:      att.MIMEType,
			FileSizeBytes: att.FileSizeBytes,
			Width:         att.Width,
			Height:        att.Height,
			DurationSec:   att.DurationSec,
			CreatedAt:     att.CreatedAt.Format(time.RFC3339),
		})
	}

	out.Reactions = make([]dto.MessageReactionResponse, 0, len(mm.Reactions))
	for _, reaction := range mm.Reactions {
		out.Reactions = append(out.Reactions, dto.MessageReactionResponse{
			ID:        reaction.ID,
			MessageID: hex.EncodeToString(reaction.MessageID),
			UserID:    hex.EncodeToString(reaction.UserID),
			Emoji:     reaction.Emoji,
			CreatedAt: reaction.CreatedAt.Format(time.RFC3339),
		})
	}

	return out
}
