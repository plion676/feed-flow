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
