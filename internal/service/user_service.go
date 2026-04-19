package service

import (
	"context"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type userReadRepository interface {
	GetByID(ctx context.Context, userID int64) (*model.User, error)
}

// UserService handles user profile related read operations.
type UserService struct {
	userRepo userReadRepository
}

type MeResult struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
}

func NewUserService(userRepo userReadRepository) *UserService {
	return &UserService{userRepo: userRepo}
}

func (s *UserService) GetMe(ctx context.Context, userID int64) (*MeResult, *xerror.Error) {
	if userID <= 0 {
		return nil, xerror.ErrUnauthorized
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if user == nil {
		return nil, xerror.ErrUnauthorized
	}

	return &MeResult{
		UserID:   user.ID,
		Username: user.Username,
		Nickname: user.Nickname,
		Avatar:   user.Avatar,
		Bio:      user.Bio,
	}, nil
}
