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

func (r *PostRepository) SoftDeleteByIDAndUserID(ctx context.Context, postID int64, userID int64) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Post{}).
		Where("id = ? AND user_id = ? AND status = ?", postID, userID, model.PostStatusPublished).
		Update("status", model.PostStatusDeleted)
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}

func (r *PostRepository) ListByUserIDs(ctx context.Context, userIDs []int64, lastPostID int64, limit int) ([]*model.Post, error) {
	if len(userIDs) == 0 {
		return []*model.Post{}, nil
	}

	query := r.db.WithContext(ctx).
		Model(&model.Post{}).
		Where("status = ?", model.PostStatusPublished).
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

func (r *PostRepository) ListByUserID(ctx context.Context, userID int64, lastPostID int64, limit int) ([]*model.Post, error) {
	query := r.db.WithContext(ctx).
		Model(&model.Post{}).
		Where("status = ?", model.PostStatusPublished).
		Where("user_id = ?", userID)

	if lastPostID > 0 {
		query = query.Where("id < ?", lastPostID)
	}

	var posts []*model.Post
	if err := query.Order("id DESC").Limit(limit).Find(&posts).Error; err != nil {
		return nil, err
	}

	return posts, nil
}

func (r *PostRepository) CountPublishedByUserID(ctx context.Context, userID int64) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&model.Post{}).
		Where("status = ?", model.PostStatusPublished).
		Where("user_id = ?", userID).
		Count(&count).Error
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (r *PostRepository) ListByIDs(ctx context.Context, postIDs []int64) ([]*model.Post, error) {
	if len(postIDs) == 0 {
		return []*model.Post{}, nil
	}

	var posts []*model.Post
	if err := r.db.WithContext(ctx).
		Model(&model.Post{}).
		Where("status = ?", model.PostStatusPublished).
		Where("id IN ?", postIDs).
		Find(&posts).Error; err != nil {
		return nil, err
	}

	byID := make(map[int64]*model.Post, len(posts))
	for _, post := range posts {
		byID[post.ID] = post
	}

	ordered := make([]*model.Post, 0, len(posts))
	for _, postID := range postIDs {
		post, ok := byID[postID]
		if !ok {
			continue
		}
		ordered = append(ordered, post)
	}

	return ordered, nil
}
