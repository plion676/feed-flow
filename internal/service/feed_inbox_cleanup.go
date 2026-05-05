package service

import (
	"context"
	"fmt"
	"time"
)

type feedInboxCleanupWriter interface {
	RemovePostFromInbox(ctx context.Context, userID int64, postID int64) error
}

type feedInboxBatchCleanupWriter interface {
	BatchRemovePostFromInboxes(ctx context.Context, userIDs []int64, postID int64) error
}

// FeedInboxCleanup removes invalidated posts from follower inboxes.
type FeedInboxCleanup struct {
	inboxRepo feedInboxCleanupWriter
	batchSize int
	workers   int
	logger    feedInboxFanoutLogger
}

func NewFeedInboxCleanup(inboxRepo feedInboxCleanupWriter) *FeedInboxCleanup {
	return &FeedInboxCleanup{
		inboxRepo: inboxRepo,
		batchSize: defaultInboxFanoutBatchSize,
		workers:   defaultInboxFanoutWorkers,
		logger:    nil,
	}
}

func (c *FeedInboxCleanup) WithBatchOptions(batchSize int, workers int) *FeedInboxCleanup {
	if c == nil {
		return c
	}
	if batchSize > 0 {
		c.batchSize = batchSize
	}
	if workers > 0 {
		c.workers = workers
	}
	return c
}

func (c *FeedInboxCleanup) WithLogger(logger feedInboxFanoutLogger) *FeedInboxCleanup {
	if c == nil {
		return c
	}
	if logger != nil {
		c.logger = logger
	}
	return c
}

func (c *FeedInboxCleanup) RemovePostFromFollowers(ctx context.Context, followerIDs []int64, postID int64) error {
	if c == nil || c.inboxRepo == nil {
		return fmt.Errorf("feed inbox cleanup dependencies are not configured")
	}
	if postID <= 0 {
		return fmt.Errorf("post_id must be positive")
	}

	validFollowerIDs := normalizeFollowerIDs(followerIDs)
	if len(validFollowerIDs) == 0 {
		return nil
	}
	startedAt := time.Now()

	if batchWriter, ok := c.inboxRepo.(feedInboxBatchCleanupWriter); ok {
		return c.cleanupByBatch(ctx, batchWriter, validFollowerIDs, postID, startedAt)
	}

	c.logEventf(ctx, "feed inbox cleanup start mode=single follower_count=%d", len(validFollowerIDs))
	for _, followerID := range validFollowerIDs {
		if err := c.inboxRepo.RemovePostFromInbox(ctx, followerID, postID); err != nil {
			c.logEventf(ctx, "feed inbox cleanup failed mode=single follower_id=%d err=%v", followerID, err)
			return fmt.Errorf("remove post from inbox user_id=%d: %w", followerID, err)
		}
	}
	c.logEventf(
		ctx,
		"feed inbox cleanup done mode=single follower_count=%d elapsed=%s",
		len(validFollowerIDs),
		time.Since(startedAt),
	)
	return nil
}

func (c *FeedInboxCleanup) cleanupByBatch(
	ctx context.Context,
	batchWriter feedInboxBatchCleanupWriter,
	followerIDs []int64,
	postID int64,
	startedAt time.Time,
) error {
	batchSize := c.batchSize
	if batchSize <= 0 {
		batchSize = defaultInboxFanoutBatchSize
	}
	chunks := splitInt64Slice(followerIDs, batchSize)
	if len(chunks) == 0 {
		return nil
	}

	workerCount := c.workers
	if workerCount <= 0 {
		workerCount = defaultInboxFanoutWorkers
	}
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}
	c.logEventf(
		ctx,
		"feed inbox cleanup start mode=batch follower_count=%d chunk_count=%d batch_size=%d workers=%d",
		len(followerIDs),
		len(chunks),
		batchSize,
		workerCount,
	)

	jobs := make(chan []int64)
	errCh := make(chan error, 1)
	doneBatchesCh := make(chan int, workerCount)

	worker := func() {
		doneBatches := 0
		defer func() {
			doneBatchesCh <- doneBatches
		}()
		for chunk := range jobs {
			if err := batchWriter.BatchRemovePostFromInboxes(ctx, chunk, postID); err != nil {
				c.logEventf(
					ctx,
					"feed inbox cleanup failed mode=batch chunk_size=%d err=%v",
					len(chunk),
					err,
				)
				select {
				case errCh <- err:
				default:
				}
				return
			}
			doneBatches++
		}
	}

	for i := 0; i < workerCount; i++ {
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

	successBatches := 0
	for i := 0; i < workerCount; i++ {
		successBatches += <-doneBatchesCh
	}

	if submitErr != nil {
		c.logEventf(
			ctx,
			"feed inbox cleanup done mode=batch success_batches=%d chunk_count=%d elapsed=%s err=%v",
			successBatches,
			len(chunks),
			time.Since(startedAt),
			submitErr,
		)
		return fmt.Errorf("batch cleanup post_id=%d: %w", postID, submitErr)
	}

	select {
	case err := <-errCh:
		c.logEventf(
			ctx,
			"feed inbox cleanup done mode=batch success_batches=%d chunk_count=%d elapsed=%s err=%v",
			successBatches,
			len(chunks),
			time.Since(startedAt),
			err,
		)
		return fmt.Errorf("batch cleanup post_id=%d: %w", postID, err)
	default:
		c.logEventf(
			ctx,
			"feed inbox cleanup done mode=batch success_batches=%d chunk_count=%d elapsed=%s",
			successBatches,
			len(chunks),
			time.Since(startedAt),
		)
		return nil
	}
}

func (c *FeedInboxCleanup) logf(format string, v ...any) {
	if c == nil || c.logger == nil {
		return
	}
	c.logger.Printf(format, v...)
}

func (c *FeedInboxCleanup) logEventf(ctx context.Context, format string, v ...any) {
	fields := getFeedEventLogFields(ctx)
	prefix := "stream_id=%s author_id=%d post_id=%d "
	args := []any{fields.StreamID, fields.AuthorUserID, fields.PostID}
	args = append(args, v...)
	c.logf(prefix+format, args...)
}
