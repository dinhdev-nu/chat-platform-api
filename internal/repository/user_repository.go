package repository

import (
	"context"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
)

type UserRepository interface {
	FindByID(ctx context.Context, id []byte) (*model.User, error)
	FindByIDs(ctx context.Context, ids [][]byte) (map[string]*model.User, error)
	FindActiveIDs(ctx context.Context, ids [][]byte) (map[string]bool, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, userID []byte, update *model.UserProfileUpdate) error
	UpdateLastSeenAt(ctx context.Context, userID []byte, seenAt time.Time) error

	SearchUsers(ctx context.Context, curUID []byte, q string, cursor *string, limit int) ([]*model.SearchUser, error)
	CheckUserExists(ctx context.Context, id []byte) (bool, error)

	GetContactPair(ctx context.Context, uid1, uid2 []byte) ([]*model.UserContact, error)
	UpdateContactStatus(ctx context.Context, contactID uint64, status model.ContactStatus) (int64, error)
	CreateContactRequest(ctx context.Context, userID, contactID []byte) error
	GetContactRecord(ctx context.Context, userID, contactID []byte) (*model.UserContact, error)
	GetAcceptedContacts(ctx context.Context, userID []byte, cursor *string, limit int) ([]*model.SearchUser, error)
	GetIncomingRequests(ctx context.Context, userID []byte, cursor *string, limit int) ([]*model.SearchUser, error)
}

type UserTokenRepository interface {
	FindByJTI(ctx context.Context, jti []byte) (*model.UserToken, error)
	GetJTIByUserAndDevice(ctx context.Context, userID, deviceID []byte) ([]byte, error)
	Upsert(ctx context.Context, token *model.UserToken) error
	CountByUserID(ctx context.Context, userID []byte) (int64, error)
	GetOldestJTIByUserIDBeyondLimit(ctx context.Context, userID []byte, limit int) ([][]byte, error)
	DeleteOldestBeyondLimit(ctx context.Context, userID []byte, limit int) error
	DeleteByJTI(ctx context.Context, jti []byte) error
	UpdateLastUsed(ctx context.Context, jti []byte, usedAt time.Time) error
}
