package repository

import (
	"context"
	"errors"

	"github.com/plion676/feed-flow/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PostInteractionRepository stores post likes, collects, and comments.
type PostInteractionRepository struct{ db *gorm.DB }

func NewPostInteractionRepository(db *gorm.DB) *PostInteractionRepository {
	return &PostInteractionRepository{db: db}
}

func (r *PostInteractionRepository) Like(ctx context.Context, userID int64, postID int64) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.PostLike{
		UserID: userID,
		PostID: postID,
	}).Error
}

func (r *PostInteractionRepository) Unlike(ctx context.Context, userID int64, postID int64) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND post_id = ?", userID, postID).Delete(&model.PostLike{}).Error
}

func (r *PostInteractionRepository) Collect(ctx context.Context, userID int64, postID int64) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.PostCollect{
		UserID: userID,
		PostID: postID,
	}).Error
}

func (r *PostInteractionRepository) Uncollect(ctx context.Context, userID int64, postID int64) error {
	return r.db.WithContext(ctx).Where("user_id = ? AND post_id = ?", userID, postID).Delete(&model.PostCollect{}).Error
}

func (r *PostInteractionRepository) CreateComment(ctx context.Context, comment *model.PostComment) error {
	return r.db.WithContext(ctx).Create(comment).Error
}

func (r *PostInteractionRepository) ListComments(ctx context.Context, postID int64, lastCommentID int64, limit int) ([]*model.PostComment, error) {
	query := r.db.WithContext(ctx).
		Model(&model.PostComment{}).
		Where("post_id = ? AND status = ?", postID, model.CommentStatusPublished)

	if lastCommentID > 0 {
		query = query.Where("id < ?", lastCommentID)
	}

	var rows []*model.PostComment
	if err := query.Order("id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *PostInteractionRepository) CountLikesByPostIDs(ctx context.Context, postIDs []int64) (map[int64]int64, error) {
	return countByPostIDs[model.PostLike](ctx, r.db, postIDs)
}

func (r *PostInteractionRepository) CountCollectsByPostIDs(ctx context.Context, postIDs []int64) (map[int64]int64, error) {
	return countByPostIDs[model.PostCollect](ctx, r.db, postIDs)
}

func (r *PostInteractionRepository) CountCommentsByPostIDs(ctx context.Context, postIDs []int64) (map[int64]int64, error) {
	if len(postIDs) == 0 {
		return map[int64]int64{}, nil
	}

	type row struct {
		PostID int64
		Count  int64
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&model.PostComment{}).
		Select("post_id, count(*) as count").
		Where("post_id IN ? AND status = ?", postIDs, model.CommentStatusPublished).
		Group("post_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	counts := make(map[int64]int64, len(rows))
	for _, item := range rows {
		counts[item.PostID] = item.Count
	}
	return counts, nil
}

func (r *PostInteractionRepository) ListLikedPostIDs(ctx context.Context, userID int64, postIDs []int64) ([]int64, error) {
	return listUserPostIDs[model.PostLike](ctx, r.db, userID, postIDs)
}

func (r *PostInteractionRepository) ListCollectedPostIDs(ctx context.Context, userID int64, postIDs []int64) ([]int64, error) {
	return listUserPostIDs[model.PostCollect](ctx, r.db, userID, postIDs)
}

type postIDCountRow struct {
	PostID int64
	Count  int64
}

func countByPostIDs[T any](ctx context.Context, db *gorm.DB, postIDs []int64) (map[int64]int64, error) {
	if len(postIDs) == 0 {
		return map[int64]int64{}, nil
	}

	var rows []postIDCountRow
	if err := db.WithContext(ctx).
		Model(new(T)).
		Select("post_id, count(*) as count").
		Where("post_id IN ?", postIDs).
		Group("post_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	counts := make(map[int64]int64, len(rows))
	for _, item := range rows {
		counts[item.PostID] = item.Count
	}
	return counts, nil
}

func listUserPostIDs[T any](ctx context.Context, db *gorm.DB, userID int64, postIDs []int64) ([]int64, error) {
	if len(postIDs) == 0 {
		return []int64{}, nil
	}

	var rows []struct {
		PostID int64
	}
	if err := db.WithContext(ctx).
		Model(new(T)).
		Select("post_id").
		Where("user_id = ? AND post_id IN ?", userID, postIDs).
		Scan(&rows).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return []int64{}, nil
		}
		return nil, err
	}

	ids := make([]int64, 0, len(rows))
	for _, item := range rows {
		ids = append(ids, item.PostID)
	}
	return ids, nil
}
