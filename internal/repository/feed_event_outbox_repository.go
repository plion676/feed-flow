package repository

import (
	"context"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// FeedEventOutboxRepository persists relayable domain events in MySQL.
type FeedEventOutboxRepository struct {
	db *gorm.DB
}

func NewFeedEventOutboxRepository(db *gorm.DB) *FeedEventOutboxRepository {
	return &FeedEventOutboxRepository{db: db}
}

func (r *FeedEventOutboxRepository) CreateTx(ctx context.Context, tx *gorm.DB, event *model.FeedEventOutbox) error {
	return tx.WithContext(ctx).Create(event).Error
}

func (r *FeedEventOutboxRepository) ClaimPending(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]*model.FeedEventOutbox, error) {
	if limit <= 0 {
		limit = 20
	}

	var rows []*model.FeedEventOutbox
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status = ? AND next_retry_at <= ?", model.FeedEventOutboxStatusPending, now).
			Order("next_retry_at ASC, id ASC").
			Limit(limit)
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}

		ids := make([]int64, 0, len(rows))
		for _, row := range rows {
			if row == nil {
				continue
			}
			ids = append(ids, row.ID)
		}
		if len(ids) == 0 {
			return nil
		}

		lockUntil := now.Add(30 * time.Second)
		return tx.WithContext(ctx).
			Model(&model.FeedEventOutbox{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"next_retry_at": lockUntil,
			}).Error
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *FeedEventOutboxRepository) MarkSent(ctx context.Context, id int64, sentAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.FeedEventOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     model.FeedEventOutboxStatusSent,
			"sent_at":    sentAt,
			"last_error": "",
		}).Error
}

func (r *FeedEventOutboxRepository) MarkPublishFailed(
	ctx context.Context,
	id int64,
	retryCount int32,
	nextRetryAt time.Time,
	lastError string,
) error {
	return r.db.WithContext(ctx).
		Model(&model.FeedEventOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"retry_count":   retryCount,
			"next_retry_at": nextRetryAt,
			"last_error":    lastError,
		}).Error
}
