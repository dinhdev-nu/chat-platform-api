package websocket

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Inbound event ( client -> server ) types
const (
	InboundTyping  = "typing"  // User is typing
	InboundViewing = "viewing" // User is viewing a conversation
	InboundLeft    = "left"    // User left a conversation
	InboundRead    = "read"    // User read messages in a conversation
)

type InboundEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type TypingPayload struct {
	ConvID string `json:"conv_id"`
}
type ViewingPayload struct {
	ConvID string `json:"conv_id"`
}
type LeftPayload struct {
	ConvID string `json:"conv_id"`
}
type ReadPayload struct {
	ConvID        string `json:"conv_id"`
	LastReadMsgID string `json:"last_read_msg_id"`
}

const (
	DomainMemberAdded   = "member.added"
	DomainMemberRemoved = "member.removed"
)

type DomainEvent struct {
	Event  string `json:"event"`
	UserID string `json:"user_id"` // hex uid của user bị thêm/xóa
	ConvID string `json:"conv_id"` // hex conv_id
}

const (
	SysConvSubscribe   = "conv.subscribe"
	SysConvUnsubscribe = "conv.unsubscribe"
	SysContactsSet     = "contacts.set"
	SysContactsAdd     = "contacts.add"
)

const presenceEventChannel = "presence:events"

type sysEvent struct {
	Type    string          `json:"type"`              // SysConvSubscribe | SysConvUnsubscribe | SysContactsSet | SysContactsAdd
	ConvID  string          `json:"conv_id,omitempty"` // hex conv_id
	UserID  string          `json:"user_id,omitempty"` // hex user_id
	UserIDs []string        `json:"user_ids,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"` // optional client-facing event payload
}

type presenceEvent struct {
	UserID   string   `json:"user_id"`
	IsOnline bool     `json:"is_online"`
	ConvIDs  []string `json:"conv_ids,omitempty"`
}

const (
	OutboundTyping         = "typing"   // Notify others that user is typing
	OutboundPresence       = "presence" // Notify others about user presence (online/offline)
	OutboundMessageNew     = "message.new"
	OutboundMessageEdited  = "message.edited"
	OutboundMessageDeleted = "message.deleted"
	OutboundMessageRead    = "message.read"
	OutboundReactionToggle = "reaction.toggle"
	OutboundConvCreated    = "conversation.created"
	OutboundMemberAdded    = "member.added"
	OutboundMemberRemoved  = "member.removed"
)

type OutboundEvent struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload,omitempty"`
}

func notifyChannel(convID []byte) string     { return "notify:" + hex.EncodeToString(convID) }
func notifyChannelHex(cidHex string) string  { return "notify:" + cidHex }
func sysChannel(uidHex string) string        { return "sys:" + uidHex }
func presenceKey(uidHex string) string       { return "presence:" + uidHex }
func typingKey(cidHex, uidHex string) string { return fmt.Sprintf("typing:%s:%s", cidHex, uidHex) }
