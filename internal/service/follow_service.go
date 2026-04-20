package service

import (
	"context"
	"errors"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"gorm.io/gorm"
)

type followRepository interface {
	Create(ctx context.Context, follow *model.Follow) error
	Delete(ctx context.Context, userID int64, targetUserID int64) error
}

type followUserRepository interface {
	GetByID(ctx context.Context, userID int64) (*model.User, error)
}

// FollowService handles follow/unfollow workflows.
type FollowService struct {
	followRepo followRepository
	userRepo   followUserRepository
}

func NewFollowService(followRepo followRepository, userRepo followUserRepository) *FollowService {
	return &FollowService{
		followRepo: followRepo,
		userRepo:   userRepo,
	}
}

func (s *FollowService) Follow(ctx context.Context, userID int64, targetUserID int64) *xerror.Error {
	if userID <= 0 || targetUserID <= 0 || userID == targetUserID {
		return xerror.ErrBadRequest
	}

	targetUser, err := s.userRepo.GetByID(ctx, targetUserID)
	if err != nil {
		return xerror.ErrInternal
	}
	if targetUser == nil {
		return xerror.ErrNotFound
	}

	follow := &model.Follow{
		UserID:       userID,
		TargetUserID: targetUserID,
	}

	if err := s.followRepo.Create(ctx, follow); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return xerror.ErrFollowAlreadyExists
		}
		return xerror.ErrInternal
	}

	return nil
}

func (s *FollowService) Unfollow(ctx context.Context, userID int64, targetUserID int64) *xerror.Error {
	if userID <= 0 || targetUserID <= 0 || userID == targetUserID {
		return xerror.ErrBadRequest
	}

	if err := s.followRepo.Delete(ctx, userID, targetUserID); err != nil {
		return xerror.ErrInternal
	}

	return nil
}
