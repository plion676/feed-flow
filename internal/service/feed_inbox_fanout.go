package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
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
	batchSize int
	workers   int
	logger    feedInboxFanoutLogger
}

type feedInboxFanoutLogger interface {
	Printf(format string, v ...any)
}

const (
	defaultInboxFanoutBatchSize = 200
	defaultInboxFanoutWorkers   = 8
)

func NewFeedInboxFanout(inboxRepo feedInboxWriter, maxItems int64) *FeedInboxFanout {
	return &FeedInboxFanout{
		inboxRepo: inboxRepo,
		maxItems:  maxItems,
		batchSize: defaultInboxFanoutBatchSize,
		workers:   defaultInboxFanoutWorkers,
		logger:    log.Default(),
	}
}

func (f *FeedInboxFanout) WithBatchOptions(batchSize int, workers int) *FeedInboxFanout {
	if f == nil {
		return f
	}
	if batchSize > 0 {
		f.batchSize = batchSize
	}
	if workers > 0 {
		f.workers = workers
	}
	return f
}

func (f *FeedInboxFanout) WithLogger(logger feedInboxFanoutLogger) *FeedInboxFanout {
	if f == nil {
		return f
	}
	if logger != nil {
		f.logger = logger
	}
	return f
}

func (f *FeedInboxFanout) FanoutPostToFollowers(ctx context.Context, followerIDs []int64, postID int64, occurredAt int64) error {
	if f == nil || f.inboxRepo == nil {
		return fmt.Errorf("feed inbox fanout dependencies are not configured")
	}
	if postID <= 0 {
		return fmt.Errorf("post_id must be positive")
	}

	validFollowerIDs := normalizeFollowerIDs(followerIDs)
	if len(validFollowerIDs) == 0 {
		return nil
	}
	startedAt := time.Now()

	// Fast path: use repository pipeline batching if supported.
	if batchWriter, ok := f.inboxRepo.(feedInboxBatchWriter); ok {
		return f.fanoutByBatch(ctx, batchWriter, validFollowerIDs, postID, occurredAt, startedAt)
	}

	// Fallback path: single-user writes for compatibility.
	f.logEventf(ctx, "feed inbox fanout start mode=single follower_count=%d", len(validFollowerIDs))
	for _, followerID := range validFollowerIDs {
		if err := f.inboxRepo.AddPostToInbox(ctx, followerID, postID, occurredAt); err != nil {
			f.logEventf(ctx, "feed inbox fanout failed mode=single follower_id=%d err=%v", followerID, err)
			return fmt.Errorf("add post to inbox user_id=%d: %w", followerID, err)
		}
		if err := f.inboxRepo.TrimInbox(ctx, followerID, f.maxItems); err != nil {
			f.logEventf(ctx, "feed inbox fanout failed mode=single follower_id=%d err=%v", followerID, err)
			return fmt.Errorf("trim inbox user_id=%d: %w", followerID, err)
		}
	}
	f.logEventf(
		ctx,
		"feed inbox fanout done mode=single follower_count=%d elapsed=%s",
		len(validFollowerIDs),
		time.Since(startedAt),
	)

	return nil
}

func (f *FeedInboxFanout) fanoutByBatch(
	ctx context.Context,
	batchWriter feedInboxBatchWriter,
	followerIDs []int64,
	postID int64,
	occurredAt int64,
	startedAt time.Time,
) error {
	batchSize := f.batchSize
	if batchSize <= 0 {
		batchSize = defaultInboxFanoutBatchSize
	}
	chunks := splitInt64Slice(followerIDs, batchSize)
	if len(chunks) == 0 {
		return nil
	}

	workerCount := f.workers
	if workerCount <= 0 {
		workerCount = defaultInboxFanoutWorkers
	}
	if workerCount > len(chunks) {
		workerCount = len(chunks)
	}
	f.logEventf(
		ctx,
		"feed inbox fanout start mode=batch follower_count=%d chunk_count=%d batch_size=%d workers=%d",
		len(followerIDs),
		len(chunks),
		batchSize,
		workerCount,
	)

	jobs := make(chan []int64)
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	successBatches := 0
	var successMu sync.Mutex

	worker := func() {
		defer wg.Done()
		for chunk := range jobs {
			if err := batchWriter.BatchAddPostToInboxes(ctx, chunk, postID, occurredAt, f.maxItems); err != nil {
				f.logEventf(
					ctx,
					"feed inbox fanout failed mode=batch chunk_size=%d err=%v",
					len(chunk),
					err,
				)
				select {
				case errCh <- err:
				default:
				}
				return
			}
			successMu.Lock()
			successBatches++
			successMu.Unlock()
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
		successMu.Lock()
		doneBatches := successBatches
		successMu.Unlock()
		f.logEventf(
			ctx,
			"feed inbox fanout done mode=batch success_batches=%d chunk_count=%d elapsed=%s err=%v",
			doneBatches,
			len(chunks),
			time.Since(startedAt),
			submitErr,
		)
		return fmt.Errorf("batch fanout post_id=%d: %w", postID, submitErr)
	}

	select {
	case err := <-errCh:
		successMu.Lock()
		doneBatches := successBatches
		successMu.Unlock()
		f.logEventf(
			ctx,
			"feed inbox fanout done mode=batch success_batches=%d chunk_count=%d elapsed=%s err=%v",
			doneBatches,
			len(chunks),
			time.Since(startedAt),
			err,
		)
		return fmt.Errorf("batch fanout post_id=%d: %w", postID, err)
	default:
		successMu.Lock()
		doneBatches := successBatches
		successMu.Unlock()
		f.logEventf(
			ctx,
			"feed inbox fanout done mode=batch success_batches=%d chunk_count=%d elapsed=%s",
			doneBatches,
			len(chunks),
			time.Since(startedAt),
		)
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

func normalizeFollowerIDs(followerIDs []int64) []int64 {
	if len(followerIDs) == 0 {
		return nil
	}

	result := make([]int64, 0, len(followerIDs))
	seen := make(map[int64]struct{}, len(followerIDs))
	for _, followerID := range followerIDs {
		if followerID <= 0 {
			continue
		}
		if _, ok := seen[followerID]; ok {
			continue
		}
		seen[followerID] = struct{}{}
		result = append(result, followerID)
	}
	return result
}

func (f *FeedInboxFanout) logf(format string, v ...any) {
	if f == nil || f.logger == nil {
		return
	}
	f.logger.Printf(format, v...)
}

func (f *FeedInboxFanout) logEventf(ctx context.Context, format string, v ...any) {
	fields := getFeedEventLogFields(ctx)
	prefix := "stream_id=%s author_id=%d post_id=%d "
	args := []any{fields.StreamID, fields.AuthorUserID, fields.PostID}
	args = append(args, v...)
	f.logf(prefix+format, args...)
}
