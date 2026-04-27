package repository

import (
	"context"
	"errors"

	"github.com/plion676/feed-flow/internal/model"
	"gorm.io/gorm"
)

type FeedDLQOperatorRepository struct {
	db *gorm.DB
}

func NewFeedDLQOperatorRepository(db *gorm.DB) *FeedDLQOperatorRepository {
	return &FeedDLQOperatorRepository{db: db}
}

func (r *FeedDLQOperatorRepository) GetByUserID(ctx context.Context, userID int64) (*model.FeedDLQOperator, error) {
	if userID <= 0 {
		return nil, nil
	}

	var operator model.FeedDLQOperator
	err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&operator).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &operator, nil
}
