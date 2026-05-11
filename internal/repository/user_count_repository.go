package repository

import (
	"context"
	"errors"

	"github.com/plion676/feed-flow/internal/model"
	"gorm.io/gorm"
)

// UserCountRepository is the data-access abstraction for user counters.
type UserCountRepository struct{ db *gorm.DB }

func NewUserCountRepository(db *gorm.DB) *UserCountRepository {
	return &UserCountRepository{db: db}
}

func (r *UserCountRepository) InitTx(ctx context.Context, tx *gorm.DB, userCount *model.UserCount) error {
	return tx.WithContext(ctx).Create(userCount).Error
}

func (r *UserCountRepository) GetByUserID(ctx context.Context, userID int64) (*model.UserCount, error) {
	var row model.UserCount
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

func (r *UserCountRepository) AddPostCountTx(ctx context.Context, tx *gorm.DB, userID int64, delta int64) error {
	return r.addCountDeltaTx(ctx, tx, userID, map[string]int64{
		"post_count": delta,
	})
}

func (r *UserCountRepository) AddFollowingCountTx(ctx context.Context, tx *gorm.DB, userID int64, delta int64) error {
	return r.addCountDeltaTx(ctx, tx, userID, map[string]int64{
		"following_count": delta,
	})
}

func (r *UserCountRepository) AddFollowerCountTx(ctx context.Context, tx *gorm.DB, userID int64, delta int64) error {
	return r.addCountDeltaTx(ctx, tx, userID, map[string]int64{
		"follower_count": delta,
	})
}

func (r *UserCountRepository) addCountDeltaTx(ctx context.Context, tx *gorm.DB, userID int64, deltas map[string]int64) error {
	if tx == nil {
		return gorm.ErrInvalidDB
	}
	if userID <= 0 || len(deltas) == 0 {
		return nil
	}

	updates := make(map[string]any, len(deltas))
	for column, delta := range deltas {
		if delta == 0 {
			continue
		}
		updates[column] = gorm.Expr(column+" + ?", delta)
	}
	if len(updates) == 0 {
		return nil
	}

	result := tx.WithContext(ctx).
		Model(&model.UserCount{}).
		Where("user_id = ?", userID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *UserCountRepository) BatchGetFollowerCounts(ctx context.Context, userIDs []int64) (map[int64]int64, error) {
	result := make(map[int64]int64, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}

	var rows []model.UserCount
	if err := r.db.WithContext(ctx).
		Model(&model.UserCount{}).
		Where("user_id IN ?", userIDs).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		result[userID] = 0
	}
	for _, row := range rows {
		if row.UserID <= 0 {
			continue
		}
		result[row.UserID] = row.FollowerCount
	}
	return result, nil
}
