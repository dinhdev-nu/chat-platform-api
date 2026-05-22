package repo

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	gormmodel "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/gorm/model"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"gorm.io/gorm"
)

type userRepo struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) r.UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) GetIncomingRequests(ctx context.Context, userID []byte, cursor *string, limit int) ([]*model.SearchUser, error) {
	if cursor == nil || *cursor == "" {
		rows, err := ListIncomingFirstPage(ctx, r.db, ListIncomingFirstPageParams{
			CurrentUserID: userID,
			Limit:         limit,
		})
		if err != nil {
			return nil, fmt.Errorf("userRepo.GetIncomingRequests: %w", err)
		}
		var requests []*model.SearchUser
		for _, row := range rows {
			requests = append(requests, row.ToDomain())
		}
		return requests, nil
	}
	rows, err := ListIncomingNextPage(ctx, r.db, ListIncomingNextPageParams{
		CurrentUserID: userID,
		Cursor:        *cursor,
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("userRepo.GetIncomingRequests: %w", err)
	}
	var requests []*model.SearchUser
	for _, row := range rows {
		requests = append(requests, row.ToDomain())
	}
	return requests, nil
}

func (r *userRepo) GetAcceptedContacts(ctx context.Context, userID []byte, cursor *string, limit int) ([]*model.SearchUser, error) {
	if cursor == nil || *cursor == "" {
		rows, err := ListFriendsFirstPage(ctx, r.db, ListFriendsFirstPageParams{
			CurrentUserID: userID,
			Limit:         limit,
		})
		if err != nil {
			return nil, fmt.Errorf("userRepo.GetAcceptedContacts: %w", err)
		}
		var contacts []*model.SearchUser
		for _, row := range rows {
			contacts = append(contacts, row.ToDomain())
		}
		return contacts, nil
	}
	rows, err := ListFriendsNextPage(ctx, r.db, ListFriendsNextPageParams{
		CurrentUserID: userID,
		Cursor:        *cursor,
		Limit:         limit,
	})
	if err != nil {
		return nil, fmt.Errorf("userRepo.GetAcceptedContacts: %w", err)
	}
	var contacts []*model.SearchUser
	for _, row := range rows {
		contacts = append(contacts, row.ToDomain())
	}
	return contacts, nil
}

func (r *userRepo) GetContactRecord(ctx context.Context, userID, contactID []byte) (*model.UserContact, error) {
	var contact *gormmodel.UserContact

	err := r.db.WithContext(ctx).
		Where("user_id = ? AND contact_id = ?", userID, contactID).
		First(&contact).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &model.UserContact{
		ID:        contact.ID,
		UserID:    contact.UserID,
		ContactID: contact.ContactID,
		Status:    model.ContactStatus(contact.Status),
		UpdatedAt: contact.UpdatedAt,
		CreatedAt: contact.CreatedAt,
	}, nil
}

func (r *userRepo) CreateContactRequest(ctx context.Context, userID, contactID []byte) error {
	contact := &gormmodel.UserContact{
		UserID:    userID,
		ContactID: contactID,
		Status:    gormmodel.ContactStatusPending,
	}
	if err := r.db.WithContext(ctx).Create(contact).Error; err != nil {
		return fmt.Errorf("userRepo.CreateContactRequest: %w", err)
	}
	return nil
}

func (r *userRepo) UpdateContactStatus(ctx context.Context, contactID uint64, status model.ContactStatus) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&gormmodel.UserContact{}).
		Where("id = ?", contactID).
		Update("status", status)

	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

func (r *userRepo) GetContactPair(ctx context.Context, uid1, uid2 []byte) ([]*model.UserContact, error) {
	var contacts []*gormmodel.UserContact

	err := r.db.WithContext(ctx).
		Select("id, user_id, contact_id, status").
		Where("(user_id = ? AND contact_id = ?) OR (user_id = ? AND contact_id = ?)",
			uid1, uid2,
			uid2, uid1,
		).
		Limit(2). // tối đa 2 row — không cần full scan
		Find(&contacts).Error

	if err != nil {
		return nil, fmt.Errorf("userRepo.GetContactPair: %w", err)
	}

	var result []*model.UserContact
	for _, c := range contacts {
		result = append(result, &model.UserContact{
			ID:        c.ID,
			UserID:    c.UserID,
			ContactID: c.ContactID,
			Status:    model.ContactStatus(c.Status),
		})
	}
	return result, nil
}

func (r *userRepo) CheckUserExists(ctx context.Context, id []byte) (bool, error) {
	var exists bool
	err := r.db.WithContext(ctx).
		Model(&gormmodel.User{}).
		Select("1").
		Where("id = ?", id).
		Limit(1).
		Find(&exists). // Nếu tìm thấy, exists sẽ là true
		Error

	if err != nil {
		return false, fmt.Errorf("userRepo.CheckUserExists: %w", err)
	}

	return exists, nil
}

func (r *userRepo) SearchUsers(ctx context.Context, curUID []byte, q string, cursor *string, limit int) ([]*model.SearchUser, error) {
	if cursor == nil || *cursor == "" {
		rows, err := r.SearchUsersFirstPage(ctx, SearchUsersFirstPageParams{
			CurrentUserID:  curUID,
			UsernamePrefix: q,
			Limit:          limit,
		})
		if err != nil {
			return nil, fmt.Errorf("userRepo.SearchUsers: %w", err)
		}
		var searchUsers []*model.SearchUser
		for _, row := range rows {
			searchUsers = append(searchUsers, row.ToDomain())
		}
		return searchUsers, nil
	}
	rows, err := r.SearchUsersNextPage(ctx, SearchUsersNextPageParams{
		CurrentUserID:  curUID,
		UsernamePrefix: q,
		Cursor:         *cursor,
		Limit:          limit,
	})
	if err != nil {
		return nil, fmt.Errorf("userRepo.SearchUsers: %w", err)
	}
	var searchUsers []*model.SearchUser
	for _, row := range rows {
		searchUsers = append(searchUsers, row.ToDomain())
	}
	return searchUsers, nil
}

func (r *userRepo) FindByID(ctx context.Context, id []byte) (*model.User, error) {
	var g gormmodel.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("userRepo.FindByID: %w", err)
	}
	return g.ToDomain(), nil
}

func (r *userRepo) FindActiveIDs(ctx context.Context, ids [][]byte) (map[string]bool, error) {
	found := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return found, nil
	}

	var rows []struct {
		ID []byte `gorm:"column:id"`
	}
	if err := r.db.WithContext(ctx).
		Model(&gormmodel.User{}).
		Select("id").
		Where("id IN ? AND status = ?", ids, gormmodel.UserStatusActive).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("userRepo.FindActiveIDs: %w", err)
	}
	for _, row := range rows {
		found[string(row.ID)] = true
	}
	return found, nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	var g gormmodel.User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("userRepo.FindByEmail: %w", err)
	}
	return g.ToDomain(), nil
}

func (r *userRepo) Create(ctx context.Context, user *model.User) error {
	g := gormmodel.UserFormDomain(user)
	if err := r.db.WithContext(ctx).Create(&g).Error; err != nil {
		return fmt.Errorf("userRepo.Create: %w", err)
	}
	return nil
}

func (r *userRepo) Update(ctx context.Context, userID []byte, update *model.UserProfileUpdate) error {
	if update == nil {
		return nil
	}
	updates := map[string]any{}
	if update.HasName {
		if update.Name == nil {
			updates["username"] = nil
		} else {
			updates["username"] = *update.Name
		}
	}
	if update.HasAvatar {
		if update.AvatarURL == nil {
			updates["avatar_url"] = nil
		} else {
			updates["avatar_url"] = *update.AvatarURL
		}
	}
	if update.HasBio {
		if update.Bio == nil {
			updates["bio"] = nil
		} else {
			updates["bio"] = *update.Bio
		}
	}
	if len(updates) == 0 {
		return nil
	}

	if err := r.db.WithContext(ctx).
		Model(&gormmodel.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return fmt.Errorf("userRepo.Update: %w", err)
	}
	return nil
}

func (r *userRepo) UpdateLastSeenAt(ctx context.Context, userID []byte) error {
	if err := r.db.WithContext(ctx).
		Model(&gormmodel.User{}).Where("id = ?", userID).Update("last_seen_at", time.Now()).Error; err != nil {
		return fmt.Errorf("userRepo.UpdateLastSeenAt: %w", err)
	}
	return nil
}

type SearchUserRow struct {
	ID             []byte                   `gorm:"column:id"`
	Username       string                   `gorm:"column:username"`
	AvatarURL      *string                  `gorm:"column:avatar_url"`
	Bio            *string                  `gorm:"column:bio"`
	LastSeenAt     *time.Time               `gorm:"column:last_seen_at"`
	OutgoingStatus *gormmodel.ContactStatus `gorm:"column:outgoing_status"`
	IncomingStatus *gormmodel.ContactStatus `gorm:"column:incoming_status"`
}

func (s *SearchUserRow) ToDomain() *model.SearchUser {
	return &model.SearchUser{
		ID:             hex.EncodeToString(s.ID),
		Username:       s.Username,
		AvatarURL:      s.AvatarURL,
		Bio:            s.Bio,
		LastSeenAt:     s.LastSeenAt,
		OutgoingStatus: (*model.ContactStatus)(s.OutgoingStatus),
		IncomingStatus: (*model.ContactStatus)(s.IncomingStatus),
	}
}

type SearchUsersFirstPageParams struct {
	CurrentUserID  []byte
	UsernamePrefix string
	Limit          int
}

type SearchUsersNextPageParams struct {
	CurrentUserID  []byte
	UsernamePrefix string
	Cursor         string // username cuối trang trước
	Limit          int
}

func (r *userRepo) SearchUsersNextPage(
	ctx context.Context,
	params SearchUsersNextPageParams,
) ([]*SearchUserRow, error) {
	var rows []*SearchUserRow

	err := r.searchUsersBaseQuery(ctx, params.CurrentUserID, params.UsernamePrefix).
		Where("u.username > ?", params.Cursor). // ← điểm khác duy nhất
		Limit(params.Limit).
		Scan(&rows).Error

	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *userRepo) SearchUsersFirstPage(
	ctx context.Context,
	params SearchUsersFirstPageParams,
) ([]*SearchUserRow, error) {
	var rows []*SearchUserRow

	err := r.searchUsersBaseQuery(ctx, params.CurrentUserID, params.UsernamePrefix).
		Limit(params.Limit).
		Scan(&rows).Error

	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *userRepo) searchUsersBaseQuery(ctx context.Context, currentUserID []byte, usernamePrefix string) *gorm.DB {
	likePattern := usernamePrefix + "%"

	return r.db.WithContext(ctx).Table("users AS u").
		Select(`
			u.id,
			u.username,
			u.avatar_url,
			u.bio,
			u.last_seen_at,
			c_out.status AS outgoing_status,
			c_in.status  AS incoming_status`,
		).
		Joins(
			"LEFT JOIN user_contacts AS c_out ON c_out.user_id = ? AND c_out.contact_id = u.id",
			currentUserID,
		).
		Joins(
			"LEFT JOIN user_contacts AS c_in ON c_in.contact_id = ? AND c_in.user_id = u.id",
			currentUserID,
		).
		Where("u.status = ?", gormmodel.UserStatusActive).
		Where("u.id != ?", currentUserID).
		Where("u.username LIKE ?", likePattern).
		Where("c_in.status IS NULL OR c_in.status != ?", gormmodel.ContactStatusBlocked).
		Order("u.username ASC")
}

// ─────────────────────────────────────────────
// Params
// ─────────────────────────────────────────────

type ListFriendsFirstPageParams struct {
	CurrentUserID []byte
	Limit         int
}

type ListFriendsNextPageParams struct {
	CurrentUserID []byte
	Cursor        string // username cuối trang trước
	Limit         int
}

// ─────────────────────────────────────────────
// Base builder
// ─────────────────────────────────────────────

func listFriendsBaseQuery(db *gorm.DB, currentUserID []byte) *gorm.DB {
	return db.Table("users AS u").
		Select("u.id, u.username, u.avatar_url, u.bio, u.last_seen_at").
		// INNER JOIN filter trực tiếp — không cần LEFT JOIN c_out / c_in
		Joins(`
			INNER JOIN user_contacts AS c_link
				ON c_link.status = ?
				AND (
					(c_link.user_id    = ? AND c_link.contact_id = u.id)
				 OR (c_link.contact_id = ? AND c_link.user_id    = u.id)
				)`,
			model.ContactStatusAccepted,
			currentUserID,
			currentUserID,
		).
		Where("u.status = ?", model.UserStatusActive).
		Order("u.username ASC")
}

// ─────────────────────────────────────────────
// Query functions
// ─────────────────────────────────────────────

func ListFriendsFirstPage(
	ctx context.Context,
	db *gorm.DB,
	params ListFriendsFirstPageParams,
) ([]*SearchUserRow, error) {
	var rows []*SearchUserRow

	err := listFriendsBaseQuery(db.WithContext(ctx), params.CurrentUserID).
		Limit(params.Limit).
		Scan(&rows).Error

	if err != nil {
		return nil, err
	}
	return rows, nil
}

func ListFriendsNextPage(
	ctx context.Context,
	db *gorm.DB,
	params ListFriendsNextPageParams,
) ([]*SearchUserRow, error) {
	var rows []*SearchUserRow

	err := listFriendsBaseQuery(db.WithContext(ctx), params.CurrentUserID).
		Where("u.username > ?", params.Cursor).
		Limit(params.Limit).
		Scan(&rows).Error

	if err != nil {
		return nil, err
	}
	return rows, nil
}

type ListIncomingFirstPageParams struct {
	CurrentUserID []byte
	Limit         int
}

type ListIncomingNextPageParams struct {
	CurrentUserID []byte
	Cursor        string
	Limit         int
}

func listIncomingBaseQuery(db *gorm.DB, currentUserID []byte) *gorm.DB {
	return db.Table("users AS u").
		Select("u.id, u.username, u.avatar_url, u.bio, u.last_seen_at").
		Joins(`
			INNER JOIN user_contacts AS c
				ON c.user_id    = u.id
				AND c.contact_id = ?
				AND c.status     = ?`,
			currentUserID,
			model.ContactStatusPending,
		).
		Where("u.status = ?", model.UserStatusActive).
		Order("u.username ASC")
}

func ListIncomingFirstPage(
	ctx context.Context,
	db *gorm.DB,
	params ListIncomingFirstPageParams,
) ([]*SearchUserRow, error) {
	var rows []*SearchUserRow

	err := listIncomingBaseQuery(db.WithContext(ctx), params.CurrentUserID).
		Limit(params.Limit).
		Scan(&rows).Error

	if err != nil {
		return nil, err
	}
	return rows, nil
}

func ListIncomingNextPage(
	ctx context.Context,
	db *gorm.DB,
	params ListIncomingNextPageParams,
) ([]*SearchUserRow, error) {
	var rows []*SearchUserRow

	err := listIncomingBaseQuery(db.WithContext(ctx), params.CurrentUserID).
		Where("u.username > ?", params.Cursor).
		Limit(params.Limit).
		Scan(&rows).Error

	if err != nil {
		return nil, err
	}
	return rows, nil
}
