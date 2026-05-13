package repository

import (
	"context"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

type UserRepository interface {
	FindByID(ctx context.Context, id []byte) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, user *model.User, userID []byte) error
	UpdateLastSeenAt(ctx context.Context, userID []byte) error
}

type UserTokenRepository interface {
	FindByJTI(ctx context.Context, jti []byte) (*model.UserToken, error)
	GetJTIByUserAndDevice(ctx context.Context, userID, deviceID []byte) ([]byte, error)
	Upsert(ctx context.Context, token *model.UserToken) error
	CountByUserID(ctx context.Context, userID []byte) (int64, error)
	GetOldestJTIByUserIDBeyondLimit(ctx context.Context, userID []byte, limit int) ([][]byte, error)
	DeleteOldestBeyondLimit(ctx context.Context, userID []byte, limit int) error
	DeleteByJTI(ctx context.Context, jti []byte) error
	UpdateLastUsed(ctx context.Context, jti []byte) error
}
