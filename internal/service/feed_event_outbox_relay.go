package service

import (
	"context"
	"fmt"
	"time"

	"github.com/plion676/feed-flow/internal/model"
)

type feedEventOutboxSource interface {
	ClaimPending(ctx context.Context, now time.Time, limit int) ([]*model.FeedEventOutbox, error)
	MarkSent(ctx context.Context, id int64, sentAt time.Time) error
	MarkPublishFailed(ctx context.Context, id int64, retryCount int32, nextRetryAt time.Time, lastError string) error
}

type feedEventOutboxPublisher interface {
	PublishEventPayload(ctx context.Context, outboxEventID int64, payload string) error
}

type FeedEventOutboxRelay struct {
	outboxRepo     feedEventOutboxSource
	publisher      feedEventOutboxPublisher
	batchSize      int
	initialBackoff time.Duration
	maxBackoff     time.Duration
	idleSleep      time.Duration
}

func NewFeedEventOutboxRelay(
	outboxRepo feedEventOutboxSource,
	publisher feedEventOutboxPublisher,
) *FeedEventOutboxRelay {
	return &FeedEventOutboxRelay{
		outboxRepo:     outboxRepo,
		publisher:      publisher,
		batchSize:      20,
		initialBackoff: time.Second,
		maxBackoff:     30 * time.Second,
		idleSleep:      time.Second,
	}
}

func (r *FeedEventOutboxRelay) WithBatchSize(batchSize int) *FeedEventOutboxRelay {
	if batchSize > 0 {
		r.batchSize = batchSize
	}
	return r
}

func (r *FeedEventOutboxRelay) WithRetryBackoff(initial time.Duration, max time.Duration) *FeedEventOutboxRelay {
	if initial > 0 {
		r.initialBackoff = initial
	}
	if max > 0 {
		r.maxBackoff = max
	}
	return r
}

func (r *FeedEventOutboxRelay) WithIdleSleep(idleSleep time.Duration) *FeedEventOutboxRelay {
	if idleSleep > 0 {
		r.idleSleep = idleSleep
	}
	return r
}

func (r *FeedEventOutboxRelay) Run(ctx context.Context) error {
	if r == nil || r.outboxRepo == nil || r.publisher == nil {
		return fmt.Errorf("feed event outbox relay dependencies are not configured")
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		rows, err := r.outboxRepo.ClaimPending(ctx, time.Now(), r.batchSize)
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			if err := sleepWithContext(ctx, r.idleSleep); err != nil {
				return err
			}
			continue
		}

		for _, row := range rows {
			if row == nil {
				continue
			}
			if err := r.publisher.PublishEventPayload(ctx, row.ID, row.Payload); err != nil {
				retryCount := row.RetryCount + 1
				nextRetryAt := time.Now().Add(r.resolveRetryBackoff(retryCount))
				if markErr := r.outboxRepo.MarkPublishFailed(ctx, row.ID, retryCount, nextRetryAt, err.Error()); markErr != nil {
					return fmt.Errorf("mark outbox publish failed id=%d: %w", row.ID, markErr)
				}
				continue
			}
			if err := r.outboxRepo.MarkSent(ctx, row.ID, time.Now()); err != nil {
				return fmt.Errorf("mark outbox sent id=%d: %w", row.ID, err)
			}
		}
	}
}

func (r *FeedEventOutboxRelay) resolveRetryBackoff(retryCount int32) time.Duration {
	backoff := r.initialBackoff
	if backoff <= 0 {
		backoff = time.Second
	}
	if retryCount <= 1 {
		return backoff
	}

	for i := int32(1); i < retryCount; i++ {
		backoff *= 2
		if r.maxBackoff > 0 && backoff >= r.maxBackoff {
			return r.maxBackoff
		}
	}
	if r.maxBackoff > 0 && backoff > r.maxBackoff {
		return r.maxBackoff
	}
	return backoff
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
