package service

import (
	"context"
	"fmt"

	"github.com/plion676/feed-flow/internal/model"
)

type feedInboxAuthorCleanupInboxRepository interface {
	ListPostIDsByCursor(ctx context.Context, userID int64, lastPostID int64, limit int) ([]int64, error)
	RemovePostsFromInbox(ctx context.Context, userID int64, postIDs []int64) error
}

type feedInboxAuthorCleanupPostRepository interface {
	ListByIDs(ctx context.Context, postIDs []int64) ([]*model.Post, error)
}

// FeedInboxAuthorCleanup removes a specific author's historical posts from one user's inbox.
type FeedInboxAuthorCleanup struct {
	inboxRepo feedInboxAuthorCleanupInboxRepository
	postRepo  feedInboxAuthorCleanupPostRepository
	scanLimit int
	scanBatch int
}

const (
	defaultInboxAuthorCleanupScanLimit = 1000
	defaultInboxAuthorCleanupScanBatch = 200
)

func NewFeedInboxAuthorCleanup(
	inboxRepo feedInboxAuthorCleanupInboxRepository,
	postRepo feedInboxAuthorCleanupPostRepository,
) *FeedInboxAuthorCleanup {
	return &FeedInboxAuthorCleanup{
		inboxRepo: inboxRepo,
		postRepo:  postRepo,
		scanLimit: defaultInboxAuthorCleanupScanLimit,
		scanBatch: defaultInboxAuthorCleanupScanBatch,
	}
}

func (c *FeedInboxAuthorCleanup) WithScanOptions(scanLimit int, scanBatch int) *FeedInboxAuthorCleanup {
	if c == nil {
		return c
	}
	if scanLimit > 0 {
		c.scanLimit = scanLimit
	}
	if scanBatch > 0 {
		c.scanBatch = scanBatch
	}
	return c
}

func (c *FeedInboxAuthorCleanup) RemoveAuthorPostsFromInbox(ctx context.Context, userID int64, authorUserID int64) error {
	if c == nil || c.inboxRepo == nil || c.postRepo == nil {
		return fmt.Errorf("feed inbox author cleanup dependencies are not configured")
	}
	if userID <= 0 || authorUserID <= 0 {
		return fmt.Errorf("user_id and author_user_id must be positive")
	}

	scanLimit := c.scanLimit
	if scanLimit <= 0 {
		scanLimit = defaultInboxAuthorCleanupScanLimit
	}
	scanBatch := c.scanBatch
	if scanBatch <= 0 {
		scanBatch = defaultInboxAuthorCleanupScanBatch
	}

	postIDs, err := c.inboxRepo.ListPostIDsByCursor(ctx, userID, 0, scanLimit)
	if err != nil {
		return fmt.Errorf("list inbox post ids user_id=%d: %w", userID, err)
	}
	if len(postIDs) == 0 {
		return nil
	}

	targetPostIDs := make([]int64, 0)
	for start := 0; start < len(postIDs); start += scanBatch {
		end := start + scanBatch
		if end > len(postIDs) {
			end = len(postIDs)
		}

		posts, err := c.postRepo.ListByIDs(ctx, postIDs[start:end])
		if err != nil {
			return fmt.Errorf("list posts by ids for inbox cleanup user_id=%d author_id=%d: %w", userID, authorUserID, err)
		}
		for _, post := range posts {
			if post == nil || post.ID <= 0 {
				continue
			}
			if post.UserID != authorUserID {
				continue
			}
			targetPostIDs = append(targetPostIDs, post.ID)
		}
	}

	if len(targetPostIDs) == 0 {
		return nil
	}

	if err := c.inboxRepo.RemovePostsFromInbox(ctx, userID, targetPostIDs); err != nil {
		return fmt.Errorf("remove author posts from inbox user_id=%d author_id=%d: %w", userID, authorUserID, err)
	}
	return nil
}
