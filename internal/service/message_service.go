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

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/queue"
	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/redis"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	"github.com/dinhdev-nu/chat-platform-api/internal/presenter"
	"github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/crypto"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type ConversationPublisher interface {
	Publish(context.Context, []byte, redis.Event) error
}

type SequenceAllocator interface {
	Next(context.Context, []byte) (uint64, error)
}

type MessageUsers interface {
	FindByIDs(ctx context.Context, ids [][]byte) (map[string]*model.User, error)
	FindByID(ctx context.Context, id []byte) (*model.User, error)
}
type MessageRooms interface {
	UpdateLastReadAt(ctx context.Context, convID, userID []byte, cursorTS *time.Time) error
	UpdateConversationLastActivity(ctx context.Context, convID, lastMsgID []byte, lastMsgText *string, activityAt time.Time) error
	GetConversationMemberIDs(ctx context.Context, convID []byte) ([][]byte, error)
	GetMemberRole(ctx context.Context, convID, userID []byte) (model.MemberRole, error)
}
type MessageCache interface {
	IsMember(ctx context.Context, convID, userID []byte) (isMember bool, cacheHit bool, err error)
	GetMembers(ctx context.Context, convID []byte) ([][]byte, error)
	WarmMember(ctx context.Context, convID []byte, userIDs [][]byte) error
	BatchIncrUnread(ctx context.Context, userIDs [][]byte, convID []byte) error
	RefreshTTL(ctx context.Context, convID []byte) error
	SetUnread(ctx context.Context, userID, convID []byte, count int64) error
	DeleteUnread(ctx context.Context, userID, convID []byte) error
}

type MessageUserCache interface {
	GetUsersWithMisses(context.Context, [][]byte) (map[string]*model.User, [][]byte, error)
	WarmUsers(context.Context, map[string]*model.User) error
}

// UserCache and Jobs may be nil: use DB reads and synchronous summary fallback.
// Cache, Sequences and Events are explicit adapters supplied by the caller.
type MessageDependencies struct {
	Rooms     MessageRooms
	Messages  repository.MessageRepository
	Users     MessageUsers
	Viewer    RoomViewer
	Cache     MessageCache
	UserCache MessageUserCache
	Sequences SequenceAllocator
	Events    ConversationPublisher
	Jobs      JobEnqueuer
	Logger    *zap.Logger
	Now       func() time.Time
}

type RoomViewer interface {
	IsViewing(userID, convID []byte) bool
}

type MessageService struct {
	roomRepo   MessageRooms
	msgRepo    repository.MessageRepository
	userRepo   MessageUsers
	roomViewer RoomViewer
	cache      MessageCache
	userCache  MessageUserCache
	sequences  SequenceAllocator
	events     ConversationPublisher
	jobs       JobEnqueuer
	logger     *zap.Logger
	now        func() time.Time
}

func NewMessageService(d MessageDependencies) *MessageService {
	return &MessageService{
		roomRepo:   d.Rooms,
		msgRepo:    d.Messages,
		userRepo:   d.Users,
		roomViewer: d.Viewer,
		cache:      d.Cache,
		userCache:  d.UserCache,
		sequences:  d.Sequences,
		events:     d.Events,
		jobs:       d.Jobs,
		logger:     loggerOrNop(d.Logger),
		now:        clockOrNow(d.Now),
	}
}

type SendMessageCommand struct {
	ConversationID []byte
	SenderID       []byte
	ParentID       []byte
	Type           model.MessageType
	Content        string
	Attachments    []*model.Attachment
}

// Send is the common use case; the wrappers retain the existing handler contracts.
func (s *MessageService) Send(ctx context.Context, cmd SendMessageCommand) (*model.MessageWithMeta, error) {
	if len(cmd.Attachments) == 0 && cmd.Content == "" {
		return nil, ae.ValidationError("Message content cannot be empty")
	}
	for _, attachment := range cmd.Attachments {
		if attachment == nil {
			return nil, ae.ValidationError("Attachment cannot be null")
		}
	}
	if err := s.requireMembership(ctx, cmd.ConversationID, cmd.SenderID); err != nil {
		return nil, err
	}
	seq, err := s.sequences.Next(ctx, cmd.ConversationID)
	if err != nil {
		return nil, ae.Internal(err)
	}
	id, err := crypto.NewUUIDv7Bytes()
	if err != nil {
		return nil, ae.Internal(err)
	}
	msg := &model.Message{
		ID:             id,
		ConversationID: cmd.ConversationID,
		SenderID:       cmd.SenderID,
		ParentID:       cmd.ParentID,
		Type:           cmd.Type,
		Content:        &cmd.Content,
		Seq:            seq,
	}
	if err := prepareAttachments(msg.ID, cmd.Attachments); err != nil {
		return nil, ae.Internal(err)
	}
	if err := s.persistMessage(ctx, msg, cmd.Attachments); err != nil {
		return nil, err
	}

	now := s.now()
	msg.CreatedAt, msg.UpdatedAt = now, now
	for _, attachment := range cmd.Attachments {
		attachment.CreatedAt = now
	}
	result := &model.MessageWithMeta{Message: msg, Attachments: cmd.Attachments, Reactions: []*model.MessageReaction{}}
	s.enqueueConversationLastActivity(ctx, msg.ConversationID, msg.ID, msg.Content, msg.CreatedAt)
	// afterSend enriches sender metadata; it must not mutate the returned metadata.
	outgoing := *result
	go s.afterSend(ctx, &outgoing)
	return result, nil
}

func (s *MessageService) SendMessage(ctx context.Context, convID, senderID []byte, msgType int8, content string, parentID []byte) (*model.Message, error) {
	result, err := s.Send(ctx, SendMessageCommand{
		ConversationID: convID,
		SenderID:       senderID,
		ParentID:       parentID,
		Type:           model.MessageType(msgType),
		Content:        content,
	})
	if err != nil {
		return nil, err
	}
	return result.Message, nil
}

func (s *MessageService) SendMessageWithAttachment(ctx context.Context, convID, senderID []byte, msgType int8, content string, parentID []byte, attachments []*model.Attachment) (*model.MessageWithMeta, error) {
	return s.Send(ctx, SendMessageCommand{
		ConversationID: convID,
		SenderID:       senderID,
		ParentID:       parentID,
		Type:           model.MessageType(msgType),
		Content:        content,
		Attachments:    attachments,
	})
}

func prepareAttachments(messageID []byte, attachments []*model.Attachment) error {
	for _, attachment := range attachments {
		if len(attachment.ID) == 0 {
			id, err := crypto.NewUUIDv7Bytes()
			if err != nil {
				return err
			}
			attachment.ID = id
		}
		attachment.MessageID = messageID
	}
	return nil
}

func (s *MessageService) persistMessage(ctx context.Context, msg *model.Message, attachments []*model.Attachment) error {
	if err := s.msgRepo.InsertMessage(ctx, msg); err != nil {
		return ae.Internal(err)
	}
	if len(attachments) == 0 {
		return nil
	}
	if err := s.msgRepo.BatchInsertAttachments(ctx, attachments); err != nil {
		cleanupCtx, cancel := detachedContext(ctx, sideEffectTimeout)
		defer cancel()
		if cleanupErr := s.msgRepo.SoftDeleteMessage(cleanupCtx, msg.ID); cleanupErr != nil {
			s.logger.Error("messageService.SendMessageWithAttachment: failed to roll back message",
				zap.String("msg_id", hex.EncodeToString(msg.ID)), zap.Error(cleanupErr))
		}
		return ae.Internal(err)
	}
	return nil
}

type messagePage struct {
	rows    []*model.Message
	limit   int
	hasMore bool
}
type messageRelationIDs struct{ messages, attachments, senders [][]byte }
type messageRelations struct {
	attachments []*model.Attachment
	reactions   []*model.MessageReaction
	users       map[string]*model.User
}

func (s *MessageService) ListMessages(ctx context.Context, uid, convID []byte, cursor *string, limit int) (*ResultPage[*model.MessageWithMeta], error) {
	if err := s.requireMembership(ctx, convID, uid); err != nil {
		return nil, err
	}
	page, err := s.readMessagePage(ctx, convID, cursor, limit)
	if err != nil {
		return nil, err
	}
	if len(page.rows) == 0 {
		return assembleMessagePage(page, messageRelations{}), nil
	}
	relations, err := s.loadMessageRelations(ctx, indexMessageRelations(page.rows))
	if err != nil {
		return nil, ae.Internal(err)
	}
	return assembleMessagePage(page, relations), nil
}

func (s *MessageService) readMessagePage(ctx context.Context, convID []byte, cursor *string, limit int) (messagePage, error) {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	var cursorTS *time.Time
	var cursorSeq *uint64
	if cursor != nil && *cursor != "" {
		ts, seq, err := decodeMsgCursor(*cursor)
		if err != nil {
			return messagePage{}, ae.BadRequest("Invalid cursor format")
		}
		cursorTS, cursorSeq = &ts, &seq
	}
	rows, err := s.msgRepo.ListMessages(ctx, convID, cursorTS, cursorSeq, int32(limit+1))
	if err != nil {
		return messagePage{}, ae.Internal(err)
	}
	page := messagePage{rows: rows, limit: limit, hasMore: len(rows) == limit+1}
	if page.hasMore {
		page.rows = rows[:limit]
	}
	return page, nil
}

// Index only the returned page; the lookahead row must not trigger enrichment.
func indexMessageRelations(rows []*model.Message) messageRelationIDs {
	ids := messageRelationIDs{messages: make([][]byte, 0, len(rows))}
	senders := make(map[string]struct{}, len(rows))
	for _, msg := range rows {
		ids.messages = append(ids.messages, msg.ID)
		if len(msg.SenderID) > 0 {
			key := string(msg.SenderID)
			if _, seen := senders[key]; !seen {
				senders[key] = struct{}{}
				ids.senders = append(ids.senders, msg.SenderID)
			}
		}
		if msg.Type != model.MessageTypeText && msg.Type != model.MessageTypeSystem {
			ids.attachments = append(ids.attachments, msg.ID)
		}
	}
	return ids
}

func (s *MessageService) loadMessageRelations(ctx context.Context, ids messageRelationIDs) (messageRelations, error) {
	var related messageRelations
	group, readCtx := errgroup.WithContext(ctx)
	if len(ids.attachments) > 0 {
		group.Go(func() error {
			var err error
			related.attachments, err = s.msgRepo.GetAttachmentsByMessageIDs(readCtx, ids.attachments)
			return err
		})
	}
	group.Go(func() error {
		var err error
		related.reactions, err = s.msgRepo.GetReactionsByMessageIDs(readCtx, ids.messages)
		return err
	})
	group.Go(func() error {
		var err error
		related.users, err = s.loadSenders(readCtx, ctx, ids.senders)
		return err
	})
	err := group.Wait()
	return related, err
}

func (s *MessageService) loadSenders(ctx, requestCtx context.Context, ids [][]byte) (map[string]*model.User, error) {
	users := make(map[string]*model.User, len(ids))
	missing := ids
	if s.userCache != nil {
		cached, misses, err := s.userCache.GetUsersWithMisses(ctx, ids)
		if err != nil {
			s.logger.Warn("messageService.ListMessages: user cache unavailable", zap.Error(err))
		} else {
			for key, user := range cached {
				users[key] = user
			}
			missing = misses
		}
	}
	if len(missing) == 0 {
		return users, nil
	}
	fromDB, err := s.userRepo.FindByIDs(ctx, missing)
	if err != nil {
		return nil, err
	}
	for key, user := range fromDB {
		users[key] = user
	}
	if s.userCache != nil && len(fromDB) > 0 {
		go s.warmSenders(requestCtx, fromDB)
	}
	return users, nil
}

func (s *MessageService) warmSenders(parent context.Context, users map[string]*model.User) {
	ctx, cancel := detachedContext(parent, cacheTaskTimeout)
	defer cancel()
	if err := s.userCache.WarmUsers(ctx, users); err != nil {
		s.logger.Warn("messageService.ListMessages: failed to warm user cache", zap.Error(err))
	}
}

// Pure assembly preserves DB ordering and non-nil collections without further I/O.
func assembleMessagePage(page messagePage, related messageRelations) *ResultPage[*model.MessageWithMeta] {
	out := &ResultPage[*model.MessageWithMeta]{Items: make([]*model.MessageWithMeta, 0, len(page.rows)), Limit: page.limit, HasMore: page.hasMore}
	byID := make(map[string]*model.MessageWithMeta, len(page.rows))
	for _, msg := range page.rows {
		meta := &model.MessageWithMeta{Message: msg, Attachments: []*model.Attachment{}, Reactions: []*model.MessageReaction{}}
		if user := related.users[string(msg.SenderID)]; user != nil {
			meta.SenderName = user.Username
			meta.SenderAvatarURL = user.AvatarURL
		}
		out.Items = append(out.Items, meta)
		byID[string(msg.ID)] = meta
	}
	for _, att := range related.attachments {
		if meta := byID[string(att.MessageID)]; meta != nil {
			meta.Attachments = append(meta.Attachments, att)
		}
	}
	for _, reaction := range related.reactions {
		if meta := byID[string(reaction.MessageID)]; meta != nil {
			meta.Reactions = append(meta.Reactions, reaction)
		}
	}
	if page.hasMore && len(page.rows) > 0 {
		last := page.rows[len(page.rows)-1]
		cursor := encodeMsgCursor(last.CreatedAt, last.Seq)
		out.NextCursor = &cursor
	}
	return out
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
		_ = s.cache.DeleteUnread(ctx, userID, convID)
	} else {
		_ = s.cache.SetUnread(ctx, userID, convID, unread)
	}

	raw := presenter.MessageReadPayload(convID, userID, lastReadMsgID, s.now())
	if len(raw) == 0 {
		s.logger.Error("messageService.MarkAsRead: failed to marshal pubsub payload")
		return nil
	}
	_ = s.events.Publish(ctx, convID, redis.Event{
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
	msg.UpdatedAt = s.now()
	msg.IsEdited = true
	affected, err := s.msgRepo.UpdateMessageContent(ctx, msg)
	if err != nil {
		return nil, ae.Internal(err)
	}
	if affected == 0 {
		return nil, ae.New(ae.ErrCannotEditMessage, "Edit window expired (24h) or message not found")
	}

	s.enqueueConversationLastActivity(ctx, msg.ConversationID, msg.ID, msg.Content, msg.UpdatedAt)

	sender, _ := s.userRepo.FindByID(ctx, msg.SenderID)
	raw := presenter.MessageEditedPayload(msg, sender)
	if len(raw) == 0 {
		s.logger.Error("messageService.EditMessage: failed to marshal pubsub payload")
		return msg, nil
	}
	_ = s.events.Publish(ctx, msg.ConversationID, redis.Event{
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

	msgText := "Message deleted"
	s.enqueueConversationLastActivity(ctx, msg.ConversationID, msg.ID, &msgText, s.now())

	raw := presenter.MessageDeletedPayload(msg.ConversationID, msgID, s.now())
	if len(raw) == 0 {
		s.logger.Error("messageService.DeleteMessage: failed to marshal pubsub payload")
		return nil
	}
	_ = s.events.Publish(ctx, msg.ConversationID, redis.Event{
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

	raw := presenter.ReactionTogglePayload(msg.ConversationID, msgID, userID, emoji, action)
	if len(raw) == 0 {
		s.logger.Error("messageService.ToggleReaction: failed to marshal pubsub payload")
		return action, nil
	}
	_ = s.events.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventToggleReaction,
		ConvID:  hex.EncodeToString(msg.ConversationID),
		Payload: raw,
	})

	return action, nil
}

func (s *MessageService) afterSend(parent context.Context, msgWithMeta *model.MessageWithMeta) {
	ctx, cancel := detachedContext(parent, systemMessageTimeout)
	defer cancel()

	msg := msgWithMeta.Message
	convHex := hex.EncodeToString(msg.ConversationID)
	senderHex := hex.EncodeToString(msg.SenderID)

	// Fan-out to members (pubsub)
	if sender, err := s.userRepo.FindByID(ctx, msg.SenderID); err != nil {
		s.logger.Warn("messageService.afterSend: failed to load sender", zap.Error(err))
	} else if sender != nil {
		msgWithMeta.SenderName = sender.Username
		msgWithMeta.SenderAvatarURL = sender.AvatarURL
	}
	raw := presenter.MessageNewPayload(msgWithMeta)
	if len(raw) == 0 {
		s.logger.Error("messageService.afterSend: failed to marshal pubsub payload",
			zap.String("msg_id", hex.EncodeToString(msg.ID)))
		return
	}
	if err := s.events.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventNewMessage,
		ConvID:  convHex,
		Payload: raw,
	}); err != nil {
		s.logger.Warn("messageService.afterSend: failed to publish message",
			zap.String("msg_id", hex.EncodeToString(msg.ID)),
			zap.Error(err),
		)
	}

	// Update unread counts (cache)
	members, cacheHit, err := s.getMembersCached(ctx, msg.ConversationID)
	allOffMembers := make([][]byte, 0)
	if err != nil {
		s.logger.Warn("messageService.afterSend: failed to load conversation members", zap.Error(err))
	} else {
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
		if err := s.cache.BatchIncrUnread(ctx, allOffMembers, msg.ConversationID); err != nil {
			s.logger.Warn("messageService.afterSend: failed to increment unread cache", zap.Error(err))
		} else if cacheHit {
			if err := s.cache.RefreshTTL(ctx, msg.ConversationID); err != nil {
				s.logger.Warn("messageService.afterSend: failed to refresh member cache TTL", zap.Error(err))
			}
		}
	}

	// The whole afterSend workflow is already asynchronous.
	if err == nil && !cacheHit && len(members) > 0 {
		if err := s.cache.WarmMember(ctx, msg.ConversationID, members); err != nil {
			s.logger.Warn("messageService.afterSend: failed to warm member cache", zap.Error(err))
		}
	}
}

func (s *MessageService) enqueueConversationLastActivity(parent context.Context, convID, msgID []byte, text *string, activityAt time.Time) {
	payload := queue.ConversationLastActivityPayload{
		ConversationID: convID,
		MessageID:      msgID,
		MessageText:    text,
		ActivityAt:     activityAt,
	}
	if s.jobs == nil {
		s.logger.Warn("messageService: stream unavailable for conversation last activity, applying sync fallback",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.String("msg_id", hex.EncodeToString(msgID)),
		)
		s.updateConversationLastActivityFallback(parent, convID, msgID, text, activityAt)
		return
	}

	enqueueCtx, cancel := detachedContext(parent, streamEnqueueTimeout)
	err := s.jobs.EnqueueJob(enqueueCtx, queue.JobUpdateConversationLastActivity, payload)
	cancel()
	if err != nil {
		s.logger.Warn("messageService: enqueue conversation last activity failed, applying sync fallback",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.String("msg_id", hex.EncodeToString(msgID)),
			zap.Error(err),
		)
		s.updateConversationLastActivityFallback(parent, convID, msgID, text, activityAt)
	}
}

func (s *MessageService) updateConversationLastActivityFallback(
	parent context.Context,
	convID, msgID []byte,
	text *string,
	activityAt time.Time,
) {
	fallbackCtx, cancel := detachedContext(parent, sideEffectTimeout)
	defer cancel()
	if fallbackErr := s.roomRepo.UpdateConversationLastActivity(fallbackCtx, convID, msgID, text, activityAt); fallbackErr != nil {
		s.logger.Error("messageService: sync fallback update last activity failed",
			zap.String("conv_id", hex.EncodeToString(convID)),
			zap.String("msg_id", hex.EncodeToString(msgID)),
			zap.Error(fallbackErr),
		)
	}
}

func (s *MessageService) getMembersCached(ctx context.Context, convID []byte) ([][]byte, bool, error) {
	members, err := s.cache.GetMembers(ctx, convID)
	if err == nil && len(members) > 0 {
		return members, true, nil
	}
	if err != nil {
		s.logger.Warn("member cache unavailable, falling back to DB", zap.Error(err))
	}

	members, err = s.roomRepo.GetConversationMemberIDs(ctx, convID)
	if err != nil {
		return nil, false, ae.Internal(err)
	}
	return members, false, nil
}

func (s *MessageService) requireMembership(ctx context.Context, convID, senderUID []byte) error {
	isMember, hit, err := s.cache.IsMember(ctx, convID, senderUID)
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
