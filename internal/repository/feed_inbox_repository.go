package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// FeedInboxRepository stores push-lane inbox items in Redis ZSET.
// Key: feed:inbox:{user_id}
// Score: occurred_at unix seconds (newer is larger)
// Member: post_id string, so repeated fanout of the same post is naturally idempotent in ZSET.
type FeedInboxRepository struct {
	client *redis.Client
}

const inboxCursorScanBatchSize int64 = 200

func NewFeedInboxRepository(client *redis.Client) *FeedInboxRepository {
	return &FeedInboxRepository{client: client}
}

func (r *FeedInboxRepository) AddPostToInbox(ctx context.Context, userID int64, postID int64, occurredAt int64) error {
	if userID <= 0 || postID <= 0 {
		return fmt.Errorf("user_id and post_id must be positive")
	}
	if occurredAt <= 0 {
		occurredAt = time.Now().Unix()
	}

	return r.client.ZAdd(ctx, buildFeedInboxKey(userID), redis.Z{
		Score:  float64(occurredAt),
		Member: strconv.FormatInt(postID, 10),
	}).Err()
}

func (r *FeedInboxRepository) TrimInbox(ctx context.Context, userID int64, maxItems int64) error {
	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}
	if maxItems <= 0 {
		return nil
	}

	// Remove oldest overflow entries and keep only latest maxItems.
	return r.client.ZRemRangeByRank(ctx, buildFeedInboxKey(userID), 0, -maxItems-1).Err()
}

func (r *FeedInboxRepository) BatchAddPostToInboxes(
	ctx context.Context,
	userIDs []int64,
	postID int64,
	occurredAt int64,
	maxItems int64,
) error {
	if postID <= 0 {
		return fmt.Errorf("post_id must be positive")
	}
	if occurredAt <= 0 {
		occurredAt = time.Now().Unix()
	}

	member := strconv.FormatInt(postID, 10)
	_, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, userID := range userIDs {
			if userID <= 0 {
				continue
			}
			key := buildFeedInboxKey(userID)
			pipe.ZAdd(ctx, key, redis.Z{
				Score:  float64(occurredAt),
				Member: member,
			})
			if maxItems > 0 {
				pipe.ZRemRangeByRank(ctx, key, 0, -maxItems-1)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("pipeline fanout write failed: %w", err)
	}
	return nil
}

func (r *FeedInboxRepository) ListPostIDsByCursor(
	ctx context.Context,
	userID int64,
	lastPostID int64,
	limit int,
) ([]int64, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user_id must be positive")
	}
	if limit <= 0 {
		return []int64{}, nil
	}

	results := make([]int64, 0, limit)
	var start int64 = 0

	for len(results) < limit {
		stop := start + inboxCursorScanBatchSize - 1
		members, err := r.client.ZRevRange(ctx, buildFeedInboxKey(userID), start, stop).Result()
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
			if lastPostID > 0 && postID >= lastPostID {
				continue
			}

			results = append(results, postID)
			if len(results) >= limit {
				break
			}
		}

		start += int64(len(members))
		if len(members) < int(inboxCursorScanBatchSize) {
			break
		}
	}

	return results, nil
}

func buildFeedInboxKey(userID int64) string {
	return fmt.Sprintf("feed:inbox:%d", userID)
}
