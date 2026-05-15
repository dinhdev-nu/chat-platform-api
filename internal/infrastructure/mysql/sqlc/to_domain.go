package sqlc

import (
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

func (c Conversation) ToDomain() *model.Conversation {
	cm := &model.Conversation{
		ID:             c.ID,
		Type:           model.ConversationType(c.Type),
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
		LastActivityAt: c.LastActivityAt,
	}
	if c.Name.Valid {
		cm.Name = &c.Name.String
	}
	if c.AvatarUrl.Valid {
		cm.AvatarURL = &c.AvatarUrl.String
	}
	if c.Description.Valid {
		cm.Description = &c.Description.String
	}
	if c.CreatedBy.Valid {
		cm.CreatedBy = []byte(c.CreatedBy.String)
	}
	if c.LastMessageID.Valid {
		cm.LastMessageID = []byte(c.LastMessageID.String)
	}

	return cm
}

func (m ConversationMember) ToDomain() *model.ConversationMember {
	return &model.ConversationMember{
		ID:             int64(m.ID),
		ConversationID: m.ConversationID,
		UserID:         m.UserID,
		Role:           model.MemberRole(m.Role),
		IsMuted:        m.IsMuted,
		LastReadAt:     m.LastReadAt,
		JoinedAt:       m.JoinedAt,
	}
}

func (m Message) ToDomain() *model.Message {
	msg := &model.Message{
		ID:               m.ID,
		ConversationID:   m.ConversationID,
		SenderID:         m.SenderID,
		Type:             model.MessageType(m.Type),
		ContentEncrypted: m.ContentEncrypted,
		Seq:              m.Seq,
		IsEdited:         m.IsEdited,
		IsDeleted:        m.IsDeleted,
		DeletedAt:        m.DeletedAt,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
	if m.Content.Valid {
		msg.Content = &m.Content.String
	}
	if m.Iv.Valid {
		msg.Iv = &m.Iv.String
	}
	if m.ParentID.Valid {
		msg.ParentID = []byte(m.ParentID.String)
	}
	return msg
}

func (a Attachment) ToDomain() *model.Attachment {
	att := &model.Attachment{
		ID:            a.ID,
		MessageID:     a.MessageID,
		Filename:      a.FileName,
		FileURL:       a.FileUrl,
		MimeType:      a.MimeType,
		FileSizeBytes: int64(a.FileSizeBytes),
		CreatedAt:     a.CreatedAt,
	}
	if a.Width.Valid {
		w := int(a.Width.Int32)
		att.Width = &w
	}
	if a.Height.Valid {
		h := int(a.Height.Int32)
		att.Height = &h
	}
	if a.DurationSec.Valid {
		d := int(a.DurationSec.Int16)
		att.DurationSec = &d
	}
	return att
}

func (r MessageReaction) ToDomain() *model.MessageReaction {
	return &model.MessageReaction{
		ID:        r.ID,
		MessageID: r.MessageID,
		UserID:    r.UserID,
		Emoji:     r.Emoji,
		CreatedAt: r.CreatedAt,
	}
}
