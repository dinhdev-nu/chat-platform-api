package service

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"

	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	"github.com/dinhdev-nu/chat-platform-api/internal/repository"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
	"go.uber.org/zap"
)

type BulkPresenceReader interface {
	BulkIsOnline(context.Context, [][]byte) (map[string]bool, error)
}

type ProfileCacheWriter interface {
	WarmUser(context.Context, []byte, string) error
}

type UserDependencies struct {
	Users     repository.UserRepository
	UserCache ProfileCacheWriter
	Presence  BulkPresenceReader
	Controls  ControlPublisher
	Logger    *zap.Logger
}

type UserService struct {
	userRepo  repository.UserRepository
	userCache ProfileCacheWriter
	presence  BulkPresenceReader
	controls  ControlPublisher
	logger    *zap.Logger
}

const (
	sysContactsAdd = "contacts.add"
)

type contactSysEvent struct {
	Type   string `json:"type"`
	UserID string `json:"user_id,omitempty"`
}

func NewUserService(d UserDependencies) *UserService {
	return &UserService{
		userRepo:  d.Users,
		userCache: d.UserCache,
		presence:  d.Presence,
		controls:  d.Controls,
		logger:    loggerOrNop(d.Logger),
	}
}

func (s *UserService) UpdateUser(ctx context.Context, userID []byte, update *model.UserProfileUpdate) (*model.User, error) {
	err := s.userRepo.Update(ctx, userID, update)
	if err != nil {
		return nil, ae.Internal(err)
	}

	updated, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, ae.Internal(err)
	}

	go func(parent context.Context) {
		cacheCtx, cancel := detachedContext(parent, cacheTaskTimeout)
		defer cancel()

		payload, err := json.Marshal(updated)
		if err != nil {
			s.logger.Warn("userService.UpdateUser: failed to marshal user cache payload", zap.Error(err))
			return
		}
		if err := s.userCache.WarmUser(cacheCtx, userID, string(payload)); err != nil {
			s.logger.Warn("failed to warm updated user cache", zap.Error(err))
		}
	}(ctx)

	return updated, nil
}

func (s *UserService) Search(ctx context.Context, uid []byte, q string, cursor *string, limit int) (*ResultPage[*model.SearchUser], error) {
	limit = normalizePageLimit(limit)
	rows, err := s.userRepo.SearchUsers(ctx, uid, q, cursor, limit+1)
	if err != nil {
		return nil, ae.Internal(err)
	}
	return assembleUserPage(rows, limit), nil
}

func (s *UserService) SendContactRequest(ctx context.Context, senderUID, targetUID []byte) (model.ContactRequestResult, error) {
	if bytes.Equal(senderUID, targetUID) {
		return "", ae.New(ae.ErrInvalidInput, "cannot send request to yourself")
	}

	existing, err := s.userRepo.CheckUserExists(ctx, targetUID)
	if err != nil {
		return "", ae.Internal(err)
	}
	if !existing {
		return "", ae.NotFound("user not found")
	}

	pairs, err := s.userRepo.GetContactPair(ctx, senderUID, targetUID)
	if err != nil {
		return "", ae.Internal(err)
	}

	for _, p := range pairs {
		switch {
		case bytes.Equal(p.UserID, senderUID) && p.Status == model.ContactStatusBlocked:
			return "", ae.New(ae.ErrCannotSendContactRequest, "you have blocked this user")
		case bytes.Equal(p.ContactID, senderUID) && p.Status == model.ContactStatusBlocked:
			return "", ae.New(ae.ErrCannotSendContactRequest, "you cannot send request to this user")
		case p.Status == model.ContactStatusAccepted:
			return "", ae.New(ae.ErrCannotSendContactRequest, "already friends")
		case bytes.Equal(p.UserID, senderUID) && p.Status == model.ContactStatusPending:
			return "", ae.New(ae.ErrCannotSendContactRequest, "request already sent")
		case bytes.Equal(p.ContactID, senderUID) && p.Status == model.ContactStatusPending:
			// Đối phương đã gửi request → auto-accept.
			_, err := s.userRepo.UpdateContactStatus(ctx, p.ID, model.ContactStatusAccepted)
			if err != nil {
				return "", ae.Internal(err)
			}
			s.publishContactAccepted(ctx, senderUID, targetUID)
			return model.ContactRequestResultAccepted, nil
		}
	}

	// Tạo mới request
	if err := s.userRepo.CreateContactRequest(ctx, senderUID, targetUID); err != nil {
		return "", ae.Internal(err)
	}
	return model.ContactRequestResultPending, nil
}

func (s *UserService) AcceptContactRequest(ctx context.Context, currentUID, senderUID []byte) error {
	if bytes.Equal(currentUID, senderUID) {
		return ae.New(ae.ErrInvalidInput, "cannot accept request from yourself")
	}
	contact, err := s.userRepo.GetContactRecord(ctx, senderUID, currentUID)
	if err != nil {
		return ae.Internal(err)
	}
	if contact == nil {
		return ae.NotFound("contact request not found")
	}
	if contact.Status != model.ContactStatusPending {
		return ae.New(ae.ErrInvalidInput, "contact request is not pending")
	}

	effected, err := s.userRepo.UpdateContactStatus(ctx, contact.ID, model.ContactStatusAccepted)
	if err != nil {
		return ae.Internal(err)
	}
	if effected == 0 {
		return ae.New(ae.ErrInvalidInput, "contact request is not found")
	}
	s.publishContactAccepted(ctx, currentUID, senderUID)
	return nil
}

func (s *UserService) GetContacts(ctx context.Context, userID []byte, cursor *string, limit int) (*ResultPage[*model.SearchUser], error) {
	limit = normalizePageLimit(limit)
	rows, err := s.userRepo.GetAcceptedContacts(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, ae.Internal(err)
	}
	page := assembleUserPage(rows, limit)
	s.attachOnlineStatus(ctx, page.Items)
	return page, nil
}

func (s *UserService) GetIncomingContactRequests(ctx context.Context, userID []byte, cursor *string, limit int) (*ResultPage[*model.SearchUser], error) {
	limit = normalizePageLimit(limit)
	rows, err := s.userRepo.GetIncomingRequests(ctx, userID, cursor, limit+1)
	if err != nil {
		return nil, ae.Internal(err)
	}
	return assembleUserPage(rows, limit), nil
}

func assembleUserPage(rows []*model.SearchUser, limit int) *ResultPage[*model.SearchUser] {
	hasMore := len(rows) == limit+1
	if hasMore {
		rows = rows[:limit]
	}

	nextCursor := ""
	if hasMore {
		nextCursor = rows[len(rows)-1].Username
	}
	if rows == nil {
		rows = []*model.SearchUser{}
	}

	return &ResultPage[*model.SearchUser]{
		Items:      rows,
		HasMore:    hasMore,
		NextCursor: &nextCursor,
	}
}

func (s *UserService) attachOnlineStatus(ctx context.Context, rows []*model.SearchUser) {
	ids := make([][]byte, 0, len(rows))
	for _, row := range rows {
		id, err := hex.DecodeString(row.ID)
		if err == nil && len(id) == 16 {
			ids = append(ids, id)
		}
	}

	onlineByID := map[string]bool{}
	if s.presence != nil && len(ids) > 0 {
		if result, err := s.presence.BulkIsOnline(ctx, ids); err == nil {
			onlineByID = result
		}
	}

	for _, row := range rows {
		isOnline := onlineByID[row.ID]
		row.IsOnline = &isOnline
	}
}

func (s *UserService) publishContactAccepted(ctx context.Context, uid1, uid2 []byte) {
	uid1Hex := hex.EncodeToString(uid1)
	uid2Hex := hex.EncodeToString(uid2)
	publishControlEvent(ctx, s.controls, s.logger, uid1, contactSysEvent{
		Type:   sysContactsAdd,
		UserID: uid2Hex,
	})
	publishControlEvent(ctx, s.controls, s.logger, uid2, contactSysEvent{
		Type:   sysContactsAdd,
		UserID: uid1Hex,
	})
}
