package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis/cache"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type MessageService struct {
	roomRepo r.RoomRepository
	msgRepo  r.MessageRepository
	userRepo r.UserRepository

	roomViewer RoomViewer
}

func NewMessageService(rr r.RoomRepository, mg r.MessageRepository, ur r.UserRepository, rv RoomViewer) *MessageService {
	return &MessageService{
		roomRepo:   rr,
		msgRepo:    mg,
		userRepo:   ur,
		roomViewer: rv,
	}
}

func (s *MessageService) SendMessage(
	ctx context.Context, convID, senderUID []byte,
	msgType int8, content string, parentID []byte,
) (*model.Message, error) {
	if err := s.requireMembership(ctx, convID, senderUID); err != nil {
		return nil, err
	}

	seqVal, err := GetNextSeqFromRedis(ctx, s.msgRepo, convID)
	if err != nil {
		return nil, ae.Internal(err)
	}

	mID, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, ae.Internal(err)
	}

	arg := &model.Message{
		ID:             mID,
		ConversationID: convID,
		SenderID:       senderUID,
		ParentID:       parentID,
		Type:           model.MessageType(msgType),
		Content:        &content,
		Seq:            uint64(seqVal),
	}
	if err := s.msgRepo.InsertMessage(ctx, arg); err != nil {
		return nil, ae.Internal(err)
	}
	now := time.Now()
	arg.CreatedAt = now
	arg.UpdatedAt = now

	go s.afterSend(context.Background(), &model.MessageWithMeta{
		Message:     arg,
		Attachments: []*model.Attachment{},
		Reactions:   []*model.MessageReaction{},
	})

	return arg, nil
}

func (s *MessageService) SendMessageWithAttachment(
	ctx context.Context, convID, senderUID []byte,
	msgType int8, content string, parentID []byte,
	attachments []*model.Attachment,
) (*model.MessageWithMeta, error) {
	if err := s.requireMembership(ctx, convID, senderUID); err != nil {
		return nil, err
	}

	mID, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, ae.Internal(err)
	}

	seqVal, err := GetNextSeqFromRedis(ctx, s.msgRepo, convID)
	if err != nil {
		return nil, ae.Internal(err)
	}
	for _, att := range attachments {
		if len(att.ID) == 0 {
			att.ID, err = crypto.NewUUIDv7Bytes()
			if err != nil {
				return nil, ae.Internal(err)
			}
		}
		att.MessageID = mID
	}
	arg := &model.Message{
		ID:             mID,
		ConversationID: convID,
		SenderID:       senderUID,
		ParentID:       parentID,
		Type:           model.MessageType(msgType),
		Content:        &content,
		Seq:            uint64(seqVal),
	}

	if err := s.msgRepo.InsertMessage(ctx, arg); err != nil {
		return nil, ae.Internal(err)
	}

	if err := s.msgRepo.BatchInsertAttachments(ctx, attachments); err != nil {
		_ = s.msgRepo.SoftDeleteMessage(context.Background(), mID)
		return nil, ae.Internal(err)
	}

	now := time.Now()
	arg.CreatedAt = now
	arg.UpdatedAt = now
	for _, att := range attachments {
		att.CreatedAt = now
	}

	msgWithMeta := &model.MessageWithMeta{
		Message:     arg,
		Attachments: attachments,
		Reactions:   []*model.MessageReaction{},
	}
	go s.afterSend(context.Background(), msgWithMeta)

	return msgWithMeta, nil
}

func (s *MessageService) afterSend(ctx context.Context, msgWithMeta *model.MessageWithMeta) {
	msg := msgWithMeta.Message
	convHex := hex.EncodeToString(msg.ConversationID)
	senderHex := hex.EncodeToString(msg.SenderID)

	// Fan-out to members (pubsub)
	if sender, err := s.userRepo.FindByID(ctx, msg.SenderID); err == nil && sender != nil {
		msgWithMeta.SenderName = sender.Username
		msgWithMeta.SenderAvatarURL = sender.AvatarURL
	}
	raw := messageNewPayload(msgWithMeta)
	if len(raw) == 0 {
		g.Logger.Error("messageService.afterSend: failed to marshal pubsub payload",
			zap.String("msg_id", hex.EncodeToString(msg.ID)))
		return
	}
	_ = g.PubSub.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventNewMessage,
		ConvID:  convHex,
		Payload: raw,
	})

	// Update unread counts (cache)
	members, cacheHit, err := s.getMembersCached(ctx, msg.ConversationID)
	allOffMembers := make([][]byte, 0)
	if err == nil {
		for _, mID := range members {
			if hex.EncodeToString(mID) == senderHex {
				continue // skip sender
			}
			if s.roomViewer != nil && s.roomViewer.IsViewing(mID, msg.ConversationID) {
				continue
			}
			allOffMembers = append(allOffMembers, mID)
		}
	}
	if len(allOffMembers) > 0 {
		go func(hit bool) {
			bg := context.Background()
			_ = cache.BatchIncrUnread(bg, allOffMembers, msg.ConversationID)
			if hit {
				_ = cache.RefreshTTL(bg, msg.ConversationID)
			}
		}(cacheHit)
	}

	// Warm cache asynchronously
	if err == nil && !cacheHit && len(members) > 0 {
		go func() {
			_ = cache.WarmMember(context.Background(), msg.ConversationID, members)
		}()
	}

	// Last msg + act (stream worker) ( chưa triển khai ngay )
	_ = s.roomRepo.UpdateConversationLastActivity(ctx, msg.ConversationID, msg.ID, msg.Content)
}

func (s *MessageService) getMembersCached(ctx context.Context, convID []byte) ([][]byte, bool, error) {
	members, err := cache.GetMembers(ctx, convID)
	if err == nil && len(members) > 0 {
		return members, true, nil
	}
	if err != nil {
		g.Logger.Warn("member cache unavailable, falling back to DB", zap.Error(err))
	}

	members, err = s.roomRepo.GetConversationMemberIDs(ctx, convID)
	if err != nil {
		return nil, false, ae.Internal(err)
	}
	return members, false, nil
}

func (s *MessageService) requireMembership(ctx context.Context, convID, senderUID []byte) error {
	isMember, hit, err := cache.IsMember(ctx, convID, senderUID)
	if err == nil && hit {
		if isMember {
			return nil
		}
		// Negative cache can be stale immediately after a member is added.
		// Verify against DB before denying access.
	}

	// Fallback db
	role, err := s.roomRepo.GetMemberRole(ctx, convID, senderUID)
	if err != nil {
		return ae.Internal(err)
	}
	if role == 0 {
		return ae.New(ae.ErrNotAMember, "User is not a member of the conversation")
	}
	return nil
}

func (s *MessageService) getMemberRoleRequired(ctx context.Context, convID, userID []byte) (model.MemberRole, error) {
	role, err := s.roomRepo.GetMemberRole(ctx, convID, userID)
	if err != nil {
		return 0, ae.Internal(err)
	}
	if role == 0 {
		return 0, ae.New(ae.ErrNotAMember, "User is not a member of the conversation")
	}
	return role, nil
}

func (s *MessageService) ListMessages(ctx context.Context, uid, convID []byte, cursor *string, limit int) (*ResultPage[*model.MessageWithMeta], error) {
	if err := s.requireMembership(ctx, convID, uid); err != nil {
		return nil, err
	}

	const maxLimit = 50
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}
	fetch := limit + 1

	var cursorTS *time.Time
	var cursorSeq *uint64
	if cursor != nil && *cursor != "" {
		ts, seq, err := decodeMsgCursor(*cursor)
		if err != nil {
			return nil, ae.BadRequest("Invalid cursor format")
		}
		cursorTS, cursorSeq = &ts, &seq
	}

	// 1. Get messages from DB
	rows, err := s.msgRepo.ListMessages(ctx, convID, cursorTS, cursorSeq, int32(fetch))
	if err != nil {
		return nil, ae.Internal(err)
	}

	hasMore := len(rows) == fetch
	if hasMore {
		rows = rows[:limit]
	}

	if len(rows) == 0 {
		return &ResultPage[*model.MessageWithMeta]{Items: []*model.MessageWithMeta{}, Limit: limit}, nil
	}

	// Pre-allocate
	msgIDs := make([][]byte, len(rows))
	msgAttIDs := make([][]byte, 0, len(rows))
	msgResult := make([]*model.MessageWithMeta, len(rows))
	msgMap := make(map[string]*model.MessageWithMeta, len(rows))

	seenSenders := make(map[string]struct{}, len(rows))
	senderIDs := make([][]byte, 0, len(rows))

	for i, m := range rows {
		msgIDs[i] = m.ID

		// Deduplicate sender IDs trước khi truyền xuống cache layer
		if len(m.SenderID) > 0 {
			if k := string(m.SenderID); k != "" {
				if _, ok := seenSenders[k]; !ok {
					seenSenders[k] = struct{}{}
					senderIDs = append(senderIDs, m.SenderID)
				}
			}
		}

		// Chỉ fetch attachment cho các message type có thể có file
		switch m.Type {
		case model.MessageTypeText, model.MessageTypeSystem:

		default:
			msgAttIDs = append(msgAttIDs, m.ID)
		}

		meta := &model.MessageWithMeta{
			Message:     m,
			Attachments: []*model.Attachment{},
			Reactions:   []*model.MessageReaction{},
		}
		msgResult[i] = meta
		msgMap[string(m.ID)] = meta
	}

	// 2. Parallel fetch using errgroup
	eg, gCtx := errgroup.WithContext(ctx)

	var atts []*model.Attachment
	if len(msgAttIDs) > 0 {
		eg.Go(func() error {
			var err error
			atts, err = s.msgRepo.GetAttachmentsByMessageIDs(gCtx, msgAttIDs) // chỉ query IDs có attachment
			return err
		})
	}

	var reactions []*model.MessageReaction
	eg.Go(func() error {
		var err error
		reactions, err = s.msgRepo.GetReactionsByMessageIDs(gCtx, msgIDs) // tất cả message đều có thể có reaction
		return err
	})

	var users map[string]*model.User
	eg.Go(func() error {
		users = make(map[string]*model.User, len(senderIDs))
		missingIDs := senderIDs

		if g.Session != nil {
			cachedUsers, misses, err := g.Session.GetUsersWithMisses(gCtx, senderIDs)
			if err == nil {
				users = cachedUsers
				missingIDs = misses
			} else {
				g.Logger.Warn("messageService.ListMessages: user cache unavailable", zap.Error(err))
			}
		}

		if len(missingIDs) == 0 {
			return nil
		}

		dbUsers, err := s.userRepo.FindByIDs(gCtx, missingIDs)
		if err != nil {
			return err
		}
		for k, user := range dbUsers {
			users[k] = user
		}

		if g.Session != nil && len(dbUsers) > 0 {
			go func() {
				warmCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := g.Session.WarmUsers(warmCtx, dbUsers); err != nil {
					g.Logger.Warn("messageService.ListMessages: failed to warm user cache", zap.Error(err))
				}
			}()
		}

		return nil
	})

	if err := eg.Wait(); err != nil {
		return nil, ae.Internal(err)
	}

	// 3. Mapping data — O(N)
	for _, att := range atts {
		if meta, exists := msgMap[string(att.MessageID)]; exists {
			meta.Attachments = append(meta.Attachments, att)
		}
	}
	for _, react := range reactions {
		if meta, exists := msgMap[string(react.MessageID)]; exists {
			meta.Reactions = append(meta.Reactions, react)
		}
	}
	for _, meta := range msgResult {
		if user, exists := users[string(meta.SenderID)]; exists {
			meta.SenderName = user.Username
			meta.SenderAvatarURL = user.AvatarURL
		}
	}

	// 4. Build next cursor
	var nextCursor *string
	if hasMore {
		last := rows[len(rows)-1]
		s := encodeMsgCursor(last.CreatedAt, last.Seq)
		nextCursor = &s
	}

	return &ResultPage[*model.MessageWithMeta]{
		Items:      msgResult,
		NextCursor: nextCursor,
		HasMore:    hasMore,
		Limit:      limit,
	}, nil
}

func (s *MessageService) MarkAsRead(ctx context.Context, convID, userID, lastReadMsgID []byte) error {
	if err := s.requireMembership(ctx, convID, userID); err != nil {
		return err
	}

	cursorTS, err := s.msgRepo.GetMessageCursorTS(ctx, lastReadMsgID, convID)
	if err != nil {
		return ae.Internal(err)
	}
	if cursorTS == nil {
		return ae.New(ae.ErrInvalidCursor, "Invalid cursor")
	}

	if err := s.roomRepo.UpdateLastReadAt(ctx, convID, userID, cursorTS); err != nil {
		return ae.Internal(err)
	}
	unread, err := s.msgRepo.GetUnreadCountByWatermark(ctx, userID, convID)
	if err != nil {
		_ = cache.DeleteUnread(ctx, userID, convID)
	} else {
		_ = cache.SetUnread(ctx, userID, convID, unread)
	}

	raw := messageReadPayload(convID, userID, lastReadMsgID, time.Now())
	if len(raw) == 0 {
		g.Logger.Error("messageService.MarkAsRead: failed to marshal pubsub payload")
		return nil
	}
	_ = g.PubSub.Publish(ctx, convID, redis.Event{
		Type:    redis.EventReadMessage,
		ConvID:  hex.EncodeToString(convID),
		Payload: raw,
	})
	return nil
}

func (s *MessageService) EditMessage(ctx context.Context, userID, msgID []byte, newContent string) (*model.Message, error) {
	newContent = strings.TrimSpace(newContent)
	if newContent == "" {
		return nil, ae.ValidationError("Message content cannot be empty")
	}

	msg, err := s.msgRepo.GetMessageByID(ctx, msgID)
	if err != nil {
		return nil, ae.Internal(err)
	}
	if msg == nil {
		return nil, ae.New(ae.ErrMessageNotFound, "Message not found")
	}
	if msg.IsDeleted {
		return nil, ae.New(ae.ErrMessageDeleted, "Message has been deleted")
	}
	if !bytes.Equal(msg.SenderID, userID) {
		return nil, ae.New(ae.ErrForbidden, "User is not the sender of the message")
	}
	if msg.Type != model.MessageTypeText {
		return nil, ae.New(ae.ErrCannotEditMessage, "Only text messages can be edited")
	}
	msg.Content = &newContent
	msg.UpdatedAt = time.Now()
	msg.IsEdited = true
	affected, err := s.msgRepo.UpdateMessageContent(ctx, msg)
	if err != nil {
		return nil, ae.Internal(err)
	}
	if affected == 0 {
		return nil, ae.New(ae.ErrCannotEditMessage, "Edit window expired (24h) or message not found")
	}

	go func() {
		// ctx with timeout
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.roomRepo.UpdateConversationLastActivity(bg, msg.ConversationID, msg.ID, msg.Content)
	}()

	sender, _ := s.userRepo.FindByID(ctx, msg.SenderID)
	raw := messageEditedPayload(msg, sender)
	if len(raw) == 0 {
		g.Logger.Error("messageService.EditMessage: failed to marshal pubsub payload")
		return msg, nil
	}
	_ = g.PubSub.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventEditMessage,
		ConvID:  hex.EncodeToString(msg.ConversationID),
		Payload: raw,
	})

	return msg, nil
}

func (s *MessageService) DeleteMessage(ctx context.Context, userID, msgID []byte) error {
	msg, err := s.msgRepo.GetMessageByID(ctx, msgID)
	if err != nil {
		return ae.Internal(err)
	}
	if msg == nil {
		return ae.New(ae.ErrMessageNotFound, "Message not found")
	}

	role, err := s.getMemberRoleRequired(ctx, msg.ConversationID, userID)
	if err != nil {
		return err
	}
	if msg.IsDeleted {
		return ae.New(ae.ErrMessageDeleted, "Message has already been deleted")
	}
	if msg.Type == model.MessageTypeSystem {
		return ae.New(ae.ErrCannotDeleteMessage, "System messages cannot be deleted")
	}
	isSender := bytes.Equal(msg.SenderID, userID)
	if !isSender && role != model.RoleAdmin && role != model.RoleOwner {
		return ae.Forbidden("User does not have permission to delete this message")
	}

	if err := s.msgRepo.SoftDeleteMessage(ctx, msgID); err != nil {
		return ae.Internal(err)
	}

	go func() {
		// ctx with timeout
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		msgText := "Message deleted"
		_ = s.roomRepo.UpdateConversationLastActivity(bg, msg.ConversationID, msg.ID, &msgText)
	}()

	raw := messageDeletedPayload(msg.ConversationID, msgID, time.Now())
	if len(raw) == 0 {
		g.Logger.Error("messageService.DeleteMessage: failed to marshal pubsub payload")
		return nil
	}
	_ = g.PubSub.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventDelMessage,
		ConvID:  hex.EncodeToString(msg.ConversationID),
		Payload: raw,
	})

	return nil
}

func (s *MessageService) ToggleReaction(ctx context.Context, userID, msgID []byte, emoji string) (string, error) {
	emoji = strings.TrimSpace(emoji)
	if emoji == "" {
		return "", ae.ValidationError("Emoji is required")
	}
	if utf8.RuneCountInString(emoji) > 10 {
		return "", ae.ValidationError("Emoji is too long")
	}

	msg, err := s.msgRepo.GetMessageByID(ctx, msgID)
	if err != nil {
		return "", ae.Internal(err)
	}
	if msg == nil {
		return "", ae.New(ae.ErrMessageNotFound, "Message not found")
	}
	if err := s.requireMembership(ctx, msg.ConversationID, userID); err != nil {
		return "", ae.Internal(err)
	}
	if msg.IsDeleted {
		return "", ae.New(ae.ErrMessageDeleted, "Message has been deleted")
	}

	affected, err := s.msgRepo.InsertMessageReaction(ctx, msgID, userID, emoji)
	if err != nil {
		return "", ae.Internal(err)
	}

	action := "added"
	if affected == 0 {
		if err := s.msgRepo.DeleteMessageReaction(ctx, msgID, userID, emoji); err != nil {
			return "", ae.Internal(err)
		}
		action = "removed"
	}

	raw := reactionTogglePayload(msg.ConversationID, msgID, userID, emoji, action)
	if len(raw) == 0 {
		g.Logger.Error("messageService.ToggleReaction: failed to marshal pubsub payload")
		return action, nil
	}
	_ = g.PubSub.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventToggleReaction,
		ConvID:  hex.EncodeToString(msg.ConversationID),
		Payload: raw,
	})

	return action, nil
}

func decodeMsgCursor(cursor string) (time.Time, uint64, error) {
	raw, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, 0, err
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor format")
	}
	ms, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0, err
	}
	seq, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0, err
	}
	return time.UnixMilli(ms), seq, nil
}

func encodeMsgCursor(ts time.Time, seq uint64) string {
	raw := fmt.Sprintf("%d:%d", ts.UnixMilli(), seq)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}
