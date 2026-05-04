package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type fakeUserReadRepoForService struct {
	users map[int64]*model.User
	err   error
}

func (r *fakeUserReadRepoForService) GetByID(_ context.Context, userID int64) (*model.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.users[userID], nil
}

type fakeUserPostReadRepoForService struct {
	postsByUser   map[int64][]*model.Post
	err           error
	gotUserID     int64
	gotLastPostID int64
	gotLimit      int
}

func (r *fakeUserPostReadRepoForService) ListByUserID(_ context.Context, userID int64, lastPostID int64, limit int) ([]*model.Post, error) {
	r.gotUserID = userID
	r.gotLastPostID = lastPostID
	r.gotLimit = limit

	if r.err != nil {
		return nil, r.err
	}

	posts := r.postsByUser[userID]
	filtered := make([]*model.Post, 0, len(posts))
	for _, post := range posts {
		if post.Status != model.PostStatusPublished {
			continue
		}
		if lastPostID > 0 && post.ID >= lastPostID {
			continue
		}
		filtered = append(filtered, post)
		if len(filtered) >= limit {
			break
		}
	}

	return filtered, nil
}

func TestUserServiceGetUserPosts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 5, 4, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		req            GetUserPostsRequest
		userRepo       *fakeUserReadRepoForService
		postRepo       *fakeUserPostReadRepoForService
		wantErr        *xerror.Error
		wantPostIDs    []int64
		wantHasMore    bool
		wantNextCursor int64
		wantRepoLimit  int
		wantRepoCursor int64
	}{
		{
			name:     "bad request when user id invalid",
			req:      GetUserPostsRequest{UserID: 0, Limit: 2},
			userRepo: &fakeUserReadRepoForService{},
			postRepo: &fakeUserPostReadRepoForService{},
			wantErr:  xerror.ErrBadRequest,
		},
		{
			name:     "bad request when last post id negative",
			req:      GetUserPostsRequest{UserID: 1001, LastPostID: -1, Limit: 2},
			userRepo: &fakeUserReadRepoForService{},
			postRepo: &fakeUserPostReadRepoForService{},
			wantErr:  xerror.ErrBadRequest,
		},
		{
			name:     "internal error when post repo is not configured",
			req:      GetUserPostsRequest{UserID: 1001, Limit: 2},
			userRepo: &fakeUserReadRepoForService{},
			postRepo: nil,
			wantErr:  xerror.ErrInternal,
		},
		{
			name: "internal error when user repo fails",
			req:  GetUserPostsRequest{UserID: 1001, Limit: 2},
			userRepo: &fakeUserReadRepoForService{
				err: errors.New("query user failed"),
			},
			postRepo: &fakeUserPostReadRepoForService{},
			wantErr:  xerror.ErrInternal,
		},
		{
			name: "not found when user missing",
			req:  GetUserPostsRequest{UserID: 1001, Limit: 2},
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{},
			},
			postRepo: &fakeUserPostReadRepoForService{},
			wantErr:  xerror.ErrNotFound,
		},
		{
			name: "internal error when post repo fails",
			req:  GetUserPostsRequest{UserID: 1001, Limit: 2},
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {ID: 1001, Username: "alice"},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{
				err: errors.New("query posts failed"),
			},
			wantErr: xerror.ErrInternal,
		},
		{
			name: "success with pagination and published-only filter",
			req:  GetUserPostsRequest{UserID: 1001, Limit: 2},
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {ID: 1001, Username: "alice"},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{
				postsByUser: map[int64][]*model.Post{
					1001: {
						{ID: 9, UserID: 1001, Content: "p9", Status: model.PostStatusPublished, CreatedAt: now},
						{ID: 8, UserID: 1001, Content: "deleted", Status: model.PostStatusDeleted, CreatedAt: now.Add(-1 * time.Minute)},
						{ID: 7, UserID: 1001, Content: "p7", Status: model.PostStatusPublished, CreatedAt: now.Add(-2 * time.Minute)},
						{ID: 6, UserID: 1001, Content: "p6", Status: model.PostStatusPublished, CreatedAt: now.Add(-3 * time.Minute)},
					},
				},
			},
			wantPostIDs:    []int64{9, 7},
			wantHasMore:    true,
			wantNextCursor: 7,
			wantRepoLimit:  3,
		},
		{
			name: "success with last post id cursor",
			req:  GetUserPostsRequest{UserID: 1001, LastPostID: 7, Limit: 2},
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {ID: 1001, Username: "alice"},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{
				postsByUser: map[int64][]*model.Post{
					1001: {
						{ID: 9, UserID: 1001, Content: "p9", Status: model.PostStatusPublished, CreatedAt: now},
						{ID: 7, UserID: 1001, Content: "p7", Status: model.PostStatusPublished, CreatedAt: now.Add(-1 * time.Minute)},
						{ID: 6, UserID: 1001, Content: "p6", Status: model.PostStatusPublished, CreatedAt: now.Add(-2 * time.Minute)},
						{ID: 5, UserID: 1001, Content: "p5", Status: model.PostStatusPublished, CreatedAt: now.Add(-3 * time.Minute)},
					},
				},
			},
			wantPostIDs:    []int64{6, 5},
			wantHasMore:    false,
			wantNextCursor: 5,
			wantRepoLimit:  3,
			wantRepoCursor: 7,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewUserService(tc.userRepo)
			if tc.postRepo != nil {
				svc = svc.WithPostRepository(tc.postRepo)
			}

			got, gotErr := svc.GetUserPosts(ctx, tc.req)
			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}

			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("expected nil result on error, got=%+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil result on success")
			}
			if tc.postRepo.gotUserID != tc.req.UserID {
				t.Fatalf("unexpected repo user id: got=%d want=%d", tc.postRepo.gotUserID, tc.req.UserID)
			}
			if tc.postRepo.gotLastPostID != tc.wantRepoCursor {
				t.Fatalf("unexpected repo cursor: got=%d want=%d", tc.postRepo.gotLastPostID, tc.wantRepoCursor)
			}
			if tc.postRepo.gotLimit != tc.wantRepoLimit {
				t.Fatalf("unexpected repo limit: got=%d want=%d", tc.postRepo.gotLimit, tc.wantRepoLimit)
			}

			gotPostIDs := make([]int64, 0, len(got.Items))
			for _, item := range got.Items {
				gotPostIDs = append(gotPostIDs, item.PostID)
			}
			if len(gotPostIDs) != len(tc.wantPostIDs) {
				t.Fatalf("unexpected item count: got=%d want=%d", len(gotPostIDs), len(tc.wantPostIDs))
			}
			for i := range gotPostIDs {
				if gotPostIDs[i] != tc.wantPostIDs[i] {
					t.Fatalf("unexpected post ids: got=%v want=%v", gotPostIDs, tc.wantPostIDs)
				}
			}
			if got.HasMore != tc.wantHasMore {
				t.Fatalf("unexpected has_more: got=%v want=%v", got.HasMore, tc.wantHasMore)
			}
			if got.NextCursor != tc.wantNextCursor {
				t.Fatalf("unexpected next cursor: got=%d want=%d", got.NextCursor, tc.wantNextCursor)
			}
		})
	}
}
