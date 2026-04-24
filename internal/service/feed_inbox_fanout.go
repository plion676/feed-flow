package service

import (
	"context"
	"fmt"
	"sync"
)

type feedInboxWriter interface {
	AddPostToInbox(ctx context.Context, userID int64, postID int64, occurredAt int64) error
	TrimInbox(ctx context.Context, userID int64, maxItems int64) error
}

type feedInboxBatchWriter interface {
	BatchAddPostToInboxes(ctx context.Context, userIDs []int64, postID int64, occurredAt int64, maxItems int64) error
}

// FeedInboxFanout performs push-lane delivery into follower inboxes.
type FeedInboxFanout struct {
	inboxRepo feedInboxWriter
	maxItems  int64
}

const (
	defaultInboxFanoutBatchSize = 200
	defaultInboxFanoutWorkers   = 8
)

func NewFeedInboxFanout(inboxRepo feedInboxWriter, maxItems int64) *FeedInboxFanout {
	return &FeedInboxFanout{
		inboxRepo: inboxRepo,
		maxItems:  maxItems,
	}
}

func (f *FeedInboxFanout) FanoutPostToFollowers(ctx context.Context, followerIDs []int64, postID int64, occurredAt int64) error {
	if f == nil || f.inboxRepo == nil {
		return fmt.Errorf("feed inbox fanout dependencies are not configured")
	}
	if postID <= 0 {
		return fmt.Errorf("post_id must be positive")
	}

	validFollowerIDs := make([]int64, 0, len(followerIDs))
	for _, followerID := range followerIDs {
		if followerID <= 0 {
			continue
		}
		validFollowerIDs = append(validFollowerIDs, followerID)
	}
	if len(validFollowerIDs) == 0 {
		return nil
	}

	// Fast path: use repository pipeline batching if supported.
	if batchWriter, ok := f.inboxRepo.(feedInboxBatchWriter); ok {
		return f.fanoutByBatch(ctx, batchWriter, validFollowerIDs, postID, occurredAt)
	}

	// Fallback path: single-user writes for compatibility.
	for _, followerID := range validFollowerIDs {
		if err := f.inboxRepo.AddPostToInbox(ctx, followerID, postID, occurredAt); err != nil {
			return fmt.Errorf("add post to inbox user_id=%d: %w", followerID, err)
		}
		if err := f.inboxRepo.TrimInbox(ctx, followerID, f.maxItems); err != nil {
			return fmt.Errorf("trim inbox user_id=%d: %w", followerID, err)
		}
	}

	return nil
}

func (f *FeedInboxFanout) fanoutByBatch(
	ctx context.Context,
	batchWriter feedInboxBatchWriter,
	followerIDs []int64,
	postID int64,
	occurredAt int64,
) error {
	chunks := splitInt64Slice(followerIDs, defaultInboxFanoutBatchSize)
	if len(chunks) == 0 {
		return nil
	}

	workerCount := defaultInboxFanoutWorkers
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}

	jobs := make(chan []int64)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup

	worker := func() {
		defer wg.Done()
		for chunk := range jobs {
			if err := batchWriter.BatchAddPostToInboxes(ctx, chunk, postID, occurredAt, f.maxItems); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

	var submitErr error
	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			submitErr = ctx.Err()
		case err := <-errCh:
			submitErr = err
		case jobs <- chunk:
		}
		if submitErr != nil {
			break
		}
	}

	close(jobs)
	wg.Wait()

	if submitErr != nil {
		return fmt.Errorf("batch fanout post_id=%d: %w", postID, submitErr)
	}

	select {
	case err := <-errCh:
		return fmt.Errorf("batch fanout post_id=%d: %w", postID, err)
	default:
		return nil
	}
}

func splitInt64Slice(values []int64, size int) [][]int64 {
	if size <= 0 || len(values) == 0 {
		return nil
	}

	result := make([][]int64, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		result = append(result, values[start:end])
	}
	return result
}
