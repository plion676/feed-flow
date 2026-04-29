package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type fakeFeedFollowRepo struct {
	followingIDs []int64
	err          error
}

func (r *fakeFeedFollowRepo) ListFollowingUserIDs(_ context.Context, _ int64) ([]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]int64, len(r.followingIDs))
	copy(out, r.followingIDs)
	return out, nil
}

type fakeFeedPostRepo struct {
	posts []*model.Post
	err   error

	gotUserIDs  []int64
	gotPostIDs  []int64
	gotLastPost int64
	gotLimit    int
	calledTimes int
	idsCalled   int
}

func (r *fakeFeedPostRepo) ListByUserIDs(_ context.Context, userIDs []int64, lastPostID int64, limit int) ([]*model.Post, error) {
	r.calledTimes++
	r.gotUserIDs = append([]int64(nil), userIDs...)
	r.gotLastPost = lastPostID
	r.gotLimit = limit

	if r.err != nil {
		return nil, r.err
	}

	allow := make(map[int64]struct{}, len(userIDs))
	for _, id := range userIDs {
		allow[id] = struct{}{}
	}

	filtered := make([]*model.Post, 0, len(r.posts))
	for _, p := range r.posts {
		if _, ok := allow[p.UserID]; !ok {
			continue
		}
		if p.Status != 1 {
			continue
		}
		if lastPostID > 0 && p.ID >= lastPostID {
			continue
		}
		filtered = append(filtered, p)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID > filtered[j].ID
	})

	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered, nil
}

func (r *fakeFeedPostRepo) ListByIDs(_ context.Context, postIDs []int64) ([]*model.Post, error) {
	r.idsCalled++
	r.gotPostIDs = append([]int64(nil), postIDs...)
	if r.err != nil {
		return nil, r.err
	}

	byID := make(map[int64]*model.Post, len(r.posts))
	for _, post := range r.posts {
		if post.Status != 1 {
			continue
		}
		byID[post.ID] = post
	}

	ordered := make([]*model.Post, 0, len(postIDs))
	for _, postID := range postIDs {
		post, ok := byID[postID]
		if !ok {
			continue
		}
		ordered = append(ordered, post)
	}
	return ordered, nil
}

type fakeFeedInboxRepo struct {
	postIDsByUser map[int64][]int64
	err           error

	gotUserID     int64
	gotLastPostID int64
	gotLimit      int
	called        int
}

func (r *fakeFeedInboxRepo) ListPostIDsByCursor(_ context.Context, userID int64, lastPostID int64, limit int) ([]int64, error) {
	r.called++
	r.gotUserID = userID
	r.gotLastPostID = lastPostID
	r.gotLimit = limit
	if r.err != nil {
		return nil, r.err
	}

	source := r.postIDsByUser[userID]
	filtered := make([]int64, 0, len(source))
	for _, postID := range source {
		if lastPostID > 0 && postID >= lastPostID {
			continue
		}
		filtered = append(filtered, postID)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered, nil
}

type fakeFeedCacheRepo struct {
	store map[string]string

	getErr error
	setErr error

	setCalled int
}

func (r *fakeFeedCacheRepo) Get(_ context.Context, key string) (string, bool, error) {
	if r.getErr != nil {
		return "", false, r.getErr
	}
	if r.store == nil {
		return "", false, nil
	}
	value, ok := r.store[key]
	return value, ok, nil
}

func (r *fakeFeedCacheRepo) Set(_ context.Context, key string, value string, _ time.Duration) error {
	r.setCalled++
	if r.setErr != nil {
		return r.setErr
	}
	if r.store == nil {
		r.store = make(map[string]string)
	}
	r.store[key] = value
	return nil
}

func containsInt64(nums []int64, target int64) bool {
	for _, n := range nums {
		if n == target {
			return true
		}
	}
	return false
}

func TestFeedServiceGetHomeFeed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 22, 10, 0, 0, 0, time.UTC)

	basePosts := []*model.Post{
		{ID: 10, UserID: 1002, Content: "p10", Status: 1, CreatedAt: now.Add(-1 * time.Minute)},
		{ID: 9, UserID: 1001, Content: "p9", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
		{ID: 8, UserID: 1002, Content: "p8", Status: 1, CreatedAt: now.Add(-3 * time.Minute)},
		{ID: 7, UserID: 1001, Content: "p7", Status: 1, CreatedAt: now.Add(-4 * time.Minute)},
		{ID: 6, UserID: 9999, Content: "p6", Status: 1, CreatedAt: now.Add(-5 * time.Minute)},
		{ID: 5, UserID: 1002, Content: "p5-hidden", Status: 0, CreatedAt: now.Add(-6 * time.Minute)},
	}

	tests := []struct {
		name            string
		req             GetFeedRequest
		followRepo      *fakeFeedFollowRepo
		postRepo        *fakeFeedPostRepo
		wantErr         *xerror.Error
		wantItemIDs     []int64
		wantHasMore     bool
		wantNextCursor  int64
		wantRepoLimit   int
		wantLastPostID  int64
		wantContainSelf bool
	}{
		{
			name:            "bad request when user id missing",
			req:             GetFeedRequest{UserID: 0, Limit: 3},
			followRepo:      &fakeFeedFollowRepo{},
			postRepo:        &fakeFeedPostRepo{},
			wantErr:         xerror.ErrUnauthorized,
			wantItemIDs:     nil,
			wantHasMore:     false,
			wantNextCursor:  0,
			wantRepoLimit:   0,
			wantContainSelf: false,
		},
		{
			name:            "default limit with empty feed",
			req:             GetFeedRequest{UserID: 1001, Limit: 0},
			followRepo:      &fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo:        &fakeFeedPostRepo{posts: []*model.Post{}},
			wantErr:         nil,
			wantItemIDs:     []int64{},
			wantHasMore:     false,
			wantNextCursor:  0,
			wantRepoLimit:   defaultFeedLimit + 1,
			wantLastPostID:  0,
			wantContainSelf: true,
		},
		{
			name:            "less than limit has_more false",
			req:             GetFeedRequest{UserID: 1001, Limit: 3},
			followRepo:      &fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo:        &fakeFeedPostRepo{posts: basePosts[2:]},
			wantErr:         nil,
			wantItemIDs:     []int64{8, 7},
			wantHasMore:     false,
			wantNextCursor:  7,
			wantRepoLimit:   4,
			wantLastPostID:  0,
			wantContainSelf: true,
		},
		{
			name:            "equal limit has_more false",
			req:             GetFeedRequest{UserID: 1001, Limit: 3},
			followRepo:      &fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo:        &fakeFeedPostRepo{posts: basePosts[:3]},
			wantErr:         nil,
			wantItemIDs:     []int64{10, 9, 8},
			wantHasMore:     false,
			wantNextCursor:  8,
			wantRepoLimit:   4,
			wantLastPostID:  0,
			wantContainSelf: true,
		},
		{
			name:            "limit plus one has_more true and trim to limit",
			req:             GetFeedRequest{UserID: 1001, Limit: 3},
			followRepo:      &fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo:        &fakeFeedPostRepo{posts: basePosts[:4]},
			wantErr:         nil,
			wantItemIDs:     []int64{10, 9, 8},
			wantHasMore:     true,
			wantNextCursor:  8,
			wantRepoLimit:   4,
			wantLastPostID:  0,
			wantContainSelf: true,
		},
		{
			name:            "last_post_id pagination works",
			req:             GetFeedRequest{UserID: 1001, LastPostID: 9, Limit: 3},
			followRepo:      &fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo:        &fakeFeedPostRepo{posts: basePosts[:4]},
			wantErr:         nil,
			wantItemIDs:     []int64{8, 7},
			wantHasMore:     false,
			wantNextCursor:  7,
			wantRepoLimit:   4,
			wantLastPostID:  9,
			wantContainSelf: true,
		},
		{
			name:            "follow repository error",
			req:             GetFeedRequest{UserID: 1001, Limit: 3},
			followRepo:      &fakeFeedFollowRepo{err: errors.New("follow query failed")},
			postRepo:        &fakeFeedPostRepo{posts: basePosts},
			wantErr:         xerror.ErrInternal,
			wantRepoLimit:   0,
			wantContainSelf: false,
		},
		{
			name:            "post repository error",
			req:             GetFeedRequest{UserID: 1001, Limit: 3},
			followRepo:      &fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo:        &fakeFeedPostRepo{err: errors.New("post query failed")},
			wantErr:         xerror.ErrInternal,
			wantRepoLimit:   4,
			wantContainSelf: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewFeedService(tc.followRepo, tc.postRepo)
			got, gotErr := svc.GetHomeFeed(ctx, tc.req)

			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}

			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("expected nil result when error happens, got=%+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil result on success")
			}

			if tc.postRepo.gotLimit != tc.wantRepoLimit {
				t.Fatalf("unexpected repository limit: got=%d want=%d", tc.postRepo.gotLimit, tc.wantRepoLimit)
			}
			if tc.postRepo.gotLastPost != tc.wantLastPostID {
				t.Fatalf("unexpected repository last_post_id: got=%d want=%d", tc.postRepo.gotLastPost, tc.wantLastPostID)
			}
			if tc.wantContainSelf && !containsInt64(tc.postRepo.gotUserIDs, tc.req.UserID) {
				t.Fatalf("candidate userIDs should contain self user id=%d, got=%v", tc.req.UserID, tc.postRepo.gotUserIDs)
			}

			if got.HasMore != tc.wantHasMore {
				t.Fatalf("unexpected has_more: got=%v want=%v", got.HasMore, tc.wantHasMore)
			}
			if got.NextCursor != tc.wantNextCursor {
				t.Fatalf("unexpected next_cursor: got=%d want=%d", got.NextCursor, tc.wantNextCursor)
			}
			if len(got.Items) != len(tc.wantItemIDs) {
				t.Fatalf("unexpected item count: got=%d want=%d", len(got.Items), len(tc.wantItemIDs))
			}
			for i, wantID := range tc.wantItemIDs {
				if got.Items[i].PostID != wantID {
					t.Fatalf("unexpected item post_id at index %d: got=%d want=%d", i, got.Items[i].PostID, wantID)
				}
			}
		})
	}
}

func TestFeedServiceGetHomeFeedCache(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 22, 12, 0, 0, 0, time.UTC)

	t.Run("cache hit should skip db repositories", func(t *testing.T) {
		t.Parallel()

		cacheKey := buildFeedCacheKey(1001, 0, "", 3)
		cacheRepo := &fakeFeedCacheRepo{
			store: map[string]string{
				cacheKey: `{"items":[{"post_id":10,"user_id":1002,"content":"cached","created_at":"2026-04-22T12:00:00Z"}],"next_cursor":10,"has_more":false}`,
			},
		}

		svc := NewFeedService(
			&fakeFeedFollowRepo{err: errors.New("should not be called")},
			&fakeFeedPostRepo{err: errors.New("should not be called")},
		).WithCache(cacheRepo)

		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 1 || got.Items[0].PostID != 10 {
			t.Fatalf("unexpected cached result: %+v", got)
		}
		if cacheRepo.setCalled != 0 {
			t.Fatalf("cache hit should not call set, got=%d", cacheRepo.setCalled)
		}
	})

	t.Run("cache miss should fallback db and then set cache", func(t *testing.T) {
		t.Parallel()

		cacheRepo := &fakeFeedCacheRepo{}
		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 10, UserID: 1002, Content: "p10", Status: 1, CreatedAt: now},
				{ID: 9, UserID: 1001, Content: "p9", Status: 1, CreatedAt: now.Add(-time.Minute)},
			},
		}

		svc := NewFeedService(
			&fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo,
		).WithCache(cacheRepo)

		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 2 {
			t.Fatalf("unexpected result on cache miss: %+v", got)
		}
		if postRepo.calledTimes != 1 {
			t.Fatalf("db repository should be called once on cache miss, got=%d", postRepo.calledTimes)
		}
		if cacheRepo.setCalled != 1 {
			t.Fatalf("cache should be set once, got=%d", cacheRepo.setCalled)
		}

		expectKey := buildFeedCacheKey(1001, 0, "", 3)
		cachedPayload, ok := cacheRepo.store[expectKey]
		if !ok {
			t.Fatalf("expected cached payload at key=%s", expectKey)
		}
		if !strings.Contains(cachedPayload, `"post_id":10`) {
			t.Fatalf("unexpected cached payload: %s", cachedPayload)
		}
	})

	t.Run("cache get error should fallback db", func(t *testing.T) {
		t.Parallel()

		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 7, UserID: 1001, Content: "p7", Status: 1, CreatedAt: now},
			},
		}
		cacheRepo := &fakeFeedCacheRepo{getErr: errors.New("redis timeout")}

		svc := NewFeedService(
			&fakeFeedFollowRepo{},
			postRepo,
		).WithCache(cacheRepo)

		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 1 || got.Items[0].PostID != 7 {
			t.Fatalf("unexpected fallback result: %+v", got)
		}
		if postRepo.calledTimes != 1 {
			t.Fatalf("db repository should be called when cache get fails, got=%d", postRepo.calledTimes)
		}
	})
}

func TestFeedServiceGetHomeFeedInbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC)

	t.Run("inbox hit should merge with pull results to keep pull-only authors", func(t *testing.T) {
		t.Parallel()

		followRepo := &fakeFeedFollowRepo{followingIDs: []int64{1002, 1003}}
		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 11, UserID: 1003, Content: "pull-only", Status: 1, CreatedAt: now},
				{ID: 10, UserID: 1002, Content: "p10", Status: 1, CreatedAt: now.Add(-time.Minute)},
				{ID: 9, UserID: 1001, Content: "p9", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
				{ID: 8, UserID: 1002, Content: "p8", Status: 1, CreatedAt: now.Add(-3 * time.Minute)},
			},
		}
		inboxRepo := &fakeFeedInboxRepo{
			postIDsByUser: map[int64][]int64{
				1001: {10, 8},
			},
		}

		svc := NewFeedService(followRepo, postRepo).WithInbox(inboxRepo)
		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 3 {
			t.Fatalf("unexpected inbox result: %+v", got)
		}
		if got.Items[0].PostID != 11 || got.Items[1].PostID != 10 || got.Items[2].PostID != 9 {
			t.Fatalf("unexpected inbox order: %+v", got.Items)
		}
		if !got.HasMore || got.NextCursor != 0 || got.NextCursorToken == "" {
			t.Fatalf("unexpected inbox paging: has_more=%v next=%d token=%q", got.HasMore, got.NextCursor, got.NextCursorToken)
		}
		if postRepo.calledTimes != 1 {
			t.Fatalf("pull query should still run for merge, pull_calls=%d", postRepo.calledTimes)
		}
		if postRepo.idsCalled != 1 {
			t.Fatalf("expected ListByIDs called once, got=%d", postRepo.idsCalled)
		}
	})

	t.Run("mix strategy should reserve pull slot when inbox can fill whole page", func(t *testing.T) {
		t.Parallel()

		followRepo := &fakeFeedFollowRepo{followingIDs: []int64{1002, 1003}}
		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 12, UserID: 1002, Content: "inbox-12", Status: 1, CreatedAt: now},
				{ID: 11, UserID: 1002, Content: "inbox-11", Status: 1, CreatedAt: now.Add(-time.Minute)},
				{ID: 10, UserID: 1002, Content: "inbox-10", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
				{ID: 9, UserID: 1003, Content: "pull-9", Status: 1, CreatedAt: now.Add(-3 * time.Minute)},
				{ID: 8, UserID: 1001, Content: "self-8", Status: 1, CreatedAt: now.Add(-4 * time.Minute)},
			},
		}
		inboxRepo := &fakeFeedInboxRepo{
			postIDsByUser: map[int64][]int64{
				1001: {12, 11, 10},
			},
		}

		svc := NewFeedService(followRepo, postRepo).WithInbox(inboxRepo)
		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 3 {
			t.Fatalf("unexpected mixed result: %+v", got)
		}
		wantOrder := []int64{12, 11, 9}
		for i, wantID := range wantOrder {
			if got.Items[i].PostID != wantID {
				t.Fatalf("unexpected mixed order at %d: got=%d want=%d", i, got.Items[i].PostID, wantID)
			}
		}
		if !got.HasMore || got.NextCursor != 0 || got.NextCursorToken == "" {
			t.Fatalf("unexpected mixed paging: has_more=%v next=%d token=%q", got.HasMore, got.NextCursor, got.NextCursorToken)
		}
	})

	t.Run("inbox miss should fallback pull", func(t *testing.T) {
		t.Parallel()

		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 9, UserID: 1001, Content: "p9", Status: 1, CreatedAt: now},
				{ID: 8, UserID: 1002, Content: "p8", Status: 1, CreatedAt: now.Add(-time.Minute)},
			},
		}
		inboxRepo := &fakeFeedInboxRepo{
			postIDsByUser: map[int64][]int64{},
		}

		svc := NewFeedService(
			&fakeFeedFollowRepo{followingIDs: []int64{1002}},
			postRepo,
		).WithInbox(inboxRepo)

		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 2 {
			t.Fatalf("unexpected fallback result: %+v", got)
		}
		if postRepo.calledTimes != 1 {
			t.Fatalf("expected pull query called once on inbox miss, got=%d", postRepo.calledTimes)
		}
		if postRepo.idsCalled != 0 {
			t.Fatalf("unexpected inbox detail query on miss, ids_called=%d", postRepo.idsCalled)
		}
	})

	t.Run("inbox read error should fallback pull", func(t *testing.T) {
		t.Parallel()

		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 7, UserID: 1001, Content: "p7", Status: 1, CreatedAt: now},
			},
		}
		inboxRepo := &fakeFeedInboxRepo{err: errors.New("redis timeout")}

		svc := NewFeedService(
			&fakeFeedFollowRepo{},
			postRepo,
		).WithInbox(inboxRepo)

		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 1 || got.Items[0].PostID != 7 {
			t.Fatalf("unexpected fallback result on inbox error: %+v", got)
		}
		if postRepo.calledTimes != 1 {
			t.Fatalf("expected pull path called once on inbox error, got=%d", postRepo.calledTimes)
		}
	})

	t.Run("inbox backfill should continue scanning when detail posts are missing", func(t *testing.T) {
		t.Parallel()

		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 6, UserID: 1001, Content: "p6", Status: 1, CreatedAt: now},
				{ID: 5, UserID: 1001, Content: "p5", Status: 1, CreatedAt: now.Add(-time.Minute)},
			},
		}
		inboxRepo := &fakeFeedInboxRepo{
			postIDsByUser: map[int64][]int64{
				1001: {10, 9, 8, 7, 6, 5},
			},
		}

		svc := NewFeedService(
			&fakeFeedFollowRepo{err: errors.New("pull unavailable")},
			postRepo,
		).WithInbox(inboxRepo)

		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  2,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 2 {
			t.Fatalf("unexpected backfill result: %+v", got)
		}
		if got.Items[0].PostID != 6 || got.Items[1].PostID != 5 {
			t.Fatalf("unexpected backfill items: %+v", got.Items)
		}
		if got.HasMore {
			t.Fatalf("unexpected has_more on backfill result: %+v", got)
		}
		if postRepo.idsCalled < 2 {
			t.Fatalf("expected multiple ListByIDs calls for backfill, got=%d", postRepo.idsCalled)
		}
		if postRepo.calledTimes != 0 {
			t.Fatalf("pull query should be skipped when follow repo fails and inbox hit succeeds, pull_calls=%d", postRepo.calledTimes)
		}
	})

	t.Run("hybrid cursor token should prevent gap when pull reservation reorders page", func(t *testing.T) {
		t.Parallel()

		followRepo := &fakeFeedFollowRepo{followingIDs: []int64{1002, 1003}}
		postRepo := &fakeFeedPostRepo{
			posts: []*model.Post{
				{ID: 12, UserID: 1002, Content: "inbox-12", Status: 1, CreatedAt: now},
				{ID: 11, UserID: 1002, Content: "inbox-11", Status: 1, CreatedAt: now.Add(-time.Minute)},
				{ID: 10, UserID: 1002, Content: "inbox-10", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
				{ID: 9, UserID: 1003, Content: "pull-9", Status: 1, CreatedAt: now.Add(-3 * time.Minute)},
				{ID: 8, UserID: 1001, Content: "pull-8", Status: 1, CreatedAt: now.Add(-4 * time.Minute)},
			},
		}
		inboxRepo := &fakeFeedInboxRepo{
			postIDsByUser: map[int64][]int64{
				1001: {12, 11, 10},
			},
		}

		svc := NewFeedService(followRepo, postRepo).WithInbox(inboxRepo)
		firstPage, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected first page error: %v", gotErr)
		}
		if firstPage == nil || len(firstPage.Items) != 3 {
			t.Fatalf("unexpected first page result: %+v", firstPage)
		}
		wantFirstPage := []int64{12, 11, 9}
		for i, wantID := range wantFirstPage {
			if firstPage.Items[i].PostID != wantID {
				t.Fatalf("unexpected first page item at %d: got=%d want=%d", i, firstPage.Items[i].PostID, wantID)
			}
		}
		if firstPage.NextCursorToken == "" || !firstPage.HasMore {
			t.Fatalf("expected hybrid token on first page: %+v", firstPage)
		}

		secondPage, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Cursor: firstPage.NextCursorToken,
			Limit:  3,
		})
		if gotErr != nil {
			t.Fatalf("unexpected second page error: %v", gotErr)
		}
		if secondPage == nil || len(secondPage.Items) != 2 {
			t.Fatalf("unexpected second page result: %+v", secondPage)
		}
		wantSecondPage := []int64{10, 8}
		for i, wantID := range wantSecondPage {
			if secondPage.Items[i].PostID != wantID {
				t.Fatalf("unexpected second page item at %d: got=%d want=%d", i, secondPage.Items[i].PostID, wantID)
			}
		}
		if secondPage.HasMore {
			t.Fatalf("unexpected has_more on second page: %+v", secondPage)
		}
		if secondPage.NextCursorToken != "" {
			t.Fatalf("expected empty next cursor token on last page, got=%q", secondPage.NextCursorToken)
		}
	})

	t.Run("invalid hybrid cursor token should return bad request", func(t *testing.T) {
		t.Parallel()

		svc := NewFeedService(
			&fakeFeedFollowRepo{followingIDs: []int64{1002}},
			&fakeFeedPostRepo{},
		).WithInbox(&fakeFeedInboxRepo{})

		got, gotErr := svc.GetHomeFeed(ctx, GetFeedRequest{
			UserID: 1001,
			Cursor: "not-a-valid-token",
			Limit:  3,
		})
		if got != nil {
			t.Fatalf("expected nil result on invalid token, got=%+v", got)
		}
		if gotErr != xerror.ErrBadRequest {
			t.Fatalf("unexpected error on invalid token: got=%v want=%v", gotErr, xerror.ErrBadRequest)
		}
	})
}

func TestMixFeedPostsForPage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 24, 13, 0, 0, 0, time.UTC)

	t.Run("should dedupe same post across inbox and pull", func(t *testing.T) {
		t.Parallel()

		inboxPosts := []*model.Post{
			{ID: 10, UserID: 1002, Content: "inbox-10", Status: 1, CreatedAt: now},
			{ID: 8, UserID: 1002, Content: "inbox-8", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
		}
		pullPosts := []*model.Post{
			{ID: 11, UserID: 1003, Content: "pull-11", Status: 1, CreatedAt: now.Add(time.Minute)},
			{ID: 10, UserID: 1002, Content: "pull-10", Status: 1, CreatedAt: now},
			{ID: 9, UserID: 1001, Content: "pull-9", Status: 1, CreatedAt: now.Add(-time.Minute)},
		}

		mixed := mixFeedPostsForPage(inboxPosts, pullPosts, 3)
		wantOrder := []int64{11, 10, 9, 8}
		if len(mixed) != len(wantOrder) {
			t.Fatalf("unexpected mixed count: got=%d want=%d", len(mixed), len(wantOrder))
		}
		for i, wantID := range wantOrder {
			if mixed[i].ID != wantID {
				t.Fatalf("unexpected mixed order at %d: got=%d want=%d", i, mixed[i].ID, wantID)
			}
		}
	})

	t.Run("should reserve pull items instead of returning all inbox", func(t *testing.T) {
		t.Parallel()

		inboxPosts := []*model.Post{
			{ID: 12, UserID: 1002, Content: "inbox-12", Status: 1, CreatedAt: now},
			{ID: 11, UserID: 1002, Content: "inbox-11", Status: 1, CreatedAt: now.Add(-time.Minute)},
			{ID: 10, UserID: 1002, Content: "inbox-10", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
		}
		pullPosts := []*model.Post{
			{ID: 9, UserID: 1003, Content: "pull-9", Status: 1, CreatedAt: now.Add(-3 * time.Minute)},
			{ID: 8, UserID: 1001, Content: "pull-8", Status: 1, CreatedAt: now.Add(-4 * time.Minute)},
		}

		mixed := mixFeedPostsForPage(inboxPosts, pullPosts, 3)
		wantOrder := []int64{12, 11, 9, 10}
		if len(mixed) != len(wantOrder) {
			t.Fatalf("unexpected reserved count: got=%d want=%d", len(mixed), len(wantOrder))
		}
		for i, wantID := range wantOrder {
			if mixed[i].ID != wantID {
				t.Fatalf("unexpected reserved order at %d: got=%d want=%d", i, mixed[i].ID, wantID)
			}
		}
	})

	t.Run("should scatter same author when alternative author exists", func(t *testing.T) {
		t.Parallel()

		inboxPosts := []*model.Post{
			{ID: 12, UserID: 1002, Content: "inbox-12", Status: 1, CreatedAt: now},
			{ID: 11, UserID: 1002, Content: "inbox-11", Status: 1, CreatedAt: now.Add(-time.Minute)},
			{ID: 10, UserID: 1002, Content: "inbox-10", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
		}
		pullPosts := []*model.Post{
			{ID: 13, UserID: 1002, Content: "pull-13", Status: 1, CreatedAt: now.Add(time.Minute)},
			{ID: 9, UserID: 1003, Content: "pull-9", Status: 1, CreatedAt: now.Add(-3 * time.Minute)},
			{ID: 8, UserID: 1004, Content: "pull-8", Status: 1, CreatedAt: now.Add(-4 * time.Minute)},
		}

		mixed := mixFeedPostsForPage(inboxPosts, pullPosts, 4)
		wantOrder := []int64{13, 12, 9, 11, 10}
		if len(mixed) != len(wantOrder) {
			t.Fatalf("unexpected scatter count: got=%d want=%d", len(mixed), len(wantOrder))
		}
		for i, wantID := range wantOrder {
			if mixed[i].ID != wantID {
				t.Fatalf("unexpected scatter order at %d: got=%d want=%d", i, mixed[i].ID, wantID)
			}
		}
	})
}
