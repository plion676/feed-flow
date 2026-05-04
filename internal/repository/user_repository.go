package repository

import (
	"context"
	"errors"

	"github.com/plion676/feed-flow/internal/model"
	"gorm.io/gorm"
)

// UserRepository is the data-access abstraction for users.
// The current implementation is intentionally a scaffold, so you can fill in the real DB logic.
type UserRepository struct{ db *gorm.DB }

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("username = ?", username).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) GetByID(ctx context.Context, userID int64) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}

func (r *UserRepository) GetByIDs(ctx context.Context, userIDs []int64) ([]*model.User, error) {
	if len(userIDs) == 0 {
		return []*model.User{}, nil
	}

	var rows []model.User
	err := r.db.WithContext(ctx).Where("id IN ?", userIDs).Find(&rows).Error
	if err != nil {
		return nil, err
	}

	users := make([]*model.User, 0, len(rows))
	for i := range rows {
		user := rows[i]
		users = append(users, &user)
	}

	return users, nil
}

func (r *UserRepository) CreateTx(ctx context.Context, tx *gorm.DB, user *model.User) error {
	return tx.WithContext(ctx).Create(user).Error
}
