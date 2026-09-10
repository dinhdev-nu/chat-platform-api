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

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	"github.com/dinhdev-nu/chat-platform-api/internal/presenter"
	"github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"github.com/dinhdev-nu/chat-platform-api/pkg/types"
	"go.uber.org/zap"
)

type PresenceReader interface {
	BulkPresenceReader
	IsOnline(context.Context, []byte) (bool, error)
}

type RoomUsers interface {
	FindByID(ctx context.Context, id []byte) (*model.User, error)
	FindActiveIDs(ctx context.Context, ids [][]byte) (map[string]bool, error)
}
type RoomMessages interface {
	GetUnreadCountByWatermark(ctx context.Context, userID, convID []byte) (int64, error)
	InsertSystemMessage(ctx context.Context, msg *model.Message) error
}
type RoomCache interface {
	GetUnreads(ctx context.Context, userID []byte, convIDs [][]byte) (map[string]int64, error)
	SetUnread(ctx context.Context, userID, convID []byte, count int64) error
	UpdateMembership(ctx context.Context, convID []byte, apply func(context.Context, repository.ConversationMembers) error) error
	DeleteUnread(ctx context.Context, userID, convID []byte) error
	GetMembers(ctx context.Context, convID []byte) ([][]byte, error)
}

type RoomDependencies struct {
	Users     RoomUsers
	Rooms     repository.RoomRepository
	Messages  RoomMessages
	Cache     RoomCache
	Sequences SequenceAllocator
	Events    ConversationPublisher
	Controls  ControlPublisher
	Presence  PresenceReader
	Jobs      JobEnqueuer
	Logger    *zap.Logger
}

type RoomService struct {
	userRepo  RoomUsers
	roomRepo  repository.RoomRepository
	msgRepo   RoomMessages
	cache     RoomCache
	sequences SequenceAllocator
	events    ConversationPublisher
	controls  ControlPublisher
	presence  PresenceReader
	jobs      JobEnqueuer
	logger    *zap.Logger
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

func NewRoomService(d RoomDependencies) *RoomService {
	return &RoomService{
		userRepo:  d.Users,
		roomRepo:  d.Rooms,
		msgRepo:   d.Messages,
		cache:     d.Cache,
		sequences: d.Sequences,
		events:    d.Events,
		controls:  d.Controls,
		presence:  d.Presence,
		jobs:      d.Jobs,
		logger:    loggerOrNop(d.Logger),
	}
}

func (s *RoomService) CreateDM(ctx context.Context, currentUID, targetUserID []byte) (*model.Conversation, bool, error) {
	if bytes.Equal(currentUID, targetUserID) {
		return nil, false, ae.New(ae.ErrInvalidInput, "Cannot create DM with yourself")
	}

	// 1/ Verify the target user exists and is active using the repository.
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
	if err := s.cache.UpdateMembership(ctx, convID, func(writeCtx context.Context, membersRepo repository.ConversationMembers) error {
		return membersRepo.BatchInsertConversationMembers(writeCtx, members)
	}); err != nil {
		return nil, false, ae.Internal(err)
	}

	// Warm member cache
	go s.warmMemberCache(ctx, convID)

	conv, err := s.roomRepo.GetConversationByID(ctx, convID)
	if err != nil {
		return nil, false, ae.Internal(err)
	}

	memberIDs := [][]byte{currentUID, targetUserID}
	currentItem := s.conversationListItemForUser(ctx, conv, currentUID, model.RoleMember, false, targetUser, memberIDs)
	targetItem := s.conversationListItemForUser(ctx, conv, targetUserID, model.RoleMember, false, currentUser, memberIDs)
	s.publishConvSubscribe(ctx, currentUID, convID, presenter.ConversationCreatedPayload(currentItem))
	s.publishConvSubscribe(ctx, targetUserID, convID, presenter.ConversationCreatedPayload(targetItem))

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

	memberIDs, err := s.validateGroupMembers(ctx, currentUID, req.MemberUIDs)
	if err != nil {
		return nil, err
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

	members := buildGroupMemberships(convID, allMemberIDs)

	if err := s.cache.UpdateMembership(ctx, convID, func(writeCtx context.Context, membersRepo repository.ConversationMembers) error {
		return membersRepo.BatchInsertConversationMembers(writeCtx, members)
	}); err != nil {
		return nil, ae.Internal(err)
	}

	// Warm member cache
	go s.warmMemberCache(ctx, convID)

	s.enqueueSystemMessage(ctx, convID, currentUID, "Group created")

	conv, err := s.roomRepo.GetConversationByID(ctx, convID)
	if err != nil {
		return nil, ae.Internal(err)
	}

	for i, memberID := range allMemberIDs {
		role := model.RoleMember
		if i == 0 {
			role = model.RoleAdmin
		}
		item := s.conversationListItemForUser(ctx, conv, memberID, role, false, nil, allMemberIDs)
		s.publishConvSubscribe(ctx, memberID, convID, presenter.ConversationCreatedPayload(item))
	}

	return conv, nil
}

func (s *RoomService) validateGroupMembers(ctx context.Context, creatorID []byte, requestedIDs []types.HexID) ([][]byte, error) {
	memberIDs := make([][]byte, 0, len(requestedIDs))
	seen := make(map[string]struct{}, len(requestedIDs)+1)
	seen[string(creatorID)] = struct{}{}
	for _, mID := range requestedIDs {
		memberID := []byte(mID)
		if len(memberID) != 16 {
			return nil, ae.ValidationError("Invalid member user ID")
		}
		if bytes.Equal(memberID, creatorID) {
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
	return memberIDs, nil
}

// The first ID is the creator; remaining IDs have already been validated.
func buildGroupMemberships(convID []byte, allMemberIDs [][]byte) []*model.ConversationMember {
	members := make([]*model.ConversationMember, 0, len(allMemberIDs))
	for i, memberID := range allMemberIDs {
		role := model.RoleMember
		if i == 0 {
			role = model.RoleAdmin
		}
		members = append(members, &model.ConversationMember{
			ConversationID: convID,
			UserID:         memberID,
			Role:           role,
		})
	}
	return members
}

func (s *RoomService) ListConversations(ctx context.Context, uid []byte, cursor *string, limit int) (*ResultPage[*model.ConversationListRow], error) {
	// Không cache vì các trường thay đổi thường xuyên (last_message_at, v.v.) và có phân trang
	limit = normalizePageLimit(limit)
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
	unreadMap, err := s.cache.GetUnreads(ctx, uid, convIDs)
	if err != nil {
		unreadMap = map[string]int64{} // Fallback nếu cache lỗi
	}
	for _, c := range convs {
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
				if err := s.cache.SetUnread(cacheCtx, uid, cid, count); err != nil {
					s.logger.Warn("roomService.ListConversations: failed to warm unread cache",
						zap.String("conv_id", hex.EncodeToString(cid)),
						zap.Error(err),
					)
				}
			}(ctx, c.ID, uid, unread)
		}
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
	err = s.cache.UpdateMembership(ctx, convUID, func(writeCtx context.Context, membersRepo repository.ConversationMembers) error {
		return membersRepo.InsertConversationMember(writeCtx, arg)
	})
	if err != nil {
		return ae.Internal(err)
	}
	s.enqueueSystemMessage(ctx, convUID, actorUID, fmt.Sprintf("%s added %s", actorName, targetUser.Username))
	// Push notification
	convHex := hex.EncodeToString(convUID)
	actor := s.memberActorSummary(ctx, actorUID, actorName)
	member := presenter.UserSummaryFromUser(targetUser)
	payload := presenter.MemberPayload(redis.EventMemberAdded, convUID, targetUID, member, actor, nil)
	_ = s.events.Publish(ctx, convUID, redis.Event{
		Type:    redis.EventMemberAdded,
		ConvID:  convHex,
		Payload: payload,
	})
	targetPayload := payload
	conv, err := s.roomRepo.GetConversationByID(ctx, convUID)
	if err == nil && conv != nil {
		memberIDs, _ := s.roomRepo.GetConversationMemberIDs(ctx, convUID)
		item := s.conversationListItemForUser(ctx, conv, targetUID, model.RoleMember, false, nil, memberIDs)
		targetPayload = presenter.MemberPayload(redis.EventMemberAdded, convUID, targetUID, member, actor, &item)
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
	err = s.cache.UpdateMembership(ctx, convID, func(writeCtx context.Context, membersRepo repository.ConversationMembers) error {
		return membersRepo.DeleteConversationMember(writeCtx, convID, targetUID)
	})
	if err != nil {
		return ae.Internal(err)
	}

	go s.removeMemberUnread(ctx, convID, targetUID)

	action := "left"
	if !isSelf {
		action = "removed"
	}
	s.enqueueSystemMessage(ctx, convID, actorUID, fmt.Sprintf("%s %s", targetName, action))

	// Push notification
	convHex := hex.EncodeToString(convID)
	actor := s.memberActorSummary(ctx, actorUID, actorName)
	member := presenter.UserSummaryFromParts(targetUID, targetName, nil)
	if targetUser != nil {
		member = presenter.UserSummaryFromUser(targetUser)
	} else if isSelf {
		member = actor
	}
	payload := presenter.MemberPayload(redis.EventMemberRemoved, convID, targetUID, member, actor, nil)
	_ = s.events.Publish(ctx, convID, redis.Event{
		Type:    redis.EventMemberRemoved,
		ConvID:  convHex,
		Payload: payload,
	})
	s.publishConvUnsubscribe(ctx, targetUID, convID, payload)

	return nil
}

// Run after persistence with a detached context, even if the request is canceled.
func (s *RoomService) removeMemberUnread(parent context.Context, convID, targetUID []byte) {
	cacheCtx, cancel := detachedContext(parent, sideEffectTimeout)
	defer cancel()

	if err := s.cache.DeleteUnread(cacheCtx, targetUID, convID); err != nil {
		s.logger.Warn("roomService.RemoveMember: failed to delete unread cache",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.Error(err),
		)
	}
}

func (s *RoomService) memberActorSummary(ctx context.Context, actorUID []byte, actorName string) *presenter.UserSummary {
	actor := presenter.UserSummaryFromParts(actorUID, actorName, nil)
	if actorUser, err := s.userRepo.FindByID(ctx, actorUID); err == nil && actorUser != nil {
		actor = presenter.UserSummaryFromUser(actorUser)
	}
	return actor
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

	seqVal, err := s.sequences.Next(ctx, convID)
	if err != nil {
		s.logger.Error("roomService: failed to allocate system message seq",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.Error(err),
		)
		return
	}

	msgID, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		s.logger.Error("roomService: failed to create system message id",
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
	if s.jobs == nil {
		s.logger.Warn("roomService: stream unavailable for system message, applying sync fallback",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.String("msg_id", hex.EncodeToString(msgID)),
		)
		s.insertSystemMessageFallback(parent, payload)
		return
	}
	if err := s.jobs.EnqueueJob(ctx, queue.JobCreateConversationSystemMessage, payload); err != nil {
		s.logger.Warn("roomService: enqueue system message failed, applying sync fallback",
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
		s.logger.Error("roomService: sync fallback insert system message failed",
			zap.String("conv_id", hex.EncodeToString(payload.ConversationID)),
			zap.String("msg_id", hex.EncodeToString(payload.MessageID)),
			zap.Error(err),
		)
		return
	}
	if err := s.roomRepo.UpdateConversationLastActivity(ctx, payload.ConversationID, payload.MessageID, &content, payload.ActivityAt); err != nil {
		s.logger.Error("roomService: sync fallback update last activity failed",
			zap.String("conv_id", hex.EncodeToString(payload.ConversationID)),
			zap.String("msg_id", hex.EncodeToString(payload.MessageID)),
			zap.Error(err),
		)
	}
}

func (s *RoomService) publishConvSubscribe(ctx context.Context, userID, convID []byte, payload json.RawMessage) {
	publishControlEvent(ctx, s.controls, s.logger, userID, conversationSysEvent{
		Type:    sysConvSubscribe,
		ConvID:  hex.EncodeToString(convID),
		Payload: payload,
	})
}

func (s *RoomService) publishConvUnsubscribe(ctx context.Context, userID, convID []byte, payload json.RawMessage) {
	publishControlEvent(ctx, s.controls, s.logger, userID, conversationSysEvent{
		Type:    sysConvUnsubscribe,
		ConvID:  hex.EncodeToString(convID),
		Payload: payload,
	})
}

func (s *RoomService) attachConversationPresence(ctx context.Context, currentUID []byte, convs []*model.ConversationListRow) {
	if len(convs) == 0 {
		return
	}

	membersByConv := make(map[string][][]byte, len(convs))
	uniqueMembers := make([][]byte, 0)
	seen := make(map[string]struct{})

	for _, conv := range convs {
		members, err := s.cache.GetMembers(ctx, conv.ID)
		if err != nil {
			continue
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
	if s.presence != nil && len(uniqueMembers) > 0 {
		if result, err := s.presence.BulkIsOnline(ctx, uniqueMembers); err == nil {
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

func (s *RoomService) warmMemberCache(parent context.Context, convID []byte) {
	cacheCtx, cancel := detachedContext(parent, cacheTaskTimeout)
	defer cancel()

	if _, err := s.cache.GetMembers(cacheCtx, convID); err != nil {
		s.logger.Warn("roomService: failed to warm member cache",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.Error(err),
		)
	}
}

// Load presence at the service boundary; the shared mapper only consumes data.
func (s *RoomService) conversationListItemForUser(ctx context.Context, conv *model.Conversation, userID []byte, role model.MemberRole, isMuted bool, peer *model.User, memberIDs [][]byte) dto.ConversationListItem {
	onlineByID := map[string]bool{}
	if s.presence != nil {
		if conv != nil && conv.Type == model.ConvTypeDirect {
			if peer != nil && len(peer.ID) > 0 {
				online, err := s.presence.IsOnline(ctx, peer.ID)
				onlineByID[hex.EncodeToString(peer.ID)] = err == nil && online
			}
		} else {
			others := make([][]byte, 0, len(memberIDs))
			for _, id := range memberIDs {
				if len(id) > 0 && !bytes.Equal(id, userID) {
					others = append(others, id)
				}
			}
			if len(others) > 0 {
				if online, err := s.presence.BulkIsOnline(ctx, others); err == nil {
					onlineByID = online
				}
			}
		}
	}
	return presenter.ConversationForUser(presenter.ConversationView{
		Conversation: conv, UserID: userID, Role: role, IsMuted: isMuted, Peer: peer, MemberIDs: memberIDs, OnlineByID: onlineByID,
	})
}
