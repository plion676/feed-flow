package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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
	ListByIDs(ctx context.Context, postIDs []int64) ([]*model.Post, error)
}

type feedInboxRepository interface {
	ListPostIDsByCursor(ctx context.Context, userID int64, lastPostID int64, limit int) ([]int64, error)
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
	inboxRepo  feedInboxRepository
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

// WithInbox wires an optional inbox backend for hybrid feed reads.
func (s *FeedService) WithInbox(inboxRepo feedInboxRepository) *FeedService {
	s.inboxRepo = inboxRepo
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

	pullPosts, err := s.getHomeFeedByPullPosts(ctx, req.UserID, req.LastPostID, limit+1)
	var inboxPosts []*model.Post
	inboxHit := false
	if maybeInboxPosts, ok := s.getHomeFeedFromInboxPosts(ctx, req.UserID, req.LastPostID, limit+1); ok {
		inboxPosts = maybeInboxPosts
		inboxHit = true
	}

	if err != nil {
		if !inboxHit {
			return nil, xerror.ErrInternal
		}
		result := buildFeedResult(inboxPosts, limit, len(inboxPosts) > limit)
		s.setFeedCache(ctx, cacheKey, result)
		return result, nil
	}

	var result *FeedResult
	if inboxHit {
		mergedPosts := mergeFeedPosts(inboxPosts, pullPosts)
		result = buildFeedResult(mergedPosts, limit, len(mergedPosts) > limit)
	} else {
		result = buildFeedResult(pullPosts, limit, len(pullPosts) > limit)
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

func (s *FeedService) getHomeFeedFromInboxPosts(
	ctx context.Context,
	userID int64,
	lastPostID int64,
	limit int,
) ([]*model.Post, bool) {
	if s.inboxRepo == nil {
		return nil, false
	}
	if limit <= 0 {
		return nil, false
	}

	cursor := lastPostID
	collected := make([]*model.Post, 0, limit)
	seen := make(map[int64]struct{}, limit)

	for len(collected) < limit {
		postIDs, err := s.inboxRepo.ListPostIDsByCursor(ctx, userID, cursor, limit)
		if err != nil {
			return nil, false
		}
		if len(postIDs) == 0 {
			break
		}

		posts, err := s.postRepo.ListByIDs(ctx, postIDs)
		if err != nil {
			return nil, false
		}
		for _, post := range posts {
			if post == nil || post.ID <= 0 {
				continue
			}
			if _, ok := seen[post.ID]; ok {
				continue
			}
			seen[post.ID] = struct{}{}
			collected = append(collected, post)
			if len(collected) >= limit {
				break
			}
		}

		nextCursor, ok := minPostID(postIDs)
		if !ok {
			break
		}
		if cursor > 0 && nextCursor >= cursor {
			break
		}
		cursor = nextCursor
	}

	if len(collected) == 0 {
		return nil, false
	}
	return collected, true
}

func (s *FeedService) getHomeFeedByPullPosts(
	ctx context.Context,
	userID int64,
	lastPostID int64,
	limit int,
) ([]*model.Post, error) {
	followingUserIDs, err := s.followRepo.ListFollowingUserIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	followingUserIDs = append(followingUserIDs, userID)

	posts, err := s.postRepo.ListByUserIDs(ctx, followingUserIDs, lastPostID, limit)
	if err != nil {
		return nil, err
	}

	return posts, nil
}

func mergeFeedPosts(sources ...[]*model.Post) []*model.Post {
	if len(sources) == 0 {
		return []*model.Post{}
	}

	mergedByID := make(map[int64]*model.Post)
	for _, posts := range sources {
		for _, post := range posts {
			if post == nil || post.ID <= 0 {
				continue
			}
			mergedByID[post.ID] = post
		}
	}

	merged := make([]*model.Post, 0, len(mergedByID))
	for _, post := range mergedByID {
		merged = append(merged, post)
	}

	sort.Slice(merged, func(i, j int) bool {
		return merged[i].ID > merged[j].ID
	})
	return merged
}

func minPostID(postIDs []int64) (int64, bool) {
	if len(postIDs) == 0 {
		return 0, false
	}
	minID := postIDs[0]
	for _, id := range postIDs[1:] {
		if id < minID {
			minID = id
		}
	}
	return minID, true
}

func buildFeedResult(posts []*model.Post, limit int, hasMore bool) *FeedResult {
	if hasMore && len(posts) > limit {
		posts = posts[:limit]
	}

	items := make([]FeedItem, 0, len(posts))
	for _, post := range posts {
		if post == nil {
			continue
		}
		items = append(items, FeedItem{
			PostID:    post.ID,
			UserID:    post.UserID,
			Content:   post.Content,
			CreatedAt: post.CreatedAt,
		})
	}

	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].PostID
	}

	return &FeedResult{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}
}
