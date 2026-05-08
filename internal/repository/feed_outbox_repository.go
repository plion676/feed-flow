package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const outboxCursorScanBatchSize int64 = 200

// FeedOutboxRepository stores author-side pull indexes in Redis ZSET.
// Key: feed:outbox:{author_id}
// Member: post_id string
// Score: post_id
type FeedOutboxRepository struct {
	client *redis.Client
}

func NewFeedOutboxRepository(client *redis.Client) *FeedOutboxRepository {
	return &FeedOutboxRepository{client: client}
}

func (r *FeedOutboxRepository) AddPostToOutbox(ctx context.Context, authorUserID int64, postID int64) error {
	if authorUserID <= 0 || postID <= 0 {
		return fmt.Errorf("author_user_id and post_id must be positive")
	}

	return r.client.ZAdd(ctx, buildFeedOutboxKey(authorUserID), redis.Z{
		Score:  float64(postID),
		Member: strconv.FormatInt(postID, 10),
	}).Err()
}

func (r *FeedOutboxRepository) RemovePostFromOutbox(ctx context.Context, authorUserID int64, postID int64) error {
	if authorUserID <= 0 || postID <= 0 {
		return fmt.Errorf("author_user_id and post_id must be positive")
	}

	return r.client.ZRem(ctx, buildFeedOutboxKey(authorUserID), strconv.FormatInt(postID, 10)).Err()
}

func (r *FeedOutboxRepository) TrimOutbox(ctx context.Context, authorUserID int64, maxItems int64) error {
	if authorUserID <= 0 {
		return fmt.Errorf("author_user_id must be positive")
	}
	if maxItems <= 0 {
		return nil
	}

	return r.client.ZRemRangeByRank(ctx, buildFeedOutboxKey(authorUserID), 0, -maxItems-1).Err()
}

func (r *FeedOutboxRepository) ListPostIDsByCursor(
	ctx context.Context,
	authorUserID int64,
	maxPostID int64,
	limit int,
) ([]int64, error) {
	if authorUserID <= 0 {
		return nil, fmt.Errorf("author_user_id must be positive")
	}
	if limit <= 0 {
		return []int64{}, nil
	}

	results := make([]int64, 0, limit)
	var start int64 = 0

	for len(results) < limit {
		stop := start + outboxCursorScanBatchSize - 1
		members, err := r.client.ZRevRange(ctx, buildFeedOutboxKey(authorUserID), start, stop).Result()
		if err != nil {
			return nil, err
		}
		if len(members) == 0 {
			break
		}

		for _, member := range members {
			postID, err := strconv.ParseInt(member, 10, 64)
			if err != nil || postID <= 0 {
				continue
			}
			if maxPostID > 0 && postID >= maxPostID {
				continue
			}

			results = append(results, postID)
			if len(results) >= limit {
				break
			}
		}

		start += int64(len(members))
		if len(members) < int(outboxCursorScanBatchSize) {
			break
		}
	}

	return results, nil
}

func buildFeedOutboxKey(authorUserID int64) string {
	return fmt.Sprintf("feed:outbox:%d", authorUserID)
}
