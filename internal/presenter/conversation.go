package presenter

import (
	"bytes"
	"encoding/hex"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

const deletedUserDisplayName = "Deleted user"

type ConversationView struct {
	Conversation *model.Conversation
	UserID       []byte
	Role         model.MemberRole
	IsMuted      bool
	Peer         *model.User
	MemberIDs    [][]byte
	OnlineByID   map[string]bool
}

// ConversationForUser uses a previously loaded presence snapshot; it performs no I/O.
func ConversationForUser(input ConversationView) dto.ConversationListItem {
	conv, userID, role, isMuted := input.Conversation, input.UserID, input.Role, input.IsMuted
	dmPeer, memberIDs := input.Peer, input.MemberIDs
	item := Conversation(conv)
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
			item.IsOnline = input.OnlineByID[hex.EncodeToString(dmPeer.ID)]
			if item.IsOnline {
				item.MemberOnlineCount = 1
			}
		}
		item.Name = &name
		return item
	}

	item.MemberOnlineCount = onlineMemberCountExcept(userID, memberIDs, input.OnlineByID)
	item.IsOnline = item.MemberOnlineCount > 0
	return item
}

func Conversation(c *model.Conversation) dto.ConversationListItem {
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

func CreateRoom(c *model.Conversation) dto.CreateRoomResponse {
	item := Conversation(c)
	return dto.CreateRoomResponse{
		ID:              item.ID,
		Type:            item.Type,
		Name:            item.Name,
		Description:     item.Description,
		AvatarURL:       item.AvatarURL,
		CreateBy:        item.CreateBy,
		LastMessageID:   item.LastMessageID,
		LastMessageText: item.LastMessageText,
		LastActivityAt:  item.LastActivityAt,
		CreatedAt:       item.CreatedAt,
		UpdatedAt:       item.UpdatedAt,
	}
}
func ConversationRow(row *model.ConversationListRow) dto.ConversationListItem {
	item := Conversation(&row.Conversation)
	item.Role, item.IsMuted, item.UnreadCount = int8(row.Role), row.IsMuted, row.UnreadCount
	item.MemberOnlineCount, item.IsOnline = row.MemberOnlineCount, row.IsOnline
	return item
}
func onlineMemberCountExcept(userID []byte, memberIDs [][]byte, onlineByID map[string]bool) int {
	count := 0
	for _, id := range memberIDs {
		if len(id) > 0 && !bytes.Equal(id, userID) && onlineByID[hex.EncodeToString(id)] {
			count++
		}
	}
	return count
}
