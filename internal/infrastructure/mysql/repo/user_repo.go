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
)

type userRepo struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) r.UserRepository {
	return &userRepo{db: db}
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

func (r *userRepo) Update(ctx context.Context, user *model.User, userID []byte) error {
	g := gormmodel.UserFormDomain(user)
	if err := r.db.WithContext(ctx).
		Model(&gormmodel.User{}).Where("id = ?", userID).Updates(&g).Error; err != nil {
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
