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

func (r *roomRepo) GetUserConversationIDs(ctx context.Context, userID []byte) ([][]byte, error) {
	rows, err := r.q.GetUserConversationIDs(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("roomRepo.GetUserConversationIDs: %w", err)
	}
	return rows, nil
}

func (r *roomRepo) UpdateLastReadAt(ctx context.Context, convID, userID []byte, cursorTS *time.Time) error {
	err := r.q.UpdateLastReadAt(ctx, sqlc.UpdateLastReadAtParams{
		ConversationID: convID,
		UserID:         userID,
		LastReadAt:     cursorTS,
	})
	if err != nil {
		return fmt.Errorf("roomRepo.UpdateLastReadAt: %w", err)
	}
	return nil
}

func (r *roomRepo) UpdateConversationLastActivity(ctx context.Context, convID, lastMsgID []byte) error {
	err := r.q.UpdateConversationLastActivity(ctx, sqlc.UpdateConversationLastActivityParams{
		LastMessageID: ByteToNullString(lastMsgID),
		ID:            convID,
	})
	if err != nil {
		return fmt.Errorf("roomRepo.UpdateConversationLastActivity: %w", err)
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

	if cursorTS != nil {
		rows, err := r.q.ListConversationsFirstPage(ctx, sqlc.ListConversationsFirstPageParams{
			UserID: userID,
			Limit:  limit,
		})
		if err != nil {
			return nil, fmt.Errorf("roomRepo.ListConversations: %w", err)
		}
		for _, row := range rows {
			c, err := appendConv(row.ID, row.Type, row.Name, row.AvatarUrl, row.LastMessageID, row.LastActivityAt, row.CreatedAt, row.UpdatedAt, row.Role, row.IsMuted)
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
			Limit:            limit,
		})
		if err != nil {
			return nil, fmt.Errorf("roomRepo.ListConversations: %w", err)
		}
		for _, row := range rows {
			c, err := appendConv(row.ID, row.Type, row.Name, row.AvatarUrl, row.LastMessageID, row.LastActivityAt, row.CreatedAt, row.UpdatedAt, row.Role, row.IsMuted)
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
		ID:        conv.ID,
		Type:      int8(conv.Type),
		CreatedBy: ByteToNullString(conv.CreatedBy),
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

func appendConv(id []byte, cType int8, name, avatar sql.NullString, lastMsgID sql.NullString, lastAct *time.Time, created, updated time.Time, role int8, isMuted bool) (*model.ConversationListRow, error) {
	msgID, err := nullStringToByte(lastMsgID)
	if err != nil {
		return nil, err
	}
	return &model.ConversationListRow{

		Conversation: model.Conversation{
			ID:             id,
			Type:           model.ConversationType(cType),
			Name:           nullStringToStringPointer(name),
			AvatarURL:      nullStringToStringPointer(avatar),
			LastMessageID:  msgID,
			LastActivityAt: lastAct,
			CreatedAt:      created,
			UpdatedAt:      updated,
		},
		Role:        model.MemberRole(role),
		IsMuted:     isMuted,
		UnreadCount: 0, // Initialize unread count
	}, nil
}

func ByteToNullString(val []byte) sql.NullString {
	return sql.NullString{
		String: string(val),
		Valid:  len(val) > 0,
	}
}

func nullStringToByte(ns sql.NullString) ([]byte, error) {
	if !ns.Valid {
		return nil, nil
	}
	return []byte(ns.String), nil
}

func nullStringToStringPointer(ns sql.NullString) *string {
	if ns.Valid {
		return &ns.String
	}
	return nil
}
