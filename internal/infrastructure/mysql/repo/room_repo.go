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

type roomRepo struct {
	q  *sqlc.Queries
	db *sql.DB
}

func NewRoomRepository(db *sql.DB) r.RoomRepository {
	return &roomRepo{
		q:  sqlc.New(db),
		db: db,
	}
}

// The row lock is held through commit and is released automatically on rollback.
// Cache fills use the same lock as membership writes, including across API nodes.
func (r *roomRepo) WithMembershipLock(ctx context.Context, convID []byte, apply func(r.ConversationMembers) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("membership transaction: %w", err)
	}
	defer tx.Rollback()
	queries := r.q.WithTx(tx)
	if _, err := queries.LockConversationMembership(ctx, convID); err != nil {
		return fmt.Errorf("lock conversation membership: %w", err)
	}
	if err := apply(&roomRepo{q: queries}); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *roomRepo) GetUserConversationIDs(ctx context.Context, userID []byte) ([][]byte, error) {
	rows, err := r.q.GetUserConversationIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("roomRepo.GetUserConversationIDs: %w", err)
	}
	return rows, nil
}

func (r *roomRepo) AdvanceReadWatermark(ctx context.Context, convID, userID []byte, cursor model.MessageCursor) (bool, error) {
	affected, err := r.q.AdvanceReadWatermark(ctx, sqlc.AdvanceReadWatermarkParams{
		ConversationID: convID,
		UserID:         userID,
		ReadAt:         &cursor.CreatedAt,
		ReadSeq:        cursor.Seq,
	})
	if err != nil {
		return false, fmt.Errorf("roomRepo.AdvanceReadWatermark: %w", err)
	}
	return affected > 0, nil
}

// Legacy job text/timestamps are intentionally ignored; the stored message is authoritative.
func (r *roomRepo) UpdateConversationLastActivity(ctx context.Context, convID, lastMsgID []byte, _ *string, _ time.Time) error {
	err := r.q.UpdateConversationLastActivity(ctx, sqlc.UpdateConversationLastActivityParams{
		MessageID:      lastMsgID,
		ConversationID: convID,
	})
	if err != nil {
		return fmt.Errorf("roomRepo.UpdateConversationLastActivity: %w", err)
	}
	return nil
}

func (r *roomRepo) RefreshConversationLastMessage(ctx context.Context, convID, msgID []byte) error {
	if err := r.q.RefreshConversationLastMessage(ctx, sqlc.RefreshConversationLastMessageParams{
		ConversationID: convID,
		MessageID:      ByteToNullString(msgID),
	}); err != nil {
		return fmt.Errorf("roomRepo.RefreshConversationLastMessage: %w", err)
	}
	return nil
}

func (r *roomRepo) GetConversationMemberIDs(ctx context.Context, convID []byte) ([][]byte, error) {
	rows, err := r.q.GetConversationMemberIDs(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("roomRepo.GetConversationMemberIDs: %w", err)
	}
	return rows, nil
}

func (r *roomRepo) GetConversationMember(ctx context.Context, convID, userID []byte) (*model.ConversationMember, error) {
	memb, err := r.q.GetConversationMember(ctx, sqlc.GetConversationMemberParams{
		ConversationID: convID,
		UserID:         userID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("roomRepo.GetConversationMember: %w", err)
	}
	return memb.ToDomain(), nil
}

func (r *roomRepo) DeleteConversationMember(ctx context.Context, convID, userID []byte) error {
	err := r.q.DeleteConversationMember(ctx, sqlc.DeleteConversationMemberParams{
		ConversationID: convID,
		UserID:         userID,
	})
	if err != nil {
		return fmt.Errorf("roomRepo.DeleteConversationMember: %w", err)
	}
	return nil
}

func (r *roomRepo) GetMemberRole(ctx context.Context, convID, userID []byte) (model.MemberRole, error) {
	role, err := r.q.GetMemberRole(ctx, sqlc.GetMemberRoleParams{
		ConversationID: convID,
		UserID:         userID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("roomRepo.GetMemberRole: %w", err)
	}
	return model.MemberRole(role), nil
}

func (r *roomRepo) ListConversations(ctx context.Context, userID []byte, cursorTS *time.Time, cursorID []byte, limit int32) ([]*model.ConversationListRow, error) {
	var convs []*model.ConversationListRow

	if cursorTS == nil {
		rows, err := r.q.ListConversationsFirstPage(ctx, sqlc.ListConversationsFirstPageParams{
			UserID: userID,
			Limit:  limit,
		})
		if err != nil {
			return nil, fmt.Errorf("roomRepo.ListConversations: %w", err)
		}
		for _, row := range rows {
			c, err := mapConversationRow(conversationRow{
				ID: row.ID, Type: row.Type,
				Name: row.Name, AvatarURL: row.AvatarUrl,
				LastMessageID: row.LastMessageID, LastMessageText: row.LastMessageText,
				LastActivityAt: row.LastActivityAt,
				CreatedAt:      row.CreatedAt, UpdatedAt: row.UpdatedAt,
				Role: row.Role, IsMuted: row.IsMuted,
			})
			if err != nil {
				return nil, fmt.Errorf("roomRepo.ListConversations: %w", err)
			}
			convs = append(convs, c)
		}
	} else {
		rows, err := r.q.ListConversationsNextPage(ctx, sqlc.ListConversationsNextPageParams{
			UserID:           userID,
			LastActivityAt:   cursorTS,
			LastActivityAt_2: cursorTS,
			ID:               cursorID,
			Limit:            limit,
		})
		if err != nil {
			return nil, fmt.Errorf("roomRepo.ListConversations: %w", err)
		}
		for _, row := range rows {
			c, err := mapConversationRow(conversationRow{
				ID: row.ID, Type: row.Type,
				Name: row.Name, AvatarURL: row.AvatarUrl,
				LastMessageID: row.LastMessageID, LastMessageText: row.LastMessageText,
				LastActivityAt: row.LastActivityAt,
				CreatedAt:      row.CreatedAt, UpdatedAt: row.UpdatedAt,
				Role: row.Role, IsMuted: row.IsMuted,
			})
			if err != nil {
				return nil, fmt.Errorf("roomRepo.ListConversations: %w", err)
			}
			convs = append(convs, c)
		}
	}

	return convs, nil
}

func (r *roomRepo) CreateConversation(ctx context.Context, conv *model.Conversation) error {
	err := r.q.CreateConversation(ctx, sqlc.CreateConversationParams{
		ID:          conv.ID,
		Type:        int8(conv.Type),
		Name:        StringPtrToNullString(conv.Name),
		AvatarUrl:   StringPtrToNullString(conv.AvatarURL),
		Description: StringPtrToNullString(conv.Description),
		CreatedBy:   ByteToNullString(conv.CreatedBy),
	})
	if err != nil {
		return fmt.Errorf("roomRepo.CreateConversation: %w", err)
	}
	return nil
}

func (r *roomRepo) GetConversationByID(ctx context.Context, id []byte) (*model.Conversation, error) {
	c, err := r.q.GetConversationByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("roomRepo.GetConversationByID: %w", err)
	}
	return c.ToDomain(), nil
}

func (r *roomRepo) GetDMConversation(ctx context.Context, userID1, userID2 []byte) ([]byte, error) {
	id, err := r.q.GetDMConversation(ctx, sqlc.GetDMConversationParams{
		UserID:   userID1,
		UserID_2: userID2,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("roomRepo.GetDMConversation: %w", err)
	}
	return id, err
}

func (r *roomRepo) InsertConversationMember(ctx context.Context, memb *model.ConversationMember) error {
	err := r.q.InsertConversationMember(ctx, sqlc.InsertConversationMemberParams{
		ConversationID: memb.ConversationID,
		UserID:         memb.UserID,
		Role:           int8(memb.Role),
	})
	if err != nil {
		return fmt.Errorf("roomRepo.InsertConversationMember: %w", err)
	}
	return nil
}

func (r *roomRepo) BatchInsertConversationMembers(ctx context.Context, membs []*model.ConversationMember) error {
	ms := make([]sqlc.InsertConversationMemberParams, len(membs))
	for i, m := range membs {
		ms[i] = sqlc.InsertConversationMemberParams{
			ConversationID: m.ConversationID,
			UserID:         m.UserID,
			Role:           int8(m.Role),
		}
	}
	err := r.q.BatchInsertConversationMembers(ctx, ms)
	if err != nil {
		return fmt.Errorf("roomRepo.BatchInsertConversationMembers: %w", err)
	}
	return nil
}

// conversationRow is the mapping boundary for sqlc's first and next page rows.
type conversationRow struct {
	ID              []byte
	Type            int8
	Name            any
	AvatarURL       any
	LastMessageID   sql.NullString
	LastMessageText sql.NullString
	LastActivityAt  *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Role            int8
	IsMuted         bool
}

func mapConversationRow(row conversationRow) (*model.ConversationListRow, error) {
	namePtr, err := nullableStringValueToPointer(row.Name)
	if err != nil {
		return nil, err
	}
	avatarPtr, err := nullableStringValueToPointer(row.AvatarURL)
	if err != nil {
		return nil, err
	}

	return &model.ConversationListRow{

		Conversation: model.Conversation{
			ID:              row.ID,
			Type:            model.ConversationType(row.Type),
			Name:            namePtr,
			AvatarURL:       avatarPtr,
			LastMessageID:   nullStringToByte(row.LastMessageID),
			LastMessageText: nullStringToStringPointer(row.LastMessageText),
			LastActivityAt:  row.LastActivityAt,
			CreatedAt:       row.CreatedAt,
			UpdatedAt:       row.UpdatedAt,
		},
		Role:        model.MemberRole(row.Role),
		IsMuted:     row.IsMuted,
		UnreadCount: 0, // Initialize unread count
	}, nil
}

func ByteToNullString(val []byte) sql.NullString {
	return sql.NullString{
		String: string(val),
		Valid:  len(val) > 0,
	}
}

func StringPtrToNullString(val *string) sql.NullString {
	if val == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{
		String: *val,
		Valid:  true,
	}
}

func nullStringToByte(ns sql.NullString) []byte {
	if !ns.Valid {
		return nil
	}
	return []byte(ns.String)
}

func nullStringToStringPointer(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}

func nullableStringValueToPointer(val any) (*string, error) {
	switch v := val.(type) {
	case nil:
		return nil, nil
	case string:
		return &v, nil
	case []byte:
		s := string(v)
		return &s, nil
	case sql.NullString:
		return nullStringToStringPointer(v), nil
	default:
		return nil, fmt.Errorf("unsupported nullable string value type %T", val)
	}
}
