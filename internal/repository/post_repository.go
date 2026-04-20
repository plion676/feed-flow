package repository

import (
	"context"
	"errors"

	"github.com/plion676/feed-flow/internal/model"
	"gorm.io/gorm"
)

// PostRepository is the data-access abstraction for posts.
type PostRepository struct{ db *gorm.DB }

func NewPostRepository(db *gorm.DB) *PostRepository {
	return &PostRepository{db: db}
}

func (r *PostRepository) Create(ctx context.Context, post *model.Post) error {
	return r.db.WithContext(ctx).Create(post).Error
}

func (r *PostRepository) GetByID(ctx context.Context, postID int64) (*model.Post, error) {
	var post model.Post
	err := r.db.WithContext(ctx).Where("id = ?", postID).First(&post).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return &post, nil
}

func (r *PostRepository) ListByUserIDs(ctx context.Context, userIDs []int64, lastPostID int64, limit int) ([]*model.Post, error) {
	if len(userIDs) == 0 {
		return []*model.Post{}, nil
	}

	query := r.db.WithContext(ctx).
		Model(&model.Post{}).
		Where("status = 1").
		Where("user_id IN ?", userIDs)

	if lastPostID > 0 {
		query = query.Where("id < ?", lastPostID)
	}

	var posts []*model.Post
	if err := query.Order("id DESC").Limit(limit).Find(&posts).Error; err != nil {
		return nil, err
	}

	return posts, nil
}
