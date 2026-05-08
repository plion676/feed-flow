package service

import (
	"container/heap"
	"context"
	"fmt"
	"sort"

	"github.com/plion676/feed-flow/internal/model"
)

const (
	defaultFeedOutboxReadChunkSize     = 32
	defaultFeedOutboxMaxAuthorsPerRead = 200
)

type feedUserCountRepository interface {
	BatchGetFollowerCounts(ctx context.Context, userIDs []int64) (map[int64]int64, error)
}

type feedOutboxReadRepository interface {
	ListPostIDsByCursor(ctx context.Context, authorUserID int64, maxPostID int64, limit int) ([]int64, error)
}

type FeedOutboxOptions struct {
	Enabled           bool
	ReadChunkSize     int
	MaxAuthorsPerRead int
	DBFallbackEnabled bool
}

type feedPullPlan struct {
	allAuthorIDs    []int64
	outboxAuthorIDs []int64
	dbAuthorIDs     []int64
	useOutbox       bool
}

type feedOutboxAuthorStream struct {
	authorUserID int64
	cursor       int64
	buffer       []int64
	exhausted    bool
}

type feedOutboxHeapItem struct {
	authorUserID int64
	postID       int64
}

type feedOutboxMaxHeap []feedOutboxHeapItem

func (h feedOutboxMaxHeap) Len() int { return len(h) }

func (h feedOutboxMaxHeap) Less(i, j int) bool {
	return h[i].postID > h[j].postID
}

func (h feedOutboxMaxHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *feedOutboxMaxHeap) Push(x any) {
	*h = append(*h, x.(feedOutboxHeapItem))
}

func (h *feedOutboxMaxHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

type feedOutboxCollector struct {
	repo      feedOutboxReadRepository
	chunkSize int
	streams   map[int64]*feedOutboxAuthorStream
	items     feedOutboxMaxHeap
}

// WithOutbox wires optional author outbox pull reads into Feed V2.
func (s *FeedService) WithOutbox(
	outboxRepo feedOutboxReadRepository,
	userCountRepo feedUserCountRepository,
	hybridPolicy *FeedHybridPolicy,
	options FeedOutboxOptions,
) *FeedService {
	s.outboxRepo = outboxRepo
	s.userCountRepo = userCountRepo
	s.hybridPolicy = hybridPolicy
	s.outboxEnabled = options.Enabled
	s.outboxReadChunkSize = options.ReadChunkSize
	s.outboxMaxAuthorsPerRead = options.MaxAuthorsPerRead
	s.outboxDBFallbackEnabled = options.DBFallbackEnabled
	return s
}

func (s *FeedService) isOutboxReadEnabled() bool {
	return s != nil &&
		s.outboxEnabled &&
		s.outboxRepo != nil &&
		s.userCountRepo != nil &&
		s.hybridPolicy != nil
}

func (s *FeedService) isOutboxDBFallbackEnabled() bool {
	if s == nil {
		return false
	}
	return s.outboxDBFallbackEnabled
}

func (s *FeedService) resolveOutboxReadChunkSize() int {
	if s == nil || s.outboxReadChunkSize <= 0 {
		return defaultFeedOutboxReadChunkSize
	}
	return s.outboxReadChunkSize
}

func (s *FeedService) resolveOutboxMaxAuthorsPerRead() int {
	if s == nil || s.outboxMaxAuthorsPerRead <= 0 {
		return defaultFeedOutboxMaxAuthorsPerRead
	}
	return s.outboxMaxAuthorsPerRead
}

func (s *FeedService) buildFeedPullPlan(
	ctx context.Context,
	userID int64,
	allowedAuthors map[int64]struct{},
) (feedPullPlan, error) {
	allAuthorIDs := collectAllowedAuthorIDs(allowedAuthors)
	sort.Slice(allAuthorIDs, func(i, j int) bool { return allAuthorIDs[i] < allAuthorIDs[j] })

	plan := feedPullPlan{
		allAuthorIDs: allAuthorIDs,
	}
	if len(allAuthorIDs) == 0 || !s.isOutboxReadEnabled() {
		return plan, nil
	}

	countTargetIDs := make([]int64, 0, len(allAuthorIDs))
	for _, authorUserID := range allAuthorIDs {
		if authorUserID <= 0 || authorUserID == userID {
			continue
		}
		countTargetIDs = append(countTargetIDs, authorUserID)
	}

	followerCounts, err := s.userCountRepo.BatchGetFollowerCounts(ctx, countTargetIDs)
	if err != nil {
		if s.isOutboxDBFallbackEnabled() {
			return plan, nil
		}
		return feedPullPlan{}, fmt.Errorf("batch get follower counts: %w", err)
	}

	outboxAuthorIDs := make([]int64, 0, len(allAuthorIDs))
	dbAuthorIDs := make([]int64, 0, len(allAuthorIDs))
	for _, authorUserID := range allAuthorIDs {
		if authorUserID <= 0 {
			continue
		}
		if authorUserID == userID {
			outboxAuthorIDs = append(outboxAuthorIDs, authorUserID)
			continue
		}

		mode := s.hybridPolicy.DecideByFollowerCount(int(followerCounts[authorUserID]))
		if mode == FeedDeliveryPullOnly {
			outboxAuthorIDs = append(outboxAuthorIDs, authorUserID)
			continue
		}
		dbAuthorIDs = append(dbAuthorIDs, authorUserID)
	}

	maxAuthorsPerRead := s.resolveOutboxMaxAuthorsPerRead()
	if maxAuthorsPerRead > 0 && len(outboxAuthorIDs) > maxAuthorsPerRead {
		if s.isOutboxDBFallbackEnabled() {
			return plan, nil
		}
		return feedPullPlan{}, fmt.Errorf(
			"outbox author count exceeds max_authors_per_read: count=%d limit=%d",
			len(outboxAuthorIDs),
			maxAuthorsPerRead,
		)
	}

	plan.outboxAuthorIDs = outboxAuthorIDs
	plan.dbAuthorIDs = dbAuthorIDs
	plan.useOutbox = len(outboxAuthorIDs) > 0
	return plan, nil
}

func (s *FeedService) listPullPostsWithPlan(
	ctx context.Context,
	lastPostID int64,
	limit int,
	plan feedPullPlan,
) ([]*model.Post, error) {
	if limit <= 0 {
		return []*model.Post{}, nil
	}
	if !plan.useOutbox {
		return s.listPullPostsFromDBAuthorIDs(ctx, plan.allAuthorIDs, lastPostID, limit)
	}

	outboxPosts, err := s.listPullPostsFromOutboxAuthors(ctx, plan.outboxAuthorIDs, lastPostID, limit)
	if err != nil {
		if s.isOutboxDBFallbackEnabled() {
			return s.listPullPostsFromDBAuthorIDs(ctx, plan.allAuthorIDs, lastPostID, limit)
		}
		return nil, err
	}
	if len(plan.dbAuthorIDs) == 0 {
		return outboxPosts, nil
	}

	dbPosts, err := s.listPullPostsFromDBAuthorIDs(ctx, plan.dbAuthorIDs, lastPostID, limit)
	if err != nil {
		return nil, err
	}
	return mergeFeedPullPosts(outboxPosts, dbPosts, limit), nil
}

func (s *FeedService) listPullPostsFromDBAuthorIDs(
	ctx context.Context,
	authorUserIDs []int64,
	lastPostID int64,
	limit int,
) ([]*model.Post, error) {
	if len(authorUserIDs) == 0 || limit <= 0 {
		return []*model.Post{}, nil
	}
	return s.postRepo.ListByUserIDs(ctx, authorUserIDs, lastPostID, limit)
}

func (s *FeedService) listPullPostsFromOutboxAuthors(
	ctx context.Context,
	authorUserIDs []int64,
	lastPostID int64,
	limit int,
) ([]*model.Post, error) {
	if len(authorUserIDs) == 0 || limit <= 0 {
		return []*model.Post{}, nil
	}

	collector, err := newFeedOutboxCollector(ctx, s.outboxRepo, authorUserIDs, lastPostID, s.resolveOutboxReadChunkSize())
	if err != nil {
		return nil, err
	}

	collected := make([]*model.Post, 0, limit)
	seenPostIDs := make(map[int64]struct{}, limit)

	for len(collected) < limit {
		needCount := limit - len(collected)
		postIDs, err := collector.NextPostIDs(ctx, needCount)
		if err != nil {
			return nil, err
		}
		if len(postIDs) == 0 {
			break
		}

		posts, err := s.postRepo.ListByIDs(ctx, postIDs)
		if err != nil {
			return nil, err
		}
		for _, post := range posts {
			if post == nil || post.ID <= 0 {
				continue
			}
			if _, ok := seenPostIDs[post.ID]; ok {
				continue
			}
			seenPostIDs[post.ID] = struct{}{}
			collected = append(collected, post)
			if len(collected) >= limit {
				break
			}
		}
	}

	return collected, nil
}

func newFeedOutboxCollector(
	ctx context.Context,
	repo feedOutboxReadRepository,
	authorUserIDs []int64,
	lastPostID int64,
	chunkSize int,
) (*feedOutboxCollector, error) {
	collector := &feedOutboxCollector{
		repo:      repo,
		chunkSize: chunkSize,
		streams:   make(map[int64]*feedOutboxAuthorStream, len(authorUserIDs)),
		items:     make(feedOutboxMaxHeap, 0, len(authorUserIDs)),
	}
	heap.Init(&collector.items)

	for _, authorUserID := range authorUserIDs {
		if authorUserID <= 0 {
			continue
		}
		stream := &feedOutboxAuthorStream{
			authorUserID: authorUserID,
			cursor:       lastPostID,
		}
		collector.streams[authorUserID] = stream
		if err := collector.pushNextItem(ctx, stream); err != nil {
			return nil, err
		}
	}

	return collector, nil
}

func (c *feedOutboxCollector) NextPostIDs(ctx context.Context, targetCount int) ([]int64, error) {
	if c == nil || targetCount <= 0 {
		return nil, nil
	}

	postIDs := make([]int64, 0, targetCount)
	seen := make(map[int64]struct{}, targetCount)

	for len(postIDs) < targetCount && c.items.Len() > 0 {
		item := heap.Pop(&c.items).(feedOutboxHeapItem)
		if item.postID > 0 {
			if _, ok := seen[item.postID]; !ok {
				seen[item.postID] = struct{}{}
				postIDs = append(postIDs, item.postID)
			}
		}

		stream := c.streams[item.authorUserID]
		if stream == nil {
			continue
		}
		if err := c.pushNextItem(ctx, stream); err != nil {
			return nil, err
		}
	}

	return postIDs, nil
}

func (c *feedOutboxCollector) pushNextItem(ctx context.Context, stream *feedOutboxAuthorStream) error {
	if c == nil || stream == nil || stream.authorUserID <= 0 {
		return nil
	}

	for len(stream.buffer) == 0 {
		if stream.exhausted {
			return nil
		}

		postIDs, err := c.repo.ListPostIDsByCursor(ctx, stream.authorUserID, stream.cursor, c.chunkSize)
		if err != nil {
			return fmt.Errorf("list outbox post ids author_id=%d: %w", stream.authorUserID, err)
		}
		postIDs = normalizeFeedOutboxPostIDs(postIDs)
		if len(postIDs) == 0 {
			stream.exhausted = true
			return nil
		}

		nextCursor, ok := minPostID(postIDs)
		if !ok {
			stream.exhausted = true
			return nil
		}
		if stream.cursor > 0 && nextCursor >= stream.cursor {
			stream.exhausted = true
			return nil
		}

		stream.buffer = postIDs
		stream.cursor = nextCursor
	}

	postID := stream.buffer[0]
	stream.buffer = stream.buffer[1:]
	heap.Push(&c.items, feedOutboxHeapItem{
		authorUserID: stream.authorUserID,
		postID:       postID,
	})
	return nil
}

func normalizeFeedOutboxPostIDs(postIDs []int64) []int64 {
	if len(postIDs) == 0 {
		return nil
	}

	result := make([]int64, 0, len(postIDs))
	seen := make(map[int64]struct{}, len(postIDs))
	for _, postID := range postIDs {
		if postID <= 0 {
			continue
		}
		if _, ok := seen[postID]; ok {
			continue
		}
		seen[postID] = struct{}{}
		result = append(result, postID)
	}
	return result
}

func mergeFeedPullPosts(outboxPosts []*model.Post, dbPosts []*model.Post, limit int) []*model.Post {
	if limit <= 0 {
		return []*model.Post{}
	}

	merged := make([]*model.Post, 0, len(outboxPosts)+len(dbPosts))
	seen := make(map[int64]struct{}, len(outboxPosts)+len(dbPosts))
	outboxIndex := 0
	dbIndex := 0

	for len(merged) < limit && (outboxIndex < len(outboxPosts) || dbIndex < len(dbPosts)) {
		var candidate *model.Post
		switch {
		case outboxIndex >= len(outboxPosts):
			candidate = dbPosts[dbIndex]
			dbIndex++
		case dbIndex >= len(dbPosts):
			candidate = outboxPosts[outboxIndex]
			outboxIndex++
		case resolveFeedPullPostID(outboxPosts[outboxIndex]) >= resolveFeedPullPostID(dbPosts[dbIndex]):
			candidate = outboxPosts[outboxIndex]
			outboxIndex++
		default:
			candidate = dbPosts[dbIndex]
			dbIndex++
		}

		if candidate == nil || candidate.ID <= 0 {
			continue
		}
		if _, ok := seen[candidate.ID]; ok {
			continue
		}
		seen[candidate.ID] = struct{}{}
		merged = append(merged, candidate)
	}

	return merged
}

func resolveFeedPullPostID(post *model.Post) int64 {
	if post == nil {
		return 0
	}
	return post.ID
}
