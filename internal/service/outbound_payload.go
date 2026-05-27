package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

const deletedUserDisplayName = "Deleted user"

type outboundConversationPayload struct {
	Event        redis.EventType          `json:"event"`
	ConvID       string                   `json:"conv_id"`
	Conversation dto.ConversationListItem `json:"conversation"`
}

type outboundUserSummary struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url,omitempty"`
}

type outboundMemberPayload struct {
	Event        redis.EventType           `json:"event"`
	ConvID       string                    `json:"conv_id"`
	UserID       string                    `json:"user_id"`
	User         *outboundUserSummary      `json:"user,omitempty"`
	Actor        *outboundUserSummary      `json:"actor,omitempty"`
	Conversation *dto.ConversationListItem `json:"conversation,omitempty"`
}

type outboundMessageNewPayload struct {
	Event    redis.EventType     `json:"event"`
	ConvID   string              `json:"conv_id"`
	MsgID    string              `json:"msg_id"`
	SenderID string              `json:"sender_id"`
	Seq      uint64              `json:"seq"`
	Type     int8                `json:"type"`
	Message  dto.MessageResponse `json:"message"`
}

type outboundMessageEditedPayload struct {
	Event    redis.EventType      `json:"event"`
	ConvID   string               `json:"conv_id"`
	MsgID    string               `json:"msg_id"`
	Content  string               `json:"content"`
	EditedAt string               `json:"edited_at"`
	Message  *dto.MessageResponse `json:"message,omitempty"`
}

type outboundMessageDeletedPayload struct {
	Event     redis.EventType `json:"event"`
	ConvID    string          `json:"conv_id"`
	MsgID     string          `json:"msg_id"`
	IsDeleted bool            `json:"is_deleted"`
	DeletedAt string          `json:"deleted_at"`
}

type outboundMessageReadPayload struct {
	Event         redis.EventType `json:"event"`
	ConvID        string          `json:"conv_id"`
	UserID        string          `json:"user_id"`
	LastReadMsgID string          `json:"last_read_msg_id"`
	ReadAt        string          `json:"read_at"`
}

type outboundReactionPayload struct {
	Event  redis.EventType `json:"event"`
	ConvID string          `json:"conv_id"`
	MsgID  string          `json:"msg_id"`
	UserID string          `json:"user_id"`
	Emoji  string          `json:"emoji"`
	Action string          `json:"action"`
}

func conversationCreatedPayload(item dto.ConversationListItem) json.RawMessage {
	return marshalRaw(outboundConversationPayload{
		Event:        redis.EventConvCreated,
		ConvID:       item.ID,
		Conversation: item,
	})
}

func memberPayload(event redis.EventType, convID, userID []byte, user, actor *outboundUserSummary, conversation *dto.ConversationListItem) json.RawMessage {
	return marshalRaw(outboundMemberPayload{
		Event:        event,
		ConvID:       hex.EncodeToString(convID),
		UserID:       hex.EncodeToString(userID),
		User:         user,
		Actor:        actor,
		Conversation: conversation,
	})
}

func messageNewPayload(mm *model.MessageWithMeta) json.RawMessage {
	msg := messageResponseFromMeta(mm)
	return marshalRaw(outboundMessageNewPayload{
		Event:    redis.EventNewMessage,
		ConvID:   msg.ConversationID,
		MsgID:    msg.ID,
		SenderID: msg.SenderID,
		Seq:      msg.Seq,
		Type:     msg.Type,
		Message:  msg,
	})
}

func messageEditedPayload(msg *model.Message, sender *model.User) json.RawMessage {
	meta := messageMetaWithSender(msg, sender)
	out := messageResponseFromMeta(meta)
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	return marshalRaw(outboundMessageEditedPayload{
		Event:    redis.EventEditMessage,
		ConvID:   hex.EncodeToString(msg.ConversationID),
		MsgID:    hex.EncodeToString(msg.ID),
		Content:  content,
		EditedAt: msg.UpdatedAt.Format(time.RFC3339Nano),
		Message:  &out,
	})
}

func messageDeletedPayload(convID, msgID []byte, deletedAt time.Time) json.RawMessage {
	return marshalRaw(outboundMessageDeletedPayload{
		Event:     redis.EventDelMessage,
		ConvID:    hex.EncodeToString(convID),
		MsgID:     hex.EncodeToString(msgID),
		IsDeleted: true,
		DeletedAt: deletedAt.Format(time.RFC3339Nano),
	})
}

func messageReadPayload(convID, userID, lastReadMsgID []byte, readAt time.Time) json.RawMessage {
	return marshalRaw(outboundMessageReadPayload{
		Event:         redis.EventReadMessage,
		ConvID:        hex.EncodeToString(convID),
		UserID:        hex.EncodeToString(userID),
		LastReadMsgID: hex.EncodeToString(lastReadMsgID),
		ReadAt:        readAt.Format(time.RFC3339Nano),
	})
}

func reactionTogglePayload(convID, msgID, userID []byte, emoji, action string) json.RawMessage {
	return marshalRaw(outboundReactionPayload{
		Event:  redis.EventToggleReaction,
		ConvID: hex.EncodeToString(convID),
		MsgID:  hex.EncodeToString(msgID),
		UserID: hex.EncodeToString(userID),
		Emoji:  emoji,
		Action: action,
	})
}

func userSummaryFromUser(user *model.User) *outboundUserSummary {
	if user == nil {
		return nil
	}
	return &outboundUserSummary{
		ID:        hex.EncodeToString(user.ID),
		Name:      user.Username,
		AvatarURL: user.AvatarURL,
	}
}

func userSummaryFromParts(userID []byte, name string, avatarURL *string) *outboundUserSummary {
	return &outboundUserSummary{
		ID:        hex.EncodeToString(userID),
		Name:      name,
		AvatarURL: avatarURL,
	}
}

func conversationListItemForUser(
	ctx context.Context,
	conv *model.Conversation,
	userID []byte,
	role model.MemberRole,
	isMuted bool,
	dmPeer *model.User,
	memberIDs [][]byte,
) dto.ConversationListItem {
	item := conversationListItemFromConversation(conv)
	item.Role = int8(role)
	item.IsMuted = isMuted
	item.UnreadCount = 0

	if conv != nil && conv.Type == model.ConvTypeDirect {
		name := deletedUserDisplayName
		avatarURL := ""
		item.AvatarURL = &avatarURL
		if dmPeer != nil {
			name = dmPeer.Username
			if dmPeer.AvatarURL != nil {
				item.AvatarURL = dmPeer.AvatarURL
			}
			item.IsOnline = isUserOnline(ctx, dmPeer.ID)
			if item.IsOnline {
				item.MemberOnlineCount = 1
			}
		}
		item.Name = &name
		return item
	}

	item.MemberOnlineCount = onlineMemberCountExcept(ctx, userID, memberIDs)
	item.IsOnline = item.MemberOnlineCount > 0
	return item
}

func conversationListItemFromConversation(c *model.Conversation) dto.ConversationListItem {
	if c == nil {
		return dto.ConversationListItem{}
	}

	return dto.ConversationListItem{
		ID:              hex.EncodeToString(c.ID),
		Type:            int8(c.Type),
		Name:            c.Name,
		Description:     c.Description,
		AvatarURL:       c.AvatarURL,
		CreateBy:        optionalHex(c.CreatedBy),
		LastMessageID:   optionalHex(c.LastMessageID),
		LastActivityAt:  optionalTime(c.LastActivityAt),
		CreatedAt:       c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       c.UpdatedAt.Format(time.RFC3339),
		LastMessageText: c.LastMessageText,
	}
}

func messageMetaWithSender(msg *model.Message, sender *model.User) *model.MessageWithMeta {
	meta := &model.MessageWithMeta{
		Message:     msg,
		Attachments: []*model.Attachment{},
		Reactions:   []*model.MessageReaction{},
	}
	if sender != nil {
		meta.SenderName = sender.Username
		meta.SenderAvatarURL = sender.AvatarURL
	}
	return meta
}

func messageResponseFromMeta(mm *model.MessageWithMeta) dto.MessageResponse {
	if mm == nil || mm.Message == nil {
		return dto.MessageResponse{}
	}

	msg := mm.Message
	out := dto.MessageResponse{
		ID:               hex.EncodeToString(msg.ID),
		ConversationID:   hex.EncodeToString(msg.ConversationID),
		SenderID:         hex.EncodeToString(msg.SenderID),
		ParentID:         optionalHex(msg.ParentID),
		Type:             int8(msg.Type),
		Content:          msg.Content,
		ContentEncrypted: msg.ContentEncrypted,
		Iv:               msg.Iv,
		Seq:              msg.Seq,
		IsEdited:         msg.IsEdited,
		IsDeleted:        msg.IsDeleted,
		DeletedAt:        optionalTime(msg.DeletedAt),
		CreatedAt:        msg.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        msg.UpdatedAt.Format(time.RFC3339),
		SenderName:       mm.SenderName,
		SenderAvatarURL:  mm.SenderAvatarURL,
	}

	out.Attachments = make([]dto.AttachmentResponse, 0, len(mm.Attachments))
	for _, att := range mm.Attachments {
		out.Attachments = append(out.Attachments, dto.AttachmentResponse{
			ID:            hex.EncodeToString(att.ID),
			MessageID:     hex.EncodeToString(att.MessageID),
			FileName:      att.Filename,
			FileURL:       att.FileURL,
			MimeType:      att.MimeType,
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

func optionalHex(value []byte) *string {
	if len(value) == 0 {
		return nil
	}
	encoded := hex.EncodeToString(value)
	return &encoded
}

func optionalTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.Format(time.RFC3339)
	return &formatted
}

func isUserOnline(ctx context.Context, userID []byte) bool {
	if g.Presence == nil || len(userID) == 0 {
		return false
	}
	online, err := g.Presence.IsOnline(ctx, userID)
	return err == nil && online
}

func onlineMemberCountExcept(ctx context.Context, currentUID []byte, memberIDs [][]byte) int {
	if g.Presence == nil || len(memberIDs) == 0 {
		return 0
	}

	others := make([][]byte, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		if len(memberID) == 0 || bytes.Equal(memberID, currentUID) {
			continue
		}
		others = append(others, memberID)
	}
	if len(others) == 0 {
		return 0
	}

	onlineByID, err := g.Presence.BulkIsOnline(ctx, others)
	if err != nil {
		return 0
	}

	count := 0
	for _, memberID := range others {
		if onlineByID[hex.EncodeToString(memberID)] {
			count++
		}
	}
	return count
}

func marshalRaw(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return raw
}
