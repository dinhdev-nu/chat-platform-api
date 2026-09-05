package presenter

import (
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

type outboundConversationPayload struct {
	Event        redis.EventType          `json:"event"`
	ConvID       string                   `json:"conv_id"`
	Conversation dto.ConversationListItem `json:"conversation"`
}

type UserSummary struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url,omitempty"`
}

type outboundMemberPayload struct {
	Event        redis.EventType           `json:"event"`
	ConvID       string                    `json:"conv_id"`
	UserID       string                    `json:"user_id"`
	User         *UserSummary              `json:"user,omitempty"`
	Actor        *UserSummary              `json:"actor,omitempty"`
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

func ConversationCreatedPayload(item dto.ConversationListItem) json.RawMessage {
	return marshalRaw(outboundConversationPayload{
		Event:        redis.EventConvCreated,
		ConvID:       item.ID,
		Conversation: item,
	})
}

func MemberPayload(event redis.EventType, convID, userID []byte, user, actor *UserSummary, conversation *dto.ConversationListItem) json.RawMessage {
	return marshalRaw(outboundMemberPayload{
		Event:        event,
		ConvID:       hex.EncodeToString(convID),
		UserID:       hex.EncodeToString(userID),
		User:         user,
		Actor:        actor,
		Conversation: conversation,
	})
}

func MessageNewPayload(mm *model.MessageWithMeta) json.RawMessage {
	msg := MessageWithMeta(mm)
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

func MessageEditedPayload(msg *model.Message, sender *model.User) json.RawMessage {
	meta := MessageMetaWithSender(msg, sender)
	out := MessageWithMeta(meta)
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

func MessageDeletedPayload(convID, msgID []byte, deletedAt time.Time) json.RawMessage {
	return marshalRaw(outboundMessageDeletedPayload{
		Event:     redis.EventDelMessage,
		ConvID:    hex.EncodeToString(convID),
		MsgID:     hex.EncodeToString(msgID),
		IsDeleted: true,
		DeletedAt: deletedAt.Format(time.RFC3339Nano),
	})
}

func MessageReadPayload(convID, userID, lastReadMsgID []byte, readAt time.Time) json.RawMessage {
	return marshalRaw(outboundMessageReadPayload{
		Event:         redis.EventReadMessage,
		ConvID:        hex.EncodeToString(convID),
		UserID:        hex.EncodeToString(userID),
		LastReadMsgID: hex.EncodeToString(lastReadMsgID),
		ReadAt:        readAt.Format(time.RFC3339Nano),
	})
}

func ReactionTogglePayload(convID, msgID, userID []byte, emoji, action string) json.RawMessage {
	return marshalRaw(outboundReactionPayload{
		Event:  redis.EventToggleReaction,
		ConvID: hex.EncodeToString(convID),
		MsgID:  hex.EncodeToString(msgID),
		UserID: hex.EncodeToString(userID),
		Emoji:  emoji,
		Action: action,
	})
}

func UserSummaryFromUser(user *model.User) *UserSummary {
	if user == nil {
		return nil
	}
	return &UserSummary{
		ID:        hex.EncodeToString(user.ID),
		Name:      user.Username,
		AvatarURL: user.AvatarURL,
	}
}

func UserSummaryFromParts(userID []byte, name string, avatarURL *string) *UserSummary {
	return &UserSummary{
		ID:        hex.EncodeToString(userID),
		Name:      name,
		AvatarURL: avatarURL,
	}
}

func MessageMetaWithSender(msg *model.Message, sender *model.User) *model.MessageWithMeta {
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

func marshalRaw(payload any) json.RawMessage {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return raw
}
