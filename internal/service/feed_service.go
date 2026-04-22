package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

const (
	defaultFeedLimit = 20
	maxFeedLimit     = 50
	feedCacheTTL     = 30 * time.Second
)

type feedFollowRepository interface {
	ListFollowingUserIDs(ctx context.Context, userID int64) ([]int64, error)
}

type feedPostRepository interface {
	ListByUserIDs(ctx context.Context, userIDs []int64, lastPostID int64, limit int) ([]*model.Post, error)
}

type feedCacheRepository interface {
	Get(ctx context.Context, key string) (value string, hit bool, err error)
	Set(ctx context.Context, key string, value string, ttl time.Duration) error
}

// FeedService handles timeline read workflows.
type FeedService struct {
	followRepo feedFollowRepository
	postRepo   feedPostRepository
	cacheRepo  feedCacheRepository
}

type GetFeedRequest struct {
	UserID     int64
	LastPostID int64
	Limit      int
}

type FeedItem struct {
	PostID    int64     `json:"post_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type FeedResult struct {
	Items      []FeedItem `json:"items"`
	NextCursor int64      `json:"next_cursor"`
	HasMore    bool       `json:"has_more"`
}

func NewFeedService(followRepo feedFollowRepository, postRepo feedPostRepository) *FeedService {
	return &FeedService{
		followRepo: followRepo,
		postRepo:   postRepo,
	}
}

// WithCache wires an optional cache backend for feed reads.
func (s *FeedService) WithCache(cacheRepo feedCacheRepository) *FeedService {
	s.cacheRepo = cacheRepo
	return s
}

func (s *FeedService) GetHomeFeed(ctx context.Context, req GetFeedRequest) (*FeedResult, *xerror.Error) {
	if req.UserID <= 0 {
		return nil, xerror.ErrUnauthorized
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultFeedLimit
	}
	if limit > maxFeedLimit {
		limit = maxFeedLimit
	}

	cacheKey := buildFeedCacheKey(req.UserID, req.LastPostID, limit)
	if cached, ok := s.getFeedFromCache(ctx, cacheKey); ok {
		return cached, nil
	}

	followingUserIDs, err := s.followRepo.ListFollowingUserIDs(ctx, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	followingUserIDs = append(followingUserIDs, req.UserID)

	posts, err := s.postRepo.ListByUserIDs(ctx, followingUserIDs, req.LastPostID, limit+1)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}

	items := make([]FeedItem, len(posts))
	for i, post := range posts {
		items[i] = FeedItem{
			PostID:    post.ID,
			UserID:    post.UserID,
			Content:   post.Content,
			CreatedAt: post.CreatedAt,
		}
	}

	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].PostID
	}

	result := &FeedResult{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}

	s.setFeedCache(ctx, cacheKey, result)
	return result, nil
}

func buildFeedCacheKey(userID int64, lastPostID int64, limit int) string {
	return fmt.Sprintf("feed:home:%d:%d:%d", userID, lastPostID, limit)
}

func (s *FeedService) getFeedFromCache(ctx context.Context, cacheKey string) (*FeedResult, bool) {
	if s.cacheRepo == nil {
		return nil, false
	}

	cachedValue, hit, err := s.cacheRepo.Get(ctx, cacheKey)
	if err != nil || !hit {
		return nil, false
	}

	var result FeedResult
	if err := json.Unmarshal([]byte(cachedValue), &result); err != nil {
		return nil, false
	}

	return &result, true
}

func (s *FeedService) setFeedCache(ctx context.Context, cacheKey string, result *FeedResult) {
	if s.cacheRepo == nil || result == nil {
		return
	}

	cacheValue, err := json.Marshal(result)
	if err != nil {
		return
	}

	_ = s.cacheRepo.Set(ctx, cacheKey, string(cacheValue), feedCacheTTL)
}
