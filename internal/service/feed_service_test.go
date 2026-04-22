package service

import (
	"context"
	"errors"
	"sort"
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

	gotUserIDs   []int64
	gotLastPost  int64
	gotLimit     int
	calledTimes  int
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
		name             string
		req              GetFeedRequest
		followRepo       *fakeFeedFollowRepo
		postRepo         *fakeFeedPostRepo
		wantErr          *xerror.Error
		wantItemIDs      []int64
		wantHasMore      bool
		wantNextCursor   int64
		wantRepoLimit    int
		wantLastPostID   int64
		wantContainSelf  bool
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
