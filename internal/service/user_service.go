package service

import (
	"context"
	"encoding/json"

	g "github.com/dinhdev-nu/chat-platform-api/global"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	ae "github.com/dinhdev-nu/chat-platform-api/pkg/errors"
)

type UserService struct {
	userRepo r.UserRepository
}

func NewUserService(ur r.UserRepository) *UserService {
	return &UserService{userRepo: ur}
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

	go func() {
		payload, err := json.Marshal(updated)
		if err != nil {
			return
		}
		g.Session.WarmUser(context.Background(), userID, string(payload))
	}()

	return updated, nil
}

func (s *UserService) Search(ctx context.Context, uid []byte, q string, cursor *string, limit int) (*ResultPage[*model.SearchUser], error) {
	const maxLimit = 50
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}
	fetch := limit + 1

	row, err := s.userRepo.SearchUsers(ctx, uid, q, cursor, fetch)
	if err != nil {
		return nil, ae.Internal(err)
	}

	hasMore := len(row) == fetch
	if hasMore {
		row = row[:limit]
	}

	nextCursor := ""
	if hasMore {
		last := row[len(row)-1]
		username := last.Username
		nextCursor = username
	}

	return &ResultPage[*model.SearchUser]{
		Items:      row,
		HasMore:    hasMore,
		NextCursor: &nextCursor,
	}, nil
}

func (s *UserService) SendContactRequest(ctx context.Context, senderUID, targetUID []byte) (model.ContactRequestResult, error) {
	if string(senderUID) == string(targetUID) {
		return model.ContactRequestResult(""), ae.New(ae.ErrInvalidInput, "cannot send request to yourself")
	}

	existing, err := s.userRepo.CheckUserExists(ctx, targetUID)
	if err != nil {
		return model.ContactRequestResult(""), ae.Internal(err)
	}
	if !existing {
		return model.ContactRequestResult(""), ae.NotFound("user not found")
	}

	pairs, err := s.userRepo.GetContactPair(ctx, senderUID, targetUID)
	if err != nil {
		return model.ContactRequestResult(""), ae.Internal(err)
	}

	for _, p := range pairs {
		switch {
		case string(p.UserID) == string(senderUID) && p.Status == model.ContactStatusBlocked:
			return model.ContactRequestResult(""), ae.New(ae.ErrCannotSendContactRequest, "you have blocked this user")
		case string(p.ContactID) == string(senderUID) && p.Status == model.ContactStatusBlocked:
			return model.ContactRequestResult(""), ae.New(ae.ErrCannotSendContactRequest, "you cannot send request to this user")
		case p.Status == model.ContactStatusAccepted:
			return model.ContactRequestResult(""), ae.New(ae.ErrCannotSendContactRequest, "already friends")
		case string(p.UserID) == string(senderUID) && p.Status == model.ContactStatusPending:
			return model.ContactRequestResult(""), ae.New(ae.ErrCannotSendContactRequest, "request already sent")
		case string(p.ContactID) == string(senderUID) && p.Status == model.ContactStatusPending:
			// Đối phương đã gửi request → auto-accept.
			_, err := s.userRepo.UpdateContactStatus(ctx, p.ID, model.ContactStatusAccepted)
			if err != nil {
				return model.ContactRequestResult(""), ae.Internal(err)
			}
			return model.ContactRequestResultAccepted, nil
		}
	}

	// Tạo mới request
	if err := s.userRepo.CreateContactRequest(ctx, senderUID, targetUID); err != nil {
		return model.ContactRequestResult(""), ae.Internal(err)
	}
	return model.ContactRequestResultPending, nil
}

func (s *UserService) AcceptContactRequest(ctx context.Context, currentUID, senderUID []byte) error {
	if string(currentUID) == string(senderUID) {
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
	return nil
}

func (s *UserService) GetContacts(ctx context.Context, userID []byte, cursor *string, limit int) (*ResultPage[*model.SearchUser], error) {
	const maxLimit = 50
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}
	fetch := limit + 1

	rows, err := s.userRepo.GetAcceptedContacts(ctx, userID, cursor, fetch)
	if err != nil {
		return nil, ae.Internal(err)
	}

	hasMore := len(rows) == fetch
	if hasMore {
		rows = rows[:limit]
	}

	nextCursor := ""
	if hasMore {
		last := rows[len(rows)-1]
		username := last.Username
		nextCursor = username
	}

	return &ResultPage[*model.SearchUser]{
		Items:      rows,
		HasMore:    hasMore,
		NextCursor: &nextCursor,
	}, nil
}

func (s *UserService) GetIncomingContactRequests(ctx context.Context, userID []byte, cursor *string, limit int) (*ResultPage[*model.SearchUser], error) {
	const maxLimit = 50
	if limit <= 0 || limit > maxLimit {
		limit = maxLimit
	}
	fetch := limit + 1

	rows, err := s.userRepo.GetIncomingRequests(ctx, userID, cursor, fetch)
	if err != nil {
		return nil, ae.Internal(err)
	}

	hasMore := len(rows) == fetch
	if hasMore {
		rows = rows[:limit]
	}

	nextCursor := ""
	if hasMore {
		last := rows[len(rows)-1]
		username := last.Username
		nextCursor = username
	}

	return &ResultPage[*model.SearchUser]{
		Items:      rows,
		HasMore:    hasMore,
		NextCursor: &nextCursor,
	}, nil
}
