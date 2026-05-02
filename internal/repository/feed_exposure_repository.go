package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const defaultFeedExposureKeyTTL = 48 * time.Hour

type FeedExposureRepositoryOptions struct {
	KeyTTL time.Duration
}

// FeedExposureRepository stores per-user exposure history in Redis ZSET.
// Key: feed:exposure:{user_id}
// Member: post_id string
// Score: seen_at unix seconds
type FeedExposureRepository struct {
	client *redis.Client
	keyTTL time.Duration
}

func NewFeedExposureRepository(client *redis.Client, options FeedExposureRepositoryOptions) *FeedExposureRepository {
	keyTTL := options.KeyTTL
	if keyTTL <= 0 {
		keyTTL = defaultFeedExposureKeyTTL
	}
	return &FeedExposureRepository{
		client: client,
		keyTTL: keyTTL,
	}
}

func (r *FeedExposureRepository) FilterUnseenPostIDs(
	ctx context.Context,
	userID int64,
	postIDs []int64,
	window time.Duration,
) ([]int64, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user_id must be positive")
	}
	if len(postIDs) == 0 {
		return []int64{}, nil
	}
	if window <= 0 {
		return nil, fmt.Errorf("window must be positive")
	}

	now := time.Now().Unix()
	cutoff := now - int64(window/time.Second)
	if err := r.client.ZRemRangeByScore(ctx, buildFeedExposureKey(userID), "-inf", strconv.FormatInt(cutoff, 10)).Err(); err != nil {
		return nil, fmt.Errorf("trim exposure window user_id=%d: %w", userID, err)
	}

	postIDMembers := make([]string, 0, len(postIDs))
	seen := make(map[int64]struct{}, len(postIDs))
	for _, postID := range postIDs {
		if postID <= 0 {
			continue
		}
		if _, ok := seen[postID]; ok {
			continue
		}
		seen[postID] = struct{}{}
		postIDMembers = append(postIDMembers, strconv.FormatInt(postID, 10))
	}
	if len(postIDMembers) == 0 {
		return []int64{}, nil
	}

	key := buildFeedExposureKey(userID)
	scoreCmds := make([]*redis.FloatCmd, 0, len(postIDMembers))
	_, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, member := range postIDMembers {
			scoreCmds = append(scoreCmds, pipe.ZScore(ctx, key, member))
		}
		return nil
	})
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("zscore exposure window user_id=%d: %w", userID, err)
	}

	unseen := make([]int64, 0, len(postIDMembers))
	for i, cmd := range scoreCmds {
		if cmd.Err() == nil {
			continue
		}
		if cmd.Err() != redis.Nil {
			return nil, fmt.Errorf("zscore exposure window user_id=%d member=%s: %w", userID, postIDMembers[i], cmd.Err())
		}
		postID, err := strconv.ParseInt(postIDMembers[i], 10, 64)
		if err != nil || postID <= 0 {
			continue
		}
		unseen = append(unseen, postID)
	}

	return unseen, nil
}

func (r *FeedExposureRepository) MarkSeenPostIDs(
	ctx context.Context,
	userID int64,
	postIDs []int64,
	window time.Duration,
) error {
	if userID <= 0 {
		return fmt.Errorf("user_id must be positive")
	}
	if len(postIDs) == 0 {
		return nil
	}
	if window <= 0 {
		return fmt.Errorf("window must be positive")
	}

	now := time.Now().Unix()
	cutoff := now - int64(window/time.Second)
	members := make([]redis.Z, 0, len(postIDs))
	seen := make(map[int64]struct{}, len(postIDs))
	for _, postID := range postIDs {
		if postID <= 0 {
			continue
		}
		if _, ok := seen[postID]; ok {
			continue
		}
		seen[postID] = struct{}{}
		members = append(members, redis.Z{
			Score:  float64(now),
			Member: strconv.FormatInt(postID, 10),
		})
	}
	if len(members) == 0 {
		return nil
	}

	key := buildFeedExposureKey(userID)
	_, err := r.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.ZAdd(ctx, key, members...)
		pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(cutoff, 10))
		pipe.Expire(ctx, key, r.keyTTL)
		return nil
	})
	if err != nil {
		return fmt.Errorf("mark seen exposure window user_id=%d: %w", userID, err)
	}
	return nil
}

func buildFeedExposureKey(userID int64) string {
	return fmt.Sprintf("feed:exposure:%d", userID)
}
