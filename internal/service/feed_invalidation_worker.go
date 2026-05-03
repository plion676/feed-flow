package service

import (
	"context"
	"fmt"
	"log"
	"sync"
)

type workerFollowRepository interface {
	ListFollowerUserIDs(ctx context.Context, targetUserID int64) ([]int64, error)
}

type workerFeedCacheInvalidator interface {
	InvalidateHomeFeed(ctx context.Context, userID int64) error
}

type workerFeedInboxFanout interface {
	FanoutPostToFollowers(ctx context.Context, followerIDs []int64, postID int64, occurredAt int64) error
}

type PostCreatedEvent struct {
	StreamID     string
	AuthorUserID int64
	PostID       int64
	OccurredAt   int64
}

// FeedInvalidationWorker handles async cache invalidation events.
type FeedInvalidationWorker struct {
	followRepo      workerFollowRepository
	feedInvalidator workerFeedCacheInvalidator
	hybridPolicy    *FeedHybridPolicy
	inboxFanout     workerFeedInboxFanout
}

const defaultFollowerInvalidationWorkers = 20

func NewFeedInvalidationWorker(followRepo workerFollowRepository, feedInvalidator workerFeedCacheInvalidator) *FeedInvalidationWorker {
	return &FeedInvalidationWorker{
		followRepo:      followRepo,
		feedInvalidator: feedInvalidator,
		hybridPolicy:    NewFeedHybridPolicy(0),
	}
}

func (w *FeedInvalidationWorker) HandlePostCreated(ctx context.Context, authorUserID int64) error {
	return w.HandlePostCreatedEvent(ctx, PostCreatedEvent{
		AuthorUserID: authorUserID,
	})
}

func (w *FeedInvalidationWorker) WithHybridPolicy(policy *FeedHybridPolicy) *FeedInvalidationWorker {
	if policy != nil {
		w.hybridPolicy = policy
	}
	return w
}

func (w *FeedInvalidationWorker) WithInboxFanout(inboxFanout workerFeedInboxFanout) *FeedInvalidationWorker {
	w.inboxFanout = inboxFanout
	return w
}

func (w *FeedInvalidationWorker) HandlePostCreatedEvent(ctx context.Context, event PostCreatedEvent) error {
	authorUserID := event.AuthorUserID
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

	mode := FeedDeliveryPullOnly
	if w.hybridPolicy != nil {
		mode = w.hybridPolicy.DecideByFollowerCount(len(followerIDs))
	}
	log.Printf(
		"feed event dispatch stream_id=%s author_id=%d post_id=%d follower_count=%d mode=%s",
		event.StreamID,
		event.AuthorUserID,
		event.PostID,
		len(followerIDs),
		mode,
	)
	if mode == FeedDeliveryPushAndPull && w.inboxFanout != nil && event.PostID > 0 {
		fanoutCtx := withFeedEventLogFields(ctx, feedEventLogFields{
			StreamID:     event.StreamID,
			AuthorUserID: event.AuthorUserID,
			PostID:       event.PostID,
		})
		if err := w.inboxFanout.FanoutPostToFollowers(fanoutCtx, followerIDs, event.PostID, event.OccurredAt); err != nil {
			return fmt.Errorf("push inbox fanout failed for post_id=%d: %w", event.PostID, err)
		}
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
