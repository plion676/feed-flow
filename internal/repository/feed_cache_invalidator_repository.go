package repository

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// FeedCacheInvalidatorRepository encapsulates feed cache invalidation operations.
type FeedCacheInvalidatorRepository struct {
	client *redis.Client
}

func NewFeedCacheInvalidatorRepository(client *redis.Client) *FeedCacheInvalidatorRepository {
	return &FeedCacheInvalidatorRepository{client: client}
}

func (r *FeedCacheInvalidatorRepository) InvalidateHomeFeed(ctx context.Context, userID int64) error {
	pattern := fmt.Sprintf("feed:home:%d:*", userID)
	var cursor uint64 = 0
	for {
		keys, nextcursor, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			if err := r.client.Unlink(ctx, keys...).Err(); err != nil {
				return err
			}
		}

		cursor = nextcursor
		if cursor == 0 {
			break
		}
	}
	return nil
}
