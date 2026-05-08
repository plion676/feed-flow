package service

import (
	"context"
	"time"

	"github.com/plion676/feed-flow/internal/model"
)

const (
	defaultFeedExposureWindowTTL       = 24 * time.Hour
	defaultFeedExposureBatchMultiplier = 3
	maxFeedExposureBatchLimit          = 200
)

type FeedExposureOptions struct {
	WindowTTL       time.Duration
	BatchMultiplier int
}

type feedExposureRepository interface {
	FilterUnseenPostIDs(ctx context.Context, userID int64, postIDs []int64, window time.Duration) ([]int64, error)
	MarkSeenPostIDs(ctx context.Context, userID int64, postIDs []int64, window time.Duration) error
}

type feedExposureCandidates struct {
	posts      []*model.Post
	nextCursor int64
	hasMore    bool
	hit        bool
}

func resolveFeedExposureBatchLimit(limit int, batchMultiplier int) int {
	if limit <= 0 {
		limit = defaultFeedLimit
	}
	if batchMultiplier <= 0 {
		batchMultiplier = defaultFeedExposureBatchMultiplier
	}

	batchLimit := limit * batchMultiplier
	if batchLimit < limit+1 {
		batchLimit = limit + 1
	}
	if batchLimit > maxFeedExposureBatchLimit {
		batchLimit = maxFeedExposureBatchLimit
	}
	return batchLimit
}

func (s *FeedService) resolveFeedExposureWindowTTL() time.Duration {
	if s == nil || s.exposureWindowTTL <= 0 {
		return defaultFeedExposureWindowTTL
	}
	return s.exposureWindowTTL
}

func (s *FeedService) collectPullExposureCandidates(
	ctx context.Context,
	userID int64,
	lastPostID int64,
	targetCount int,
	batchLimit int,
	pendingPostIDs []int64,
	allowedAuthors map[int64]struct{},
	pullPlan feedPullPlan,
) (feedExposureCandidates, error) {
	result := feedExposureCandidates{
		nextCursor: lastPostID,
	}
	if targetCount <= 0 {
		return result, nil
	}

	collected := make([]*model.Post, 0, targetCount)
	seen := make(map[int64]struct{}, targetCount)

	pendingPosts, ok := s.getPostsByPendingIDs(ctx, pendingPostIDs)
	if !ok {
		return result, context.DeadlineExceeded
	}
	pendingPosts = filterFeedPostsByAllowedAuthors(pendingPosts, allowedAuthors, userID)
	s.appendFeedExposurePosts(&collected, seen, s.filterFeedPostsByExposure(ctx, userID, pendingPosts), targetCount)

	cursor := lastPostID
	for len(collected) < targetCount {
		posts, err := s.getHomeFeedByPullPostsWithAllowedAuthors(ctx, userID, cursor, batchLimit, allowedAuthors, pullPlan)
		if err != nil {
			return result, err
		}
		if len(posts) == 0 {
			result.nextCursor = cursor
			return feedExposureCandidates{
				posts:      collected,
				nextCursor: cursor,
				hasMore:    false,
				hit:        len(collected) > 0 || len(pendingPostIDs) > 0,
			}, nil
		}

		nextCursor := resolvePostCursorFromPosts(posts, cursor)
		s.appendFeedExposurePosts(&collected, seen, s.filterFeedPostsByExposure(ctx, userID, posts), targetCount)

		hasMore := len(posts) >= batchLimit
		result = feedExposureCandidates{
			posts:      collected,
			nextCursor: nextCursor,
			hasMore:    hasMore,
			hit:        len(collected) > 0 || len(pendingPostIDs) > 0,
		}
		if len(posts) < batchLimit {
			return result, nil
		}
		if cursor > 0 && nextCursor >= cursor {
			return result, nil
		}

		cursor = nextCursor
	}

	return result, nil
}

func (s *FeedService) collectInboxExposureCandidates(
	ctx context.Context,
	userID int64,
	lastPostID int64,
	targetCount int,
	batchLimit int,
	pendingPostIDs []int64,
	allowedAuthors map[int64]struct{},
) (feedExposureCandidates, error) {
	result := feedExposureCandidates{
		nextCursor: lastPostID,
	}
	if s.inboxRepo == nil || targetCount <= 0 {
		return result, nil
	}

	collected := make([]*model.Post, 0, targetCount)
	seen := make(map[int64]struct{}, targetCount)

	pendingPosts, ok := s.getPostsByPendingIDs(ctx, pendingPostIDs)
	if !ok {
		return result, context.DeadlineExceeded
	}
	pendingPosts = filterFeedPostsByAllowedAuthors(pendingPosts, allowedAuthors, userID)
	s.appendFeedExposurePosts(&collected, seen, s.filterFeedPostsByExposure(ctx, userID, pendingPosts), targetCount)

	cursor := lastPostID
	hit := len(pendingPostIDs) > 0
	for len(collected) < targetCount {
		postIDs, err := s.inboxRepo.ListPostIDsByCursor(ctx, userID, cursor, batchLimit)
		if err != nil {
			return result, err
		}
		if len(postIDs) == 0 {
			return feedExposureCandidates{
				posts:      collected,
				nextCursor: cursor,
				hasMore:    false,
				hit:        hit || len(collected) > 0,
			}, nil
		}

		hit = true
		posts, err := s.postRepo.ListByIDs(ctx, postIDs)
		if err != nil {
			return result, err
		}
		posts = filterFeedPostsByAllowedAuthors(posts, allowedAuthors, userID)

		nextCursor, ok := minPostID(postIDs)
		if !ok {
			return feedExposureCandidates{
				posts:      collected,
				nextCursor: cursor,
				hasMore:    false,
				hit:        hit,
			}, nil
		}

		s.appendFeedExposurePosts(&collected, seen, s.filterFeedPostsByExposure(ctx, userID, posts), targetCount)

		hasMore := len(postIDs) >= batchLimit
		result = feedExposureCandidates{
			posts:      collected,
			nextCursor: nextCursor,
			hasMore:    hasMore,
			hit:        hit,
		}
		if len(postIDs) < batchLimit {
			return result, nil
		}
		if cursor > 0 && nextCursor >= cursor {
			return result, nil
		}

		cursor = nextCursor
	}

	return result, nil
}

func (s *FeedService) filterFeedPostsByExposure(ctx context.Context, userID int64, posts []*model.Post) []*model.Post {
	if s == nil || s.exposureRepo == nil || len(posts) == 0 || userID <= 0 {
		return posts
	}

	postIDs := collectPostIDs(posts)
	if len(postIDs) == 0 {
		return posts
	}

	unseenPostIDs, err := s.exposureRepo.FilterUnseenPostIDs(ctx, userID, postIDs, s.resolveFeedExposureWindowTTL())
	if err != nil {
		return posts
	}
	if len(unseenPostIDs) == 0 {
		return []*model.Post{}
	}

	allowed := make(map[int64]struct{}, len(unseenPostIDs))
	for _, postID := range unseenPostIDs {
		if postID <= 0 {
			continue
		}
		allowed[postID] = struct{}{}
	}

	filtered := make([]*model.Post, 0, len(posts))
	seen := make(map[int64]struct{}, len(unseenPostIDs))
	for _, post := range posts {
		if post == nil || post.ID <= 0 {
			continue
		}
		if _, ok := allowed[post.ID]; !ok {
			continue
		}
		if _, ok := seen[post.ID]; ok {
			continue
		}
		seen[post.ID] = struct{}{}
		filtered = append(filtered, post)
	}

	return filtered
}

func (s *FeedService) markFeedResultExposure(ctx context.Context, userID int64, result *FeedResult) {
	if s == nil || s.exposureRepo == nil || result == nil || len(result.Items) == 0 || userID <= 0 {
		return
	}

	postIDs := make([]int64, 0, len(result.Items))
	seen := make(map[int64]struct{}, len(result.Items))
	for _, item := range result.Items {
		if item.PostID <= 0 {
			continue
		}
		if _, ok := seen[item.PostID]; ok {
			continue
		}
		seen[item.PostID] = struct{}{}
		postIDs = append(postIDs, item.PostID)
	}
	if len(postIDs) == 0 {
		return
	}

	// TODO(user): consider source-specific exposure semantics for different candidate pools.
	_ = s.exposureRepo.MarkSeenPostIDs(ctx, userID, postIDs, s.resolveFeedExposureWindowTTL())
}

func (s *FeedService) appendFeedExposurePosts(
	dst *[]*model.Post,
	seen map[int64]struct{},
	posts []*model.Post,
	targetCount int,
) {
	for _, post := range posts {
		if post == nil || post.ID <= 0 {
			continue
		}
		if _, ok := seen[post.ID]; ok {
			continue
		}
		seen[post.ID] = struct{}{}
		*dst = append(*dst, post)
		if targetCount > 0 && len(*dst) >= targetCount {
			return
		}
	}
}

func collectPostIDs(posts []*model.Post) []int64 {
	if len(posts) == 0 {
		return nil
	}

	postIDs := make([]int64, 0, len(posts))
	seen := make(map[int64]struct{}, len(posts))
	for _, post := range posts {
		if post == nil || post.ID <= 0 {
			continue
		}
		if _, ok := seen[post.ID]; ok {
			continue
		}
		seen[post.ID] = struct{}{}
		postIDs = append(postIDs, post.ID)
	}
	return postIDs
}

func resolvePostCursorFromPosts(posts []*model.Post, currentCursor int64) int64 {
	if len(posts) == 0 {
		return currentCursor
	}

	minPostID := int64(0)
	for _, post := range posts {
		if post == nil || post.ID <= 0 {
			continue
		}
		if minPostID == 0 || post.ID < minPostID {
			minPostID = post.ID
		}
	}
	if minPostID == 0 {
		return currentCursor
	}
	return minPostID
}
