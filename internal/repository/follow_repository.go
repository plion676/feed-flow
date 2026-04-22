package repository

import (
	"context"

	"github.com/plion676/feed-flow/internal/model"
	"gorm.io/gorm"
)

// FollowRepository is the data-access abstraction for follow relationships.
type FollowRepository struct{ db *gorm.DB }

func NewFollowRepository(db *gorm.DB) *FollowRepository {
	return &FollowRepository{db: db}
}

func (r *FollowRepository) Create(ctx context.Context, follow *model.Follow) error {
	return r.db.WithContext(ctx).Create(follow).Error
}

func (r *FollowRepository) Delete(ctx context.Context, userID int64, targetUserID int64) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND target_user_id = ?", userID, targetUserID).
		Delete(&model.Follow{}).Error
}

func (r *FollowRepository) ListFollowingUserIDs(ctx context.Context, userID int64) ([]int64, error) {
	var targetIDs []int64
	err := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Where("user_id = ?", userID).
		Pluck("target_user_id", &targetIDs).Error
	if err != nil {
		return nil, err
	}

	return targetIDs, nil
}

func (r *FollowRepository) ListFollowerUserIDs(ctx context.Context, targetUserID int64) ([]int64, error) {
	var followerIDs []int64
	err := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Where("target_user_id = ?", targetUserID).
		Pluck("user_id", &followerIDs).Error
	if err != nil {
		return nil, err
	}

	return followerIDs, nil
}
