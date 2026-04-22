package repository

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// FeedCacheRepository is the Redis-backed cache store for home feed payloads.
type FeedCacheRepository struct {
	client *redis.Client
}

func NewFeedCacheRepository(client *redis.Client) *FeedCacheRepository {
	return &FeedCacheRepository{client: client}
}

func (r *FeedCacheRepository) Get(ctx context.Context, key string) (value string, hit bool, err error) {
	value, err = r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (r *FeedCacheRepository) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}
