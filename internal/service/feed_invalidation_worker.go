package service

import (
	"context"
	"fmt"
	"sync"
)

type workerFollowRepository interface {
	ListFollowerUserIDs(ctx context.Context, targetUserID int64) ([]int64, error)
}

type workerFeedCacheInvalidator interface {
	InvalidateHomeFeed(ctx context.Context, userID int64) error
}

// FeedInvalidationWorker handles async cache invalidation events.
type FeedInvalidationWorker struct {
	followRepo      workerFollowRepository
	feedInvalidator workerFeedCacheInvalidator
}

const defaultFollowerInvalidationWorkers = 20

func NewFeedInvalidationWorker(followRepo workerFollowRepository, feedInvalidator workerFeedCacheInvalidator) *FeedInvalidationWorker {
	return &FeedInvalidationWorker{
		followRepo:      followRepo,
		feedInvalidator: feedInvalidator,
	}
}

func (w *FeedInvalidationWorker) HandlePostCreated(ctx context.Context, authorUserID int64) error {
	if authorUserID <= 0 {
		return nil
	}
	if w.followRepo == nil || w.feedInvalidator == nil {
		return fmt.Errorf("feed invalidation worker dependencies are not configured")
	}

	followerIDs, err := w.followRepo.ListFollowerUserIDs(ctx, authorUserID)
	if err != nil {
		return fmt.Errorf("list follower user ids by author_id=%d: %w", authorUserID, err)
	}
	if len(followerIDs) == 0 {
		return nil
	}

	workerCount := defaultFollowerInvalidationWorkers
	if workerCount > len(followerIDs) {
		workerCount = len(followerIDs)
	}

	jobs := make(chan int64)
	var wg sync.WaitGroup
	var mu sync.Mutex

	var firstErr error
	failedFollowerIDs := make([]int64, 0)

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for followerID := range jobs {
				if err := w.feedInvalidator.InvalidateHomeFeed(ctx, followerID); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					failedFollowerIDs = append(failedFollowerIDs, followerID)
					mu.Unlock()
				}
			}
		}()
	}

	for _, followerID := range followerIDs {
		select {
		case jobs <- followerID:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()

	if len(failedFollowerIDs) > 0 {
		return fmt.Errorf(
			"invalidate home feed failed for %d follower(s), first_failed_follower_id=%d: %w",
			len(failedFollowerIDs),
			failedFollowerIDs[0],
			firstErr,
		)
	}

	return nil
}
