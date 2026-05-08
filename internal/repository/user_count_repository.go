package repository

import (
	"context"

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
