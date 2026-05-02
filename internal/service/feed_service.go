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
	followRepo              feedFollowRepository
	postRepo                feedPostRepository
	cacheRepo               feedCacheRepository
	inboxRepo               feedInboxRepository
	exposureRepo            feedExposureRepository
	exposureWindowTTL       time.Duration
	exposureBatchMultiplier int
}

type GetFeedRequest struct {
	UserID     int64
	LastPostID int64
	Cursor     string
	Limit      int
}

type FeedItem struct {
	PostID    int64     `json:"post_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type FeedResult struct {
	Items           []FeedItem `json:"items"`
	NextCursor      int64      `json:"next_cursor"`
	NextCursorToken string     `json:"next_cursor_token,omitempty"`
	HasMore         bool       `json:"has_more"`
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

// WithExposure wires an optional exposure-window backend for feed reads.
func (s *FeedService) WithExposure(exposureRepo feedExposureRepository, options FeedExposureOptions) *FeedService {
	s.exposureRepo = exposureRepo
	s.exposureWindowTTL = options.WindowTTL
	s.exposureBatchMultiplier = options.BatchMultiplier
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

	readCursor, err := resolveFeedReadCursor(req)
	if err != nil {
		return nil, xerror.ErrBadRequest
	}

	cacheKey := buildFeedCacheKey(req.UserID, req.LastPostID, req.Cursor, limit)
	if cached, ok := s.getFeedFromCache(ctx, cacheKey); ok {
		return cached, nil
	}

	var (
		pullCandidates  feedExposureCandidates
		inboxCandidates feedExposureCandidates
		inboxPosts      []*model.Post
		inboxHit        bool
	)

	if s.exposureRepo != nil {
		batchLimit := resolveFeedExposureBatchLimit(limit, s.exposureBatchMultiplier)
		pullCandidates, err = s.collectPullExposureCandidates(
			ctx,
			req.UserID,
			readCursor.PullLastPostID,
			limit+1,
			batchLimit,
			readCursor.PullPendingIDs,
		)
		inboxCandidates, inboxErr := s.collectInboxExposureCandidates(
			ctx,
			req.UserID,
			readCursor.InboxLastPostID,
			limit+1,
			batchLimit,
			readCursor.InboxPendingIDs,
		)
		if inboxErr != nil {
			inboxCandidates = feedExposureCandidates{}
		}
		inboxPosts = inboxCandidates.posts
		inboxHit = inboxCandidates.hit
	} else {
		pullPosts, pullErr := s.getHomeFeedByPullPostsWithPending(
			ctx,
			req.UserID,
			readCursor.PullLastPostID,
			limit+1,
			readCursor.PullPendingIDs,
		)
		err = pullErr
		pullCandidates = feedExposureCandidates{
			posts:      pullPosts,
			nextCursor: readCursor.PullLastPostID,
			hasMore:    len(pullPosts) > limit,
			hit:        len(pullPosts) > 0,
		}
		if maybeInboxPosts, ok := s.getHomeFeedFromInboxPostsWithPending(
			ctx,
			req.UserID,
			readCursor.InboxLastPostID,
			limit+1,
			readCursor.InboxPendingIDs,
		); ok {
			inboxPosts = maybeInboxPosts
			inboxHit = true
			inboxCandidates = feedExposureCandidates{
				posts:      maybeInboxPosts,
				nextCursor: readCursor.InboxLastPostID,
				hasMore:    len(maybeInboxPosts) > limit,
				hit:        true,
			}
		}
	}
	useHybridCursor := req.Cursor != "" || inboxHit

	if err != nil {
		if !inboxHit {
			return nil, xerror.ErrInternal
		}
		if useHybridCursor {
			page := mixFeedPageForCursor(inboxPosts, nil, limit, readCursor.InboxLastPostID, readCursor.PullLastPostID)
			result := buildFeedResultWithHybridCursor(page)
			s.setFeedCache(ctx, cacheKey, result)
			return result, nil
		}
		result := buildFeedResult(inboxPosts, limit, len(inboxPosts) > limit)
		s.setFeedCache(ctx, cacheKey, result)
		return result, nil
	}

	var result *FeedResult
	if useHybridCursor {
		inboxCursor := readCursor.InboxLastPostID
		pullCursor := readCursor.PullLastPostID
		if s.exposureRepo != nil {
			inboxCursor = inboxCandidates.nextCursor
			pullCursor = pullCandidates.nextCursor
		}
		page := mixFeedPageForCursor(
			inboxPosts,
			pullCandidates.posts,
			limit,
			inboxCursor,
			pullCursor,
		)
		if s.exposureRepo != nil {
			if !inboxCandidates.hasMore {
				page.nextInboxCursor = inboxCandidates.nextCursor
				page.inboxPendingPostIDs = nil
			}
			if !pullCandidates.hasMore {
				page.nextPullCursor = pullCandidates.nextCursor
				page.pullPendingPostIDs = nil
			}
		}
		result = buildFeedResultWithHybridCursor(page)
	} else if inboxHit {
		mixedPosts := mixFeedPostsForPage(inboxPosts, pullCandidates.posts, limit)
		result = buildFeedResult(mixedPosts, limit, len(mixedPosts) > limit)
	} else {
		result = buildFeedResult(pullCandidates.posts, limit, pullCandidates.hasMore || len(pullCandidates.posts) > limit)
	}

	s.setFeedCache(ctx, cacheKey, result)
	s.markFeedResultExposure(ctx, req.UserID, result)
	return result, nil
}

func buildFeedCacheKey(userID int64, lastPostID int64, cursor string, limit int) string {
	if cursor != "" {
		return fmt.Sprintf("feed:home:%d:%s:%d", userID, cursor, limit)
	}
	return fmt.Sprintf("feed:home:%d:%d:%d", userID, lastPostID, limit)
}

func resolveFeedReadCursor(req GetFeedRequest) (feedReadCursor, error) {
	if req.Cursor == "" {
		return feedReadCursor{
			InboxLastPostID: req.LastPostID,
			PullLastPostID:  req.LastPostID,
		}, nil
	}

	return decodeFeedCursorToken(req.Cursor)
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

func (s *FeedService) getHomeFeedFromInboxPostsWithPending(
	ctx context.Context,
	userID int64,
	lastPostID int64,
	limit int,
	pendingPostIDs []int64,
) ([]*model.Post, bool) {
	pendingPosts, ok := s.getPostsByPendingIDs(ctx, pendingPostIDs)
	if !ok {
		return nil, false
	}

	freshPosts, inboxHit := s.getHomeFeedFromInboxPosts(ctx, userID, lastPostID, limit)
	if !inboxHit {
		if len(pendingPosts) == 0 {
			return nil, false
		}
		return pendingPosts, true
	}

	return append(pendingPosts, freshPosts...), true
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

func (s *FeedService) getHomeFeedByPullPostsWithPending(
	ctx context.Context,
	userID int64,
	lastPostID int64,
	limit int,
	pendingPostIDs []int64,
) ([]*model.Post, error) {
	pendingPosts, ok := s.getPostsByPendingIDs(ctx, pendingPostIDs)
	if !ok {
		return nil, fmt.Errorf("list pending pull posts failed")
	}

	posts, err := s.getHomeFeedByPullPosts(ctx, userID, lastPostID, limit)
	if err != nil {
		return nil, err
	}

	return append(pendingPosts, posts...), nil
}

func (s *FeedService) getPostsByPendingIDs(ctx context.Context, postIDs []int64) ([]*model.Post, bool) {
	if len(postIDs) == 0 {
		return nil, true
	}

	posts, err := s.postRepo.ListByIDs(ctx, postIDs)
	if err != nil {
		return nil, false
	}
	return posts, true
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

func buildFeedResultWithHybridCursor(page feedMixPageResult) *FeedResult {
	items := make([]FeedItem, 0, len(page.visible))
	for _, post := range page.visible {
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

	result := &FeedResult{
		Items:   items,
		HasMore: page.hasMore,
	}
	if !page.hasMore {
		return result
	}

	token, err := encodeFeedCursorToken(feedReadCursor{
		InboxLastPostID: page.nextInboxCursor,
		PullLastPostID:  page.nextPullCursor,
		InboxPendingIDs: page.inboxPendingPostIDs,
		PullPendingIDs:  page.pullPendingPostIDs,
	})
	if err != nil {
		// Fall back to no continuation token instead of failing the feed read path.
		return result
	}
	result.NextCursorToken = token
	return result
}
