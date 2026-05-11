package repository

import (
	"context"
	"errors"

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

func (r *FollowRepository) CreateTx(ctx context.Context, tx *gorm.DB, follow *model.Follow) error {
	return tx.WithContext(ctx).Create(follow).Error
}

func (r *FollowRepository) Delete(ctx context.Context, userID int64, targetUserID int64) (bool, error) {
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND target_user_id = ?", userID, targetUserID).
		Delete(&model.Follow{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r *FollowRepository) DeleteTx(ctx context.Context, tx *gorm.DB, userID int64, targetUserID int64) (bool, error) {
	result := tx.WithContext(ctx).
		Where("user_id = ? AND target_user_id = ?", userID, targetUserID).
		Delete(&model.Follow{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
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

func (r *FollowRepository) ListFollowingRelations(ctx context.Context, userID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
	query := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Where("user_id = ?", userID)
	if lastFollowID > 0 {
		query = query.Where("id < ?", lastFollowID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}

	var rows []model.Follow
	err := query.Order("id DESC").Find(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]*model.Follow, 0, len(rows))
	for i := range rows {
		follow := rows[i]
		items = append(items, &follow)
	}
	return items, nil
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

func (r *FollowRepository) ListFollowerRelations(ctx context.Context, targetUserID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
	query := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Where("target_user_id = ?", targetUserID)
	if lastFollowID > 0 {
		query = query.Where("id < ?", lastFollowID)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}

	var rows []model.Follow
	err := query.Order("id DESC").Find(&rows).Error
	if err != nil {
		return nil, err
	}

	items := make([]*model.Follow, 0, len(rows))
	for i := range rows {
		follow := rows[i]
		items = append(items, &follow)
	}
	return items, nil
}

func (r *FollowRepository) CountFollowing(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Where("user_id = ?", userID).
		Count(&count).Error
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (r *FollowRepository) CountFollowers(ctx context.Context, targetUserID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Follow{}).
		Where("target_user_id = ?", targetUserID).
		Count(&count).Error
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (r *FollowRepository) Exists(ctx context.Context, userID int64, targetUserID int64) (bool, error) {
	var follow model.Follow
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND target_user_id = ?", userID, targetUserID).
		First(&follow).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}
