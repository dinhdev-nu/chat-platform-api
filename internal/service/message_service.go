package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

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

	roomViewer RoomViewer
}

func NewMessageService(rr r.RoomRepository, mg r.MessageRepository, rv RoomViewer) *MessageService {
	return &MessageService{
		roomRepo:   rr,
		msgRepo:    mg,
		roomViewer: rv,
	}
}

func (s *MessageService) SendMessage(
	ctx context.Context, convID, senderUID []byte,
	msgType int8, content string, parentID []byte,
) (*model.Message, error) {
	// Verify membership, permissions, etc. (omitted for brevity)
	if err := s.requireMembership(ctx, convID, senderUID); err != nil {
		return nil, err
	}
	//seq
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

	// async tasks: fan-out, unread, last_act (stream worker), notijob (pubsub)
	go s.afterSend(context.Background(), arg)

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

	for _, att := range attachments {
		att.MessageID = mID
	}
	if err := s.msgRepo.BatchInsertAttachments(ctx, attachments); err != nil {
		return nil, ae.Internal(err)
	}

	go s.afterSend(context.Background(), arg)

	return &model.MessageWithMeta{
		Message:     arg,
		Attachments: attachments,
	}, nil
}

func (s *MessageService) afterSend(ctx context.Context, msg *model.Message) {
	convHex := hex.EncodeToString(msg.ConversationID)
	senderHex := hex.EncodeToString(msg.SenderID)
	msgHex := hex.EncodeToString(msg.ID)

	// Fan-out to members (pubsub)
	payload := map[string]any{
		"event":     redis.EventNewMessage,
		"msg_id":    msgHex,
		"conv_id":   convHex,
		"sender_id": senderHex,
		"seq":       msg.Seq,
		"type":      msg.Type,
	}
	raw, err := g.PubSub.ToJsonRawMessage(payload)
	if err != nil {
		g.Logger.Error("messageService.afterSend: failed to marshal pubsub payload: %v", zap.Error(err))
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
			if hit {
				_ = cache.RefreshTTL(context.Background(), msg.ConversationID)
			} else {
				_ = cache.BatchIncrUnread(context.Background(), allOffMembers, msg.ConversationID)
			}
		}(cacheHit)
	}

	// Warm cache asynchronously
	go func() {
		_ = cache.WarmMember(context.Background(), msg.ConversationID, members)
	}()

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
		if !isMember {
			return ae.New(ae.ErrNotAMember, "User is not a member of the conversation")
		}
		return nil
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
		return &ResultPage[*model.MessageWithMeta]{Items: []*model.MessageWithMeta{}}, nil
	}

	// Pre-allocate map and slice
	msgIDs := make([][]byte, len(rows))
	msgResult := make([]*model.MessageWithMeta, len(rows))
	msgMap := make(map[string]*model.MessageWithMeta, len(rows))

	for i, m := range rows {
		msgIDs[i] = m.ID
		meta := &model.MessageWithMeta{
			Message:     m,
			Attachments: []*model.Attachment{},
			Reactions:   []*model.MessageReaction{},
		}
		msgResult[i] = meta
		msgMap[string(m.ID)] = meta // Dùng string cast thay vì hex encode cho tốc độ
	}

	// 2. Parallel fetch using errgroup
	g, gCtx := errgroup.WithContext(ctx)

	var atts []*model.Attachment
	g.Go(func() error {
		var err error
		atts, err = s.msgRepo.GetAttachmentsByMessageIDs(gCtx, msgIDs)
		return err
	})

	var reactions []*model.MessageReaction
	g.Go(func() error {
		var err error
		reactions, err = s.msgRepo.GetReactionsByMessageIDs(gCtx, msgIDs)
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, ae.Internal(err) // Trả lỗi ngay nếu DB gặp vấn đề
	}

	// 3. Mapping data (O(N) thay vì lặp lồng)
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

	// 4. Build Next Cursor
	nextCursor := ""
	if hasMore {
		last := rows[len(rows)-1]
		nextCursor = encodeMsgCursor(last.CreatedAt, last.Seq)
	}

	return &ResultPage[*model.MessageWithMeta]{
		Items:      msgResult,
		NextCursor: &nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *MessageService) MarkAsRead(ctx context.Context, convID, userID, lastReadMsgID []byte) error {
	if err := s.requireMembership(ctx, convID, userID); err != nil {
		return err
	}

	cursorTS, err := s.msgRepo.GetMessageCursorTS(ctx, lastReadMsgID, convID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ae.New(ae.ErrInvalidCursor, "Invalid cursor")
		}
		return ae.Internal(err)
	}

	_ = cache.ResetUnread(ctx, userID, convID)

	if err := s.roomRepo.UpdateLastReadAt(ctx, convID, userID, cursorTS); err != nil {
		return ae.Internal(err)
	}

	payload := map[string]interface{}{
		"event":            redis.EventReadMessage,
		"user_id":          hex.EncodeToString(userID),
		"last_read_msg_id": hex.EncodeToString(lastReadMsgID),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		g.Logger.Error("messageService.MarkAsRead: failed to marshal pubsub payload: %v", zap.Error(err))
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
	msg, err := s.msgRepo.GetMessageByID(ctx, msgID)
	if err != nil {
		return nil, ae.Internal(err)
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
	effected, er := s.msgRepo.UpdateMessageContent(ctx, msg)
	if er != nil {
		return nil, ae.Internal(er)
	}
	if effected == 0 {
		return nil, ae.New(ae.ErrCannotEditMessage, "Edit window expired (24h) or message not found")
	}
	msg.IsEdited = true

	payload := map[string]interface{}{
		"event":     redis.EventEditMessage,
		"msg_id":    hex.EncodeToString(msgID),
		"content":   newContent,
		"edited_at": time.Now().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		g.Logger.Error("messageService.EditMessage: failed to marshal pubsub payload: %v", zap.Error(err))
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
	if msg.IsDeleted {
		return ae.New(ae.ErrMessageDeleted, "Message has already been deleted")
	}
	if msg.Type == model.MessageTypeSystem {
		return ae.New(ae.ErrCannotDeleteMessage, "System messages cannot be deleted")
	}
	isSender := bytes.Equal(msg.SenderID, userID)
	if !isSender {
		role, err := s.roomRepo.GetMemberRole(ctx, msg.ConversationID, userID)
		if err != nil {
			return ae.Internal(err)
		}
		if role != model.RoleAdmin {
			return ae.Forbidden("User does not have permission to delete this message")
		}
	}

	if err := s.msgRepo.SoftDeleteMessage(ctx, msgID); err != nil {
		return ae.Internal(err)
	}

	_ = g.PubSub.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventDelMessage,
		ConvID:  hex.EncodeToString(msg.ConversationID),
		Payload: json.RawMessage(fmt.Sprintf(`{"event":"%s","msg_id":"%s"}`, redis.EventDelMessage, hex.EncodeToString(msgID))),
	})

	return nil
}

func (s *MessageService) ToggleReaction(ctx context.Context, userID, msgID []byte, emoji string) (string, error) {
	msg, err := s.msgRepo.GetMessageByID(ctx, msgID)
	if err != nil {
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

	_ = g.PubSub.Publish(ctx, msg.ConversationID, redis.Event{
		Type:    redis.EventToggleReaction,
		ConvID:  hex.EncodeToString(msg.ConversationID),
		Payload: json.RawMessage(fmt.Sprintf(`{"event":"%s","msg_id":"%s","user_id":"%s","emoji":"%s","action":"%s"}`, redis.EventToggleReaction, hex.EncodeToString(msgID), hex.EncodeToString(userID), emoji, action)),
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
