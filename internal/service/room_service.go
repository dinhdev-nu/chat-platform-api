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
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis/cache"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"go.uber.org/zap"
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
	Limit      int     `json:"limit"`
}

const (
	sysConvSubscribe   = "conv.subscribe"
	sysConvUnsubscribe = "conv.unsubscribe"
)

type conversationSysEvent struct {
	Type    string          `json:"type"`
	ConvID  string          `json:"conv_id,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func NewRoomService(ur r.UserRepository, rr r.RoomRepository, mr r.MessageRepository) *RoomService {
	return &RoomService{
		userRepo: ur,
		roomRepo: rr,
		msgRepo:  mr,
	}
}

func (s *RoomService) CreateDM(ctx context.Context, currentUID, targetUserID []byte) (*model.Conversation, bool, error) {
	if bytes.Equal(currentUID, targetUserID) {
		return nil, false, ae.New(ae.ErrInvalidInput, "Cannot create DM with yourself")
	}

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

	currentUser, err := s.userRepo.FindByID(ctx, currentUID)
	if err != nil {
		return nil, false, ae.Internal(err)
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
	go warmMemberCache(ctx, convID, [][]byte{currentUID, targetUserID})

	conv, err := s.roomRepo.GetConversationByID(ctx, convID)
	if err != nil {
		return nil, false, ae.Internal(err)
	}

	memberIDs := [][]byte{currentUID, targetUserID}
	currentItem := conversationListItemForUser(ctx, conv, currentUID, model.RoleMember, false, targetUser, memberIDs)
	targetItem := conversationListItemForUser(ctx, conv, targetUserID, model.RoleMember, false, currentUser, memberIDs)
	s.publishConvSubscribe(ctx, currentUID, convID, conversationCreatedPayload(currentItem))
	s.publishConvSubscribe(ctx, targetUserID, convID, conversationCreatedPayload(targetItem))

	return conv, false, nil
}

func (s *RoomService) CreateGroup(ctx context.Context, currentUID []byte, req dto.CreateGroupRequest) (*model.Conversation, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ae.ValidationError("Group name is required")
	}

	convType := model.ConversationType(req.Type)
	if convType != model.ConvTypeGroup && convType != model.ConvTypeChannel {
		return nil, ae.ValidationError("Invalid conversation type")
	}

	memberIDs := make([][]byte, 0, len(req.MemberUIDs))
	seen := make(map[string]struct{}, len(req.MemberUIDs)+1)
	seen[string(currentUID)] = struct{}{}
	for _, mID := range req.MemberUIDs {
		memberID := []byte(mID)
		if len(memberID) != 16 {
			return nil, ae.ValidationError("Invalid member user ID")
		}
		if bytes.Equal(memberID, currentUID) {
			return nil, ae.New(ae.ErrInvalidInput, "Creator must not be included in member_user_ids")
		}
		key := string(memberID)
		if _, ok := seen[key]; ok {
			return nil, ae.New(ae.ErrInvalidInput, "Duplicate member_user_ids are not allowed")
		}
		seen[key] = struct{}{}
		memberIDs = append(memberIDs, memberID)
	}
	if len(memberIDs) == 0 {
		return nil, ae.ValidationError("member_user_ids is required")
	}

	activeIDs, err := s.userRepo.FindActiveIDs(ctx, memberIDs)
	if err != nil {
		return nil, ae.Internal(err)
	}
	for _, memberID := range memberIDs {
		if !activeIDs[string(memberID)] {
			return nil, ae.New(ae.ErrUserNotFound, "User not found")
		}
	}

	convID, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, ae.Internal(err)
	}

	arg := &model.Conversation{
		ID:          convID,
		Type:        convType,
		Name:        &name,
		AvatarURL:   req.AvatarURL,
		Description: req.Description,
		CreatedBy:   currentUID,
	}
	if err := s.roomRepo.CreateConversation(ctx, arg); err != nil {
		return nil, ae.Internal(err)
	}

	// Insert Members
	allMemberIDs := make([][]byte, 0, len(memberIDs)+1)
	allMemberIDs = append(allMemberIDs, currentUID)
	allMemberIDs = append(allMemberIDs, memberIDs...)

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
	go warmMemberCache(ctx, convID, allMemberIDs)

	s.enqueueSystemMessage(ctx, convID, currentUID, "Group created")

	// // Push notification
	// convHex := hex.EncodeToString(convID)
	// _ = g.PubSub.Publish(ctx, convID, redis.Event{
	// 	Type:    redis.EventConvCreated,
	// 	ConvID:  convHex,
	// 	Payload: json.RawMessage(`{"event":"conversation.created","conv_id":"` + convHex + `"}`),
	// })

	conv, err := s.roomRepo.GetConversationByID(ctx, convID)
	if err != nil {
		return nil, ae.Internal(err)
	}

	for i, memberID := range allMemberIDs {
		role := model.RoleMember
		if i == 0 {
			role = model.RoleAdmin
		}
		item := conversationListItemForUser(ctx, conv, memberID, role, false, nil, allMemberIDs)
		s.publishConvSubscribe(ctx, memberID, convID, conversationCreatedPayload(item))
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
			c.UnreadCount = unread
			go func(parent context.Context, cid []byte, uid []byte, count int64) {
				cacheCtx, cancel := detachedContext(parent, cacheTaskTimeout)
				defer cancel()
				if err := cache.SetUnread(cacheCtx, uid, cid, count); err != nil {
					g.Logger.Warn("roomService.ListConversations: failed to warm unread cache",
						zap.String("conv_id", hex.EncodeToString(cid)),
						zap.Error(err),
					)
				}
			}(ctx, c.ID, uid, unread)
		}
		convs[i] = c
	}
	s.attachConversationPresence(ctx, uid, convs)

	nextCursor := ""
	if hasMore {
		// Tạo cursor cho lần gọi tiếp theo
		lastConv := convs[len(convs)-1]
		if lastConv.LastActivityAt != nil {
			nextCursor = encodeCursor(*lastConv.LastActivityAt, lastConv.ID)
		} else {
			hasMore = false
		}
	}

	return &ResultPage[*model.ConversationListRow]{
		Items:      convs,
		NextCursor: &nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *RoomService) AddMember(ctx context.Context, convUID, actorUID, targetUID []byte, actorName string) error {
	if bytes.Equal(actorUID, targetUID) {
		return ae.New(ae.ErrInvalidInput, "Cannot add yourself")
	}

	// 1/Kiểm tra actor có phải admin/owner ko
	actorRole, err := s.roomRepo.GetMemberRole(ctx, convUID, actorUID)
	if err != nil {
		return ae.Internal(err)
	}
	if actorRole == 0 {
		return ae.New(ae.ErrNotAMember, "User is not a member of the conversation")
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
	err = s.roomRepo.InsertConversationMember(ctx, arg)
	if err != nil {
		return ae.Internal(err)
	}
	if err := cache.AddMember(ctx, convUID, targetUID); err != nil {
		invalidateCtx, cancel := detachedContext(ctx, cacheTaskTimeout)
		invalidateErr := cache.InvalidateMembers(invalidateCtx, convUID)
		cancel()
		if invalidateErr != nil {
			g.Logger.Warn("roomService.AddMember: failed to invalidate member cache",
				zap.String("conv_id", hex.EncodeToString(convUID)),
				zap.Error(invalidateErr),
			)
		}
	}
	s.enqueueSystemMessage(ctx, convUID, actorUID, fmt.Sprintf("%s added %s", actorName, targetUser.Username))
	// Push notification
	convHex := hex.EncodeToString(convUID)
	actor := userSummaryFromParts(actorUID, actorName, nil)
	if actorUser, err := s.userRepo.FindByID(ctx, actorUID); err == nil && actorUser != nil {
		actor = userSummaryFromUser(actorUser)
	}
	member := userSummaryFromUser(targetUser)
	payload := memberPayload(redis.EventMemberAdded, convUID, targetUID, member, actor, nil)
	_ = g.PubSub.Publish(ctx, convUID, redis.Event{
		Type:    redis.EventMemberAdded,
		ConvID:  convHex,
		Payload: payload,
	})
	targetPayload := payload
	conv, err := s.roomRepo.GetConversationByID(ctx, convUID)
	if err == nil && conv != nil {
		memberIDs, _ := s.roomRepo.GetConversationMemberIDs(ctx, convUID)
		item := conversationListItemForUser(ctx, conv, targetUID, model.RoleMember, false, nil, memberIDs)
		targetPayload = memberPayload(redis.EventMemberAdded, convUID, targetUID, member, actor, &item)
	}
	s.publishConvSubscribe(ctx, targetUID, convUID, targetPayload)
	return nil
}

func (s *RoomService) RemoveMember(ctx context.Context, convID, actorUID, targetUID []byte, actorName string) error {
	isSelf := bytes.Equal(actorUID, targetUID)

	// 1/Kiểm tra actor có phải admin/owner ko
	actorRole, err := s.roomRepo.GetMemberRole(ctx, convID, actorUID)
	if err != nil {
		return ae.Internal(err)
	}
	if actorRole == 0 {
		return ae.New(ae.ErrNotAMember, "User is not a member of the conversation")
	}
	if !isSelf && actorRole != model.RoleAdmin && actorRole != model.RoleOwner {
		return ae.Forbidden("Only admins can remove members")
	}

	// 2/ Kiểm tra target user exists
	targetName := actorName
	var targetUser *model.User
	if !isSelf {
		targetUser, err = s.userRepo.FindByID(ctx, targetUID)
		if err != nil {
			return ae.Internal(err)
		}
		if targetUser == nil {
			return ae.New(ae.ErrUserNotFound, "User not found")
		}
		targetName = targetUser.Username
	}

	// 3/ Xóa member
	err = s.roomRepo.DeleteConversationMember(ctx, convID, targetUID)
	if err != nil {
		return ae.Internal(err)
	}

	go func(parent context.Context) {
		cacheCtx, cancel := detachedContext(parent, sideEffectTimeout)
		defer cancel()

		if err := cache.RemoveMember(cacheCtx, convID, targetUID); err != nil {
			g.Logger.Warn("roomService.RemoveMember: failed to remove member from cache",
				zap.String("conv_id", hex.EncodeToString(convID)),
				zap.Error(err),
			)
			if invalidateErr := cache.InvalidateMembers(cacheCtx, convID); invalidateErr != nil {
				g.Logger.Warn("roomService.RemoveMember: failed to invalidate member cache",
					zap.String("conv_id", hex.EncodeToString(convID)),
					zap.Error(invalidateErr),
				)
			}
		}
		if err := cache.DeleteUnread(cacheCtx, targetUID, convID); err != nil {
			g.Logger.Warn("roomService.RemoveMember: failed to delete unread cache",
				zap.String("conv_id", hex.EncodeToString(convID)),
				zap.Error(err),
			)
		}
	}(ctx)

	action := "left"
	if !isSelf {
		action = "removed"
	}
	s.enqueueSystemMessage(ctx, convID, actorUID, fmt.Sprintf("%s %s", targetName, action))

	// Push notification
	convHex := hex.EncodeToString(convID)
	actor := userSummaryFromParts(actorUID, actorName, nil)
	if actorUser, err := s.userRepo.FindByID(ctx, actorUID); err == nil && actorUser != nil {
		actor = userSummaryFromUser(actorUser)
	}
	member := userSummaryFromParts(targetUID, targetName, nil)
	if targetUser != nil {
		member = userSummaryFromUser(targetUser)
	} else if isSelf {
		member = actor
	}
	payload := memberPayload(redis.EventMemberRemoved, convID, targetUID, member, actor, nil)
	_ = g.PubSub.Publish(ctx, convID, redis.Event{
		Type:    redis.EventMemberRemoved,
		ConvID:  convHex,
		Payload: payload,
	})
	s.publishConvUnsubscribe(ctx, targetUID, convID, payload)

	return nil
}

func GetNextSeqFromRedis(ctx context.Context, msgRepo r.MessageRepository, convID []byte) (uint64, error) {
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
				if ex > 0 {
					break
				}
			}
		}
	}

	cmd := g.RedisClient.Incr(ctx, seqKey)
	val, err := cmd.Result()
	if err != nil {
		return 0, err
	}
	if val < 0 {
		return 0, fmt.Errorf("invalid negative message sequence: %d", val)
	}
	return cmd.Uint64()
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

func (s *RoomService) enqueueSystemMessage(parent context.Context, convID, senderID []byte, content string) {
	ctx, cancel := detachedContext(parent, sideEffectTimeout)
	defer cancel()

	seqVal, err := GetNextSeqFromRedis(ctx, s.msgRepo, convID)
	if err != nil {
		g.Logger.Error("roomService: failed to allocate system message seq",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.Error(err),
		)
		return
	}

	msgID, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		g.Logger.Error("roomService: failed to create system message id",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.Error(err),
		)
		return
	}

	payload := queue.ConversationSystemMessagePayload{
		ConversationID: convID,
		MessageID:      msgID,
		SenderID:       senderID,
		Content:        content,
		Seq:            seqVal,
		ActivityAt:     time.Now(),
	}
	if g.Stream == nil {
		g.Logger.Warn("roomService: stream unavailable for system message, applying sync fallback",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.String("msg_id", hex.EncodeToString(msgID)),
		)
		s.insertSystemMessageFallback(parent, payload)
		return
	}
	if err := g.Stream.EnqueueJob(ctx, queue.JobCreateConversationSystemMessage, payload); err != nil {
		g.Logger.Warn("roomService: enqueue system message failed, applying sync fallback",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.String("msg_id", hex.EncodeToString(msgID)),
			zap.Error(err),
		)
		s.insertSystemMessageFallback(parent, payload)
	}
}

func (s *RoomService) insertSystemMessageFallback(parent context.Context, payload queue.ConversationSystemMessagePayload) {
	ctx, cancel := detachedContext(parent, sideEffectTimeout)
	defer cancel()

	content := payload.Content
	msg := &model.Message{
		ID:             payload.MessageID,
		ConversationID: payload.ConversationID,
		SenderID:       payload.SenderID,
		Type:           model.MessageTypeSystem,
		Content:        &content,
		Seq:            payload.Seq,
	}
	if err := s.msgRepo.InsertSystemMessage(ctx, msg); err != nil {
		g.Logger.Error("roomService: sync fallback insert system message failed",
			zap.String("conv_id", hex.EncodeToString(payload.ConversationID)),
			zap.String("msg_id", hex.EncodeToString(payload.MessageID)),
			zap.Error(err),
		)
		return
	}
	if err := s.roomRepo.UpdateConversationLastActivity(ctx, payload.ConversationID, payload.MessageID, &content, payload.ActivityAt); err != nil {
		g.Logger.Error("roomService: sync fallback update last activity failed",
			zap.String("conv_id", hex.EncodeToString(payload.ConversationID)),
			zap.String("msg_id", hex.EncodeToString(payload.MessageID)),
			zap.Error(err),
		)
	}
}

func (s *RoomService) publishConvSubscribe(ctx context.Context, userID, convID []byte, payload json.RawMessage) {
	s.publishConversationSysEvent(ctx, userID, conversationSysEvent{
		Type:    sysConvSubscribe,
		ConvID:  hex.EncodeToString(convID),
		Payload: payload,
	})
}

func (s *RoomService) publishConvUnsubscribe(ctx context.Context, userID, convID []byte, payload json.RawMessage) {
	s.publishConversationSysEvent(ctx, userID, conversationSysEvent{
		Type:    sysConvUnsubscribe,
		ConvID:  hex.EncodeToString(convID),
		Payload: payload,
	})
}

func (s *RoomService) publishConversationSysEvent(ctx context.Context, userID []byte, evt conversationSysEvent) {
	if g.RedisClient == nil {
		return
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		g.Logger.Warn("roomService.publishConversationSysEvent: failed to marshal event", zap.Error(err))
		return
	}

	go func(parent context.Context) {
		publishCtx, cancel := detachedContext(parent, sideEffectTimeout)
		defer cancel()
		if err := g.RedisClient.Publish(publishCtx, "sys:"+hex.EncodeToString(userID), payload).Err(); err != nil {
			g.Logger.Warn("roomService.publishConversationSysEvent: failed to publish event",
				zap.String("user_id", hex.EncodeToString(userID)),
				zap.Error(err),
			)
		}
	}(ctx)
}

func (s *RoomService) attachConversationPresence(ctx context.Context, currentUID []byte, convs []*model.ConversationListRow) {
	if len(convs) == 0 {
		return
	}

	membersByConv := make(map[string][][]byte, len(convs))
	uniqueMembers := make([][]byte, 0)
	seen := make(map[string]struct{})

	for _, conv := range convs {
		members, err := cache.GetMembers(ctx, conv.ID)
		if err != nil || len(members) == 0 {
			members, err = s.roomRepo.GetConversationMemberIDs(ctx, conv.ID)
			if err != nil {
				continue
			}
			if len(members) > 0 {
				go warmMemberCache(ctx, conv.ID, members)
			}
		}

		cidHex := hex.EncodeToString(conv.ID)
		for _, memberID := range members {
			if bytes.Equal(memberID, currentUID) {
				continue
			}

			membersByConv[cidHex] = append(membersByConv[cidHex], memberID)
			memberKey := string(memberID)
			if _, ok := seen[memberKey]; ok {
				continue
			}
			seen[memberKey] = struct{}{}
			uniqueMembers = append(uniqueMembers, memberID)
		}
	}

	onlineByID := make(map[string]bool, len(uniqueMembers))
	if g.Presence != nil && len(uniqueMembers) > 0 {
		if result, err := g.Presence.BulkIsOnline(ctx, uniqueMembers); err == nil {
			onlineByID = result
		}
	}

	for _, conv := range convs {
		cidHex := hex.EncodeToString(conv.ID)
		onlineCount := 0
		for _, memberID := range membersByConv[cidHex] {
			if onlineByID[hex.EncodeToString(memberID)] {
				onlineCount++
			}
		}
		conv.MemberOnlineCount = onlineCount
		conv.IsOnline = onlineCount > 0
	}
}

func warmMemberCache(parent context.Context, convID []byte, memberIDs [][]byte) {
	cacheCtx, cancel := detachedContext(parent, cacheTaskTimeout)
	defer cancel()

	if err := cache.WarmMember(cacheCtx, convID, memberIDs); err != nil {
		g.Logger.Warn("roomService: failed to warm member cache",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.Error(err),
		)
	}
}
