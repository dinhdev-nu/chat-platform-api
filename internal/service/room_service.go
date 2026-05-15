package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis/cache"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
)

type RoomService struct {
	userRepo r.UserRepository
	roomRepo r.RoomRepository
	msgRepo  r.MessageRepository
}

type ResultPage[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
	HasMore    bool    `json:"hasMore"`
}

func NewRoomService(ur r.UserRepository, rr r.RoomRepository, mr r.MessageRepository) *RoomService {
	return &RoomService{
		userRepo: ur,
		roomRepo: rr,
		msgRepo:  mr,
	}
}

func (s *RoomService) CreateDM(ctx context.Context, currentUID, targetUserID []byte) (*model.Conversation, bool, error) {
	// Ko cần cache User Status
	// 1/ Verify target user exists
	targetUser, err := s.userRepo.FindByID(ctx, targetUserID)
	if err != nil {
		return nil, false, ae.Internal(err)
	}
	if targetUser == nil || !targetUser.IsActive() {
		return nil, false, ae.New(ae.ErrUserNotFound, "User not found")
	}

	// 2/ Kiểm tra room exists
	convID, err := s.roomRepo.GetDMConversation(ctx, currentUID, targetUserID)
	if err != nil {
		return nil, false, ae.Internal(err)
	}
	if convID != nil {
		conv, err := s.roomRepo.GetConversationByID(ctx, convID)
		if err != nil {
			return nil, false, ae.Internal(err)
		}
		if conv == nil {
			return nil, false, ae.New(ae.ErrConversationNotFound, "Conversation not found")
		}
		return conv, true, nil
	}

	// 3/ Create new room
	convID, err = crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, false, ae.Internal(err)
	}
	newConv := &model.Conversation{
		ID:        convID,
		Type:      model.ConvTypeDirect,
		CreatedBy: currentUID,
	}
	if err := s.roomRepo.CreateConversation(ctx, newConv); err != nil {
		return nil, false, ae.Internal(err)
	}
	// Insert Members
	members := []*model.ConversationMember{
		{
			ConversationID: convID,
			UserID:         currentUID,
			Role:           model.RoleMember,
		},
		{
			ConversationID: convID,
			UserID:         targetUserID,
			Role:           model.RoleMember,
		},
	}
	if err := s.roomRepo.BatchInsertConversationMembers(ctx, members); err != nil {
		return nil, false, ae.Internal(err)
	}

	// Warm member cache
	go func() {
		_ = cache.WarmMember(context.Background(), convID, [][]byte{currentUID, targetUserID})
	}()

	conv, err := s.roomRepo.GetConversationByID(ctx, convID)
	if err != nil {
		return nil, false, ae.Internal(err)
	}

	return conv, false, nil
}

func (s *RoomService) CreateGroup(ctx context.Context, currentUID []byte, req dto.CreateGroupRequest) (*model.Conversation, error) {
	convID, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, ae.Internal(err)
	}

	name := req.Name
	arg := &model.Conversation{
		ID:        convID,
		Type:      model.ConversationType(req.Type),
		Name:      &name,
		CreatedBy: currentUID,
	}
	if err := s.roomRepo.CreateConversation(ctx, arg); err != nil {
		return nil, ae.Internal(err)
	}

	// Insert Members
	allMemberIDs := make([][]byte, 0, len(req.MemberUIDs)+1)
	allMemberIDs = append(allMemberIDs, currentUID)

	for _, mID := range req.MemberUIDs {
		allMemberIDs = append(allMemberIDs, mID)
	}

	margs := make([]*model.ConversationMember, 0, len(allMemberIDs))
	for i, memberID := range allMemberIDs {
		role := model.RoleMember
		if i == 0 {
			role = model.RoleAdmin
		}
		margs = append(margs, &model.ConversationMember{
			ConversationID: convID,
			UserID:         memberID,
			Role:           role,
		})
	}

	if err := s.roomRepo.BatchInsertConversationMembers(ctx, margs); err != nil {
		return nil, ae.Internal(err)
	}

	// Warm member cache
	go func() {
		_ = cache.WarmMember(context.Background(), convID, allMemberIDs)
	}()

	// Insert sytem message: "X created the group"
	go func() {
		bg := context.Background()
		seqVal, err := GetNextSeqFromRedis(bg, s.msgRepo, convID)
		if err != nil {
			return
		}
		msgUUID, err := crypto.NewUUIDv7Bytes()
		if err != nil {
			return
		}
		content := "Group created"
		arg := &model.Message{
			ID:             msgUUID,
			ConversationID: convID,
			SenderID:       currentUID,
			Content:        &content,
			Seq:            uint64(seqVal),
		}
		if err := s.msgRepo.InsertSystemMessage(bg, arg); err != nil {
			return
		}
	}()

	// // Push notification
	// convHex := hex.EncodeToString(convID)
	// _ = g.PubSub.Publish(ctx, convID, redis.Event{
	// 	Type:    redis.EventConvCreated,
	// 	ConvID:  convHex,
	// 	Payload: json.RawMessage(`{"event":"conv.created","conv_id":"` + convHex + `"}`),
	// })

	conv, err := s.roomRepo.GetConversationByID(ctx, convID)
	if err != nil {
		return nil, ae.Internal(err)
	}

	return conv, nil
}

func (s *RoomService) ListConversations(ctx context.Context, uid []byte, cursor *string, limit int) (*ResultPage[*model.ConversationListRow], error) {
	// Không cache vì các trường thay đổi thường xuyên (last_message_at, v.v.) và có phân trang
	const maxLimit = 50
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}
	fetch := int32(limit) + 1 // Lấy dư 1 bản ghi để xác định hasNext

	var (
		cursorTS *time.Time // last_message_at
		cursorID []byte     // conversation_id
	)
	if cursor != nil && *cursor != "" {
		ts, id, err := decodeCursor(*cursor)
		if err != nil {
			return nil, ae.New(ae.ErrInvalidCursor, "Invalid cursor")
		}
		cursorTS = &ts
		cursorID = id
	}

	convs, err := s.roomRepo.ListConversations(ctx, uid, cursorTS, cursorID, fetch)
	if err != nil {
		return nil, ae.Internal(err)
	}
	hasMore := len(convs) == int(fetch)
	if hasMore {
		convs = convs[:limit] // Cắt bỏ bản ghi dư
	}
	convIDs := make([][]byte, len(convs))
	for i, c := range convs {
		convIDs[i] = c.ID
	}
	unreadMap, err := cache.GetUnreads(ctx, uid, convIDs)
	if err != nil {
		unreadMap = map[string]int64{} // Fallback nếu cache lỗi
	}
	for i, c := range convs {
		cidHex := hex.EncodeToString(c.ID)
		unread, hit := unreadMap[cidHex]
		if hit {
			c.UnreadCount = unread
		} else {
			unread, _ := s.msgRepo.GetUnreadCountByWatermark(ctx, uid, c.ID)
			go func(cid []byte, uid []byte, count int64) {
				_ = cache.SetUnread(context.Background(), uid, cid, count)
			}(c.ID, uid, unread)
		}
		convs[i] = c
	}

	nextCursor := ""
	if hasMore {
		// Tạo cursor cho lần gọi tiếp theo
		lastConv := convs[len(convs)-1]
		encoded := encodeCursor(*lastConv.LastActivityAt, lastConv.ID)
		nextCursor = encoded
	}

	return &ResultPage[*model.ConversationListRow]{
		Items:      convs,
		NextCursor: &nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *RoomService) AddMember(ctx context.Context, convUID, actorUID, targetUID []byte, actorName string) error {
	// 1/Kiểm tra actor có phải admin/owner ko
	actorRole, err := s.roomRepo.GetMemberRole(ctx, convUID, actorUID)
	if err != nil {
		return ae.Internal(err)
	}
	if actorRole != model.RoleAdmin && actorRole != model.RoleOwner {
		return ae.Forbidden("Only admins can add members")
	}
	// 2/ Kiểm tra target user exists
	targetUser, err := s.userRepo.FindByID(ctx, targetUID)
	if err != nil {
		return ae.Internal(err)
	}
	if targetUser == nil || !targetUser.IsActive() {
		return ae.New(ae.ErrUserNotFound, "User not found")
	}

	// 3/ Thêm member
	arg := &model.ConversationMember{
		ConversationID: convUID,
		UserID:         targetUID,
		Role:           model.RoleMember,
	}
	// ignore khi đã là member err == nil
	if err := s.roomRepo.InsertConversationMember(ctx, arg); err != nil {
		return ae.Internal(err)
	}
	// Warm member cache
	go func() {
		_ = cache.AddMember(context.Background(), convUID, targetUID)
	}()
	// Insert system message: "X added Y"
	go func() {
		bg := context.Background()
		seqVal, err := GetNextSeqFromRedis(bg, s.msgRepo, convUID)
		if err != nil {
			return
		}

		msgID, err := crypto.NewUUIDv7Bytes()
		if err != nil {
			return
		}
		content := fmt.Sprintf("%s added %s", actorName, targetUser.Username)

		arg := &model.Message{
			ID:             msgID,
			ConversationID: convUID,
			SenderID:       actorUID,
			Content:        &content,
			Seq:            uint64(seqVal),
		}
		_ = s.msgRepo.InsertSystemMessage(bg, arg)
	}()
	// Push notification
	// convHex := hex.EncodeToString(convUID)
	// _ = g.PubSub.Publish(ctx, convUID, redis.Event{
	// 	Type:    redis.EventMemberAdded,
	// 	ConvID:  convHex,
	// 	Payload: json.RawMessage(`{"event":"member.added","conv_id":"` + convHex + `","user_id":"` + hex.EncodeToString(targetUID) + `"}`),
	// })
	return nil
}

func (s *RoomService) RemoveMember(ctx context.Context, convID, actorUID, targetUID []byte, actorName string) error {
	// 1/Kiểm tra actor có phải admin/owner ko
	actorRole, err := s.roomRepo.GetMemberRole(ctx, convID, actorUID)
	if err != nil {
		return ae.Internal(err)
	}
	if actorRole != model.RoleAdmin && actorRole != model.RoleOwner {
		return ae.Forbidden("Only admins can remove members")
	}

	// 2/ Kiểm tra target user exists
	targetName := actorName
	isSelf := bytes.Equal(actorUID, targetUID)
	if !isSelf {
		targetUser, err := s.userRepo.FindByID(ctx, targetUID)
		if err != nil {
			return ae.Internal(err)
		}
		if targetUser == nil || !targetUser.IsActive() {
			return ae.New(ae.ErrUserNotFound, "User not found")
		}
		targetName = targetUser.Username
	}

	// 3/ Xóa member
	if err := s.roomRepo.DeleteConversationMember(ctx, convID, targetUID); err != nil {
		return ae.Internal(err)
	}

	go func() {
		bg := context.Background()
		_ = cache.RemoveMember(bg, convID, targetUID)
		_ = cache.DeleteUnread(bg, targetUID, convID)
	}()

	// Insert system message: "X left/removed Y"
	go func() {
		bg := context.Background()
		seqVal, err := GetNextSeqFromRedis(bg, s.msgRepo, convID)
		if err != nil {
			return
		}
		msgID, err := crypto.NewUUIDv7Bytes()
		if err != nil {
			return
		}
		action := "left"
		if !isSelf {
			action = "removed"
		}
		content := fmt.Sprintf("%s %s", targetName, action)
		arg := &model.Message{
			ID:             msgID,
			ConversationID: convID,
			SenderID:       actorUID,
			Content:        &content,
			Seq:            uint64(seqVal),
		}
		_ = s.msgRepo.InsertSystemMessage(bg, arg)
	}()

	// Push notification
	convHex := hex.EncodeToString(convID)
	_ = g.PubSub.Publish(ctx, convID, redis.Event{
		Type:    redis.EventMemberRemoved,
		ConvID:  convHex,
		Payload: json.RawMessage(`{"event":"member.removed","conv_id":"` + convHex + `","user_id":"` + hex.EncodeToString(targetUID) + `"}`),
	})

	return nil
}

func GetNextSeqFromRedis(ctx context.Context, msgRepo r.MessageRepository, convID []byte) (int64, error) {
	convHex := strings.ToLower(hex.EncodeToString(convID))
	seqKey := fmt.Sprintf("seq:%s", convHex)
	lookKey := fmt.Sprintf("look:seq_init:%s", convHex)

	exists, err := g.RedisClient.Exists(ctx, seqKey).Result()
	if err != nil {
		return 0, err
	}
	if exists == 0 {
		// Key ko tồn tại, khởi tạo từ DB
		acquired, err := g.RedisClient.SetNX(ctx, lookKey, 1, 5*time.Second).Result()
		if err != nil {
			return 0, err
		}

		if acquired {
			defer g.RedisClient.Del(ctx, lookKey) // Release lock sau khi khởi tạo xong

			maxSeq, err := msgRepo.GetMaxSeq(ctx, convID)
			if err != nil {
				return 0, err
			}

			if err := g.RedisClient.Set(ctx, seqKey, maxSeq, 0).Err(); err != nil {
				return 0, err
			}
		} else {
			for i := 0; i < 10; i++ { // Retry 10 lần với exponential backoff
				time.Sleep(10 * time.Millisecond)
				ex, err := g.RedisClient.Exists(ctx, seqKey).Result()
				if err != nil {
					return 0, err
				}
				if ex > 1 {
					break
				}
			}
		}
	}

	val, err := g.RedisClient.Incr(ctx, seqKey).Result()
	if err != nil {
		return 0, err
	}
	return val, nil
}

func decodeCursor(cursor string) (time.Time, []byte, error) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, nil, err
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, nil, fmt.Errorf("invalid cursor format")
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, nil, err
	}
	idBytes, err := hex.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, nil, err
	}
	return time.UnixMilli(ms), idBytes, nil
}

func encodeCursor(t time.Time, id []byte) string {
	payload := fmt.Sprintf("%d:%s", t.UnixMilli(), strings.ToUpper(hex.EncodeToString(id)))
	return base64.StdEncoding.EncodeToString([]byte(payload))
}
