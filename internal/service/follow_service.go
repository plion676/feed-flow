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
	CreateTx(ctx context.Context, tx *gorm.DB, follow *model.Follow) error
	Delete(ctx context.Context, userID int64, targetUserID int64) (bool, error)
	DeleteTx(ctx context.Context, tx *gorm.DB, userID int64, targetUserID int64) (bool, error)
}

type followUserRepository interface {
	GetByID(ctx context.Context, userID int64) (*model.User, error)
}

type followFeedCacheInvalidator interface {
	InvalidateHomeFeed(ctx context.Context, userID int64) error
}

type followInboxAuthorCleanup interface {
	RemoveAuthorPostsFromInbox(ctx context.Context, userID int64, authorUserID int64) error
}

type followUserCountRepository interface {
	AddFollowingCountTx(ctx context.Context, tx *gorm.DB, userID int64, delta int64) error
	AddFollowerCountTx(ctx context.Context, tx *gorm.DB, userID int64, delta int64) error
}

// FollowService handles follow/unfollow workflows.
type FollowService struct {
	txRunner        transactionRunner
	followRepo      followRepository
	userRepo        followUserRepository
	userCountRepo   followUserCountRepository
	feedInvalidator followFeedCacheInvalidator
	inboxCleanup    followInboxAuthorCleanup
}

func NewFollowService(followRepo followRepository, userRepo followUserRepository) *FollowService {
	return &FollowService{
		followRepo: followRepo,
		userRepo:   userRepo,
	}
}

func NewFollowServiceWithTransaction(txRunner transactionRunner, followRepo followRepository, userRepo followUserRepository, userCountRepo followUserCountRepository) *FollowService {
	return &FollowService{
		txRunner:      txRunner,
		followRepo:    followRepo,
		userRepo:      userRepo,
		userCountRepo: userCountRepo,
	}
}

// WithFeedCacheInvalidator wires an optional cache invalidator for feed consistency.
func (s *FollowService) WithFeedCacheInvalidator(invalidator followFeedCacheInvalidator) *FollowService {
	s.feedInvalidator = invalidator
	return s
}

func (s *FollowService) WithInboxAuthorCleanup(cleanup followInboxAuthorCleanup) *FollowService {
	s.inboxCleanup = cleanup
	return s
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

	if err := s.runFollowInTx(ctx, follow); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return xerror.ErrFollowAlreadyExists
		}
		return xerror.ErrInternal
	}

	if s.feedInvalidator != nil {
		// Best-effort cache invalidation: follow relation is committed in DB already.
		_ = s.feedInvalidator.InvalidateHomeFeed(ctx, userID)
	}

	return nil
}

func (s *FollowService) Unfollow(ctx context.Context, userID int64, targetUserID int64) *xerror.Error {
	if userID <= 0 || targetUserID <= 0 || userID == targetUserID {
		return xerror.ErrBadRequest
	}

	deleted, err := s.runUnfollowInTx(ctx, userID, targetUserID)
	if err != nil {
		return xerror.ErrInternal
	}
	if !deleted {
		return nil
	}

	if s.feedInvalidator != nil {
		// Best-effort cache invalidation: unfollow relation is committed in DB already.
		_ = s.feedInvalidator.InvalidateHomeFeed(ctx, userID)
	}
	if s.inboxCleanup != nil {
		// Best-effort inbox cleanup: read path still validates current follow relation.
		_ = s.inboxCleanup.RemoveAuthorPostsFromInbox(ctx, userID, targetUserID)
	}

	return nil
}

func (s *FollowService) runFollowInTx(ctx context.Context, follow *model.Follow) error {
	if s.txRunner == nil || s.userCountRepo == nil {
		return s.followRepo.Create(ctx, follow)
	}

	return s.txRunner.InTx(ctx, func(tx *gorm.DB) error {
		if err := s.followRepo.CreateTx(ctx, tx, follow); err != nil {
			return err
		}
		if err := s.userCountRepo.AddFollowingCountTx(ctx, tx, follow.UserID, 1); err != nil {
			return err
		}
		if err := s.userCountRepo.AddFollowerCountTx(ctx, tx, follow.TargetUserID, 1); err != nil {
			return err
		}
		return nil
	})
}

func (s *FollowService) runUnfollowInTx(ctx context.Context, userID int64, targetUserID int64) (bool, error) {
	if s.txRunner == nil || s.userCountRepo == nil {
		return s.followRepo.Delete(ctx, userID, targetUserID)
	}

	deleted := false
	err := s.txRunner.InTx(ctx, func(tx *gorm.DB) error {
		var err error
		deleted, err = s.followRepo.DeleteTx(ctx, tx, userID, targetUserID)
		if err != nil {
			return err
		}
		if !deleted {
			return nil
		}
		if err := s.userCountRepo.AddFollowingCountTx(ctx, tx, userID, -1); err != nil {
			return err
		}
		if err := s.userCountRepo.AddFollowerCountTx(ctx, tx, targetUserID, -1); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}
