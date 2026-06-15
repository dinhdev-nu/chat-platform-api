package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/sqlc"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
)

type messageRepo struct {
	q  *sqlc.Queries
	db *sql.DB
}

func NewMessageRepository(db *sql.DB) r.MessageRepository {
	return &messageRepo{q: sqlc.New(db), db: db}
}

func (r *messageRepo) DeleteMessageReaction(ctx context.Context, msgID, userID []byte, emoji string) error {
	err := r.q.DeleteMessageReaction(ctx, sqlc.DeleteMessageReactionParams{
		MessageID: msgID,
		UserID:    userID,
		Emoji:     emoji,
	})
	if err != nil {
		return fmt.Errorf("messageRepo.DeleteMessageReaction: %w", err)
	}
	return nil
}

func (r *messageRepo) InsertMessageReaction(ctx context.Context, msgID, userID []byte, emoji string) (int64, error) {
	result, err := r.q.InsertMessageReaction(ctx, sqlc.InsertMessageReactionParams{
		MessageID: msgID,
		UserID:    userID,
		Emoji:     emoji,
	})
	if err != nil {
		return 0, fmt.Errorf("messageRepo.InsertMessageReaction: %w", err)
	}
	return result, nil
}

func (r *messageRepo) SoftDeleteMessage(ctx context.Context, id []byte) error {
	err := r.q.SoftDeleteMessage(ctx, id)
	if err != nil {
		return fmt.Errorf("messageRepo.SoftDeleteMessage: %w", err)
	}
	return nil
}

func (r *messageRepo) UpdateMessageContent(ctx context.Context, arg *model.Message) (int64, error) {
	result, err := r.q.UpdateMessageContent(ctx, sqlc.UpdateMessageContentParams{
		ID:       arg.ID,
		Content:  stringPtrToNullString(arg.Content),
		SenderID: arg.SenderID,
	})
	if err != nil {
		return 0, fmt.Errorf("messageRepo.UpdateMessageContent: %w", err)
	}
	return result, nil
}

func (r *messageRepo) GetMessageByID(ctx context.Context, id []byte) (*model.Message, error) {
	row, err := r.q.GetMessageByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("messageRepo.GetMessageByID: %w", err)
	}
	return row.ToDomain(), nil
}

func (r *messageRepo) GetUnreadCountByWatermark(ctx context.Context, userID, convID []byte) (int64, error) {
	count, err := r.q.GetUnreadCountByWatermark(ctx, sqlc.GetUnreadCountByWatermarkParams{
		UserID:         userID,
		ConversationID: convID,
		SenderID:       userID,
	})
	if err != nil {
		return 0, fmt.Errorf("messageRepo.GetUnreadCountByWatermark: %w", err)
	}
	return count, nil
}

func (r *messageRepo) GetMessageCursorTS(ctx context.Context, msgID, convID []byte) (*time.Time, error) {
	ts, err := r.q.GetMessageCursorTS(ctx, sqlc.GetMessageCursorTSParams{
		ID:             msgID,
		ConversationID: convID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("messageRepo.GetMessageCursorTS: %w", err)
	}
	return &ts, nil
}

func (r *messageRepo) GetAttachmentsByMessageIDs(ctx context.Context, msgIDs [][]byte) ([]*model.Attachment, error) {
	rows, err := r.q.GetAttachmentsByMessageIDs(ctx, msgIDs)
	if err != nil {
		return nil, fmt.Errorf("messageRepo.GetAttachmentsByMessageIDs: %w", err)
	}
	result := make([]*model.Attachment, len(rows))
	for i, r := range rows {
		result[i] = r.ToDomain()
	}
	return result, nil
}

func (r *messageRepo) GetReactionsByMessageIDs(ctx context.Context, msgIDs [][]byte) ([]*model.MessageReaction, error) {
	rows, err := r.q.GetReactionsByMessageIDs(ctx, msgIDs)
	if err != nil {
		return nil, fmt.Errorf("messageRepo.GetReactionsByMessageIDs: %w", err)
	}
	result := make([]*model.MessageReaction, len(rows))
	for i, r := range rows {
		result[i] = r.ToDomain()
	}
	return result, nil
}

func (r *messageRepo) ListMessages(ctx context.Context, convID []byte, cursorTS *time.Time, cursorSeq *uint64, limit int32) ([]*model.Message, error) {
	var rows []sqlc.Message
	var err error
	if cursorTS == nil {
		rows, err = r.q.ListMessagesFirstPage(ctx, sqlc.ListMessagesFirstPageParams{
			ConversationID: convID,
			Limit:          limit,
		})
		if err != nil {
			return nil, fmt.Errorf("messageRepo.ListMessages: %w", err)
		}
	} else {
		rows, err = r.q.ListMessagesNextPage(ctx, sqlc.ListMessagesNextPageParams{
			ConversationID: convID,
			CreatedAt:      *cursorTS,
			CreatedAt_2:    *cursorTS,
			Seq:            *cursorSeq,
			Limit:          limit,
		})
		if err != nil {
			return nil, fmt.Errorf("messageRepo.ListMessages: %w", err)
		}
	}
	result := make([]*model.Message, len(rows))
	for i, r := range rows {
		result[i] = r.ToDomain()
	}
	return result, nil
}

func (r *messageRepo) InsertAttachment(ctx context.Context, att *model.Attachment) error {
	params, err := attachmentParams(att)
	if err != nil {
		return fmt.Errorf("messageRepo.InsertAttachment: %w", err)
	}
	err = r.q.InsertAttachment(ctx, params)
	if err != nil {
		return fmt.Errorf("messageRepo.InsertAttachment: %w", err)
	}
	return nil
}

func (r *messageRepo) BatchInsertAttachments(ctx context.Context, args []*model.Attachment) error {
	sqlArgs := make([]sqlc.InsertAttachmentParams, len(args))
	for i, att := range args {
		params, err := attachmentParams(att)
		if err != nil {
			return fmt.Errorf("messageRepo.BatchInsertAttachments: attachment %d: %w", i, err)
		}
		sqlArgs[i] = params
	}
	return r.q.BatchInsertAttachments(ctx, sqlArgs)
}

func (r *messageRepo) InsertMessage(ctx context.Context, msg *model.Message) error {
	err := r.q.InsertMessage(ctx, sqlc.InsertMessageParams{
		ID:               msg.ID,
		ConversationID:   msg.ConversationID,
		SenderID:         msg.SenderID,
		ParentID:         ByteToNullString(msg.ParentID),
		Type:             int8(msg.Type),
		Content:          stringPtrToNullString(msg.Content),
		ContentEncrypted: msg.ContentEncrypted,
		Iv:               stringPtrToNullString(msg.Iv),
		Seq:              msg.Seq,
	})
	if err != nil {
		return fmt.Errorf("messageRepo.InsertMessage: %w", err)
	}
	return nil
}

func (r *messageRepo) InsertSystemMessage(ctx context.Context, msg *model.Message) error {
	err := r.q.InsertSystemMessage(ctx, sqlc.InsertSystemMessageParams{
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		SenderID:       msg.SenderID,
		Content:        stringPtrToNullString(msg.Content),
		Seq:            msg.Seq,
	})
	if err != nil {
		return fmt.Errorf("messageRepo.InsertSystemMessage: %w", err)
	}
	return nil
}

func (r *messageRepo) GetMaxSeq(ctx context.Context, convID []byte) (int64, error) {
	seq, err := r.q.GetMaxSeq(ctx, convID)
	if err != nil {
		return 0, fmt.Errorf("messageRepo.GetMaxSeq: %w", err)
	}
	return seq, nil
}

func stringPtrToNullString(s *string) sql.NullString {
	if s == nil || *s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

func attachmentParams(att *model.Attachment) (sqlc.InsertAttachmentParams, error) {
	width, err := mediumIntParam("width", att.Width)
	if err != nil {
		return sqlc.InsertAttachmentParams{}, err
	}
	height, err := mediumIntParam("height", att.Height)
	if err != nil {
		return sqlc.InsertAttachmentParams{}, err
	}
	duration, err := smallIntParam("duration_sec", att.DurationSec)
	if err != nil {
		return sqlc.InsertAttachmentParams{}, err
	}

	return sqlc.InsertAttachmentParams{
		ID:            att.ID,
		MessageID:     att.MessageID,
		FileName:      att.Filename,
		FileUrl:       att.FileURL,
		MimeType:      att.MimeType,
		FileSizeBytes: att.FileSizeBytes,
		Width:         width,
		Height:        height,
		DurationSec:   duration,
	}, nil
}

func mediumIntParam(name string, value *int) (sql.NullInt32, error) {
	const maxMediumIntUnsigned = 1<<24 - 1

	if value == nil || *value == 0 {
		return sql.NullInt32{}, nil
	}
	if *value < 0 || *value > maxMediumIntUnsigned {
		return sql.NullInt32{}, fmt.Errorf("%s must be between 1 and %d", name, maxMediumIntUnsigned)
	}
	return sql.NullInt32{Int32: int32(*value), Valid: true}, nil
}

func smallIntParam(name string, value *int) (sql.NullInt16, error) {
	const maxSmallInt = 1<<15 - 1

	if value == nil || *value == 0 {
		return sql.NullInt16{}, nil
	}
	if *value < 0 || *value > maxSmallInt {
		return sql.NullInt16{}, fmt.Errorf("%s must be between 1 and %d", name, maxSmallInt)
	}
	return sql.NullInt16{Int16: int16(*value), Valid: true}, nil
}
