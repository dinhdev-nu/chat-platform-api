package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	gormmodel "github.com/dinhdev-nu/chat-platform-api/internal/infrastructure/mysql/gorm/model"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type userTokenRepo struct{ db *gorm.DB }

func NewUserTokenRepository(db *gorm.DB) r.UserTokenRepository {
	return &userTokenRepo{db: db}
}

func (r *userTokenRepo) GetJTIByUserAndDevice(ctx context.Context, userID, deviceID []byte) ([]byte, error) {
	var g gormmodel.UserToken
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND device_id = ?", userID, deviceID).
		First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("userTokenRepo.GetJTIByUserAndDevice: %w", err)
	}
	return g.JTI, nil
}

func (r *userTokenRepo) Upsert(ctx context.Context, token *model.UserToken) error {
	g := gormmodel.UserTokenFromDomain(token)
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "device_id"}},
			DoUpdates: clause.AssignmentColumns(
				[]string{"jti", "device_name", "ip_address", "expires_at", "last_used_at"},
			),
		}).
		Create(g).Error

	if err != nil {
		return fmt.Errorf("userTokenRepo.Upsert: %w", err)
	}
	token.ID = g.ID
	return nil
}

func (r *userTokenRepo) CountByUserID(ctx context.Context, userID []byte) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&gormmodel.UserToken{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("userTokenRepo.CountByUserID: %w", err)
	}
	return count, nil
}

func (r *userTokenRepo) GetOldestJTIByUserIDBeyondLimit(ctx context.Context, userID []byte, limit int) ([][]byte, error) {
	var jtis [][]byte

	err := r.db.WithContext(ctx).
		Model(&gormmodel.UserToken{}).
		Select("jti").
		Where("user_id = ?", userID).
		Order("last_used_at DESC").
		Offset(limit).
		Find("jti", &jtis).Error

	if err != nil {
		return nil, fmt.Errorf("userTokenRepo.GetOldestJTIByUserIDBeyondLimit: %w", err)
	}

	return jtis, nil
}

func (r *userTokenRepo) DeleteOldestBeyondLimit(ctx context.Context, userID []byte, limit int) error {
	query := `
		DELETE FROM user_tokens
		WHERE user_id = ? 
		AND id NOT IN (
			SELECT id FROM (
				SELECT id FROM user_tokens
				WHERE user_id = ?
				ORDER BY last_used_at DESC
				LIMIT ?
			) t
		)
	`
	err := r.db.WithContext(ctx).Exec(query, userID, userID, limit).Error
	if err != nil {
		return fmt.Errorf("userTokenRepo.DeleteOldestBeyondLimit: %w", err)
	}
	return nil
}

func (r *userTokenRepo) DeleteByJTI(ctx context.Context, jti []byte) error {
	err := r.db.WithContext(ctx).
		Where("jti = ?", jti).
		Delete(&gormmodel.UserToken{}).Error
	if err != nil {
		return fmt.Errorf("userTokenRepo.DeleteByJTI: %w", err)
	}
	return nil
}

func (r *userTokenRepo) FindByJTI(ctx context.Context, jti []byte) (*model.UserToken, error) {
	var g gormmodel.UserToken
	err := r.db.WithContext(ctx).
		Where("jti = ?", jti).First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("userTokenRepo.FindByJTI: %w", err)
	}
	return g.ToDomain(), nil
}

func (r *userTokenRepo) UpdateLastUsed(ctx context.Context, jti []byte) error {
	err := r.db.WithContext(ctx).
		Model(&gormmodel.UserToken{}).
		Where("jti = ?", jti).
		Update("last_used_at", time.Now()).Error
	if err != nil {
		return fmt.Errorf("userTokenRepo.UpdateLastUsed: %w", err)
	}
	return nil
}
