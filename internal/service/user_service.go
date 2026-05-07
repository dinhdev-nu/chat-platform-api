package service

import (
	"context"

	"github.com/dinhdev-nu/chat-platform-api/internal/dto"
	"github.com/dinhdev-nu/chat-platform-api/internal/model"
	r "github.com/dinhdev-nu/chat-platform-api/internal/repository"
	"github.com/dinhdev-nu/chat-platform-api/pkg/errors"
)

type UserService struct {
	userRepo r.UserRepository
}

func NewUserService(ur r.UserRepository) *UserService {
	return &UserService{userRepo: ur}
}

func (s *UserService) UpdateUser(ctx context.Context, userID []byte, req dto.UpdateUserRequest) (*model.User, error) {
	user := &model.User{
		Username:  req.Name,
		AvatarURL: req.AvatarURL,
		Bio:       req.Bio,
	}
	err := s.userRepo.Update(ctx, user, userID)
	if err != nil {
		return nil, errors.Internal(err)
	}

	updated, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, errors.Internal(err)
	}

	return updated, nil
}
