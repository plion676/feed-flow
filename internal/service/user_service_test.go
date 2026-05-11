package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type fakeUserReadRepoForService struct {
	users       map[int64]*model.User
	getByIDsErr error
	err         error
}

func (r *fakeUserReadRepoForService) GetByID(_ context.Context, userID int64) (*model.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.users[userID], nil
}

func (r *fakeUserReadRepoForService) GetByIDs(_ context.Context, userIDs []int64) ([]*model.User, error) {
	if r.getByIDsErr != nil {
		return nil, r.getByIDsErr
	}

	items := make([]*model.User, 0, len(userIDs))
	for _, userID := range userIDs {
		if user := r.users[userID]; user != nil {
			items = append(items, user)
		}
	}
	return items, nil
}

type fakeUserPostReadRepoForService struct {
	postsByUser   map[int64][]*model.Post
	err           error
	countErr      error
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

func (r *fakeUserPostReadRepoForService) CountPublishedByUserID(_ context.Context, userID int64) (int64, error) {
	r.gotUserID = userID
	if r.countErr != nil {
		return 0, r.countErr
	}

	var count int64
	for _, post := range r.postsByUser[userID] {
		if post.Status == model.PostStatusPublished {
			count++
		}
	}

	return count, nil
}

type fakeUserFollowReadRepoForService struct {
	followingCountByUser     map[int64]int64
	followerCountByUser      map[int64]int64
	followExistsByPair       map[string]bool
	followingUserIDsByUser   map[int64][]int64
	followerUserIDsByUser    map[int64][]int64
	followingRelationsByUser map[int64][]*model.Follow
	followerRelationsByUser  map[int64][]*model.Follow
	err                      error
	gotFollowingUserID       int64
	gotFollowerUserID        int64
	gotExistsUserID          int64
	gotExistsTargetID        int64
}

type fakeUserCountReadRepoForService struct {
	counts    map[int64]*model.UserCount
	err       error
	gotUserID int64
	called    int
}

func (r *fakeUserFollowReadRepoForService) CountFollowing(_ context.Context, userID int64) (int64, error) {
	r.gotFollowingUserID = userID
	if r.err != nil {
		return 0, r.err
	}
	return r.followingCountByUser[userID], nil
}

func (r *fakeUserFollowReadRepoForService) CountFollowers(_ context.Context, userID int64) (int64, error) {
	r.gotFollowerUserID = userID
	if r.err != nil {
		return 0, r.err
	}
	return r.followerCountByUser[userID], nil
}

func (r *fakeUserFollowReadRepoForService) Exists(_ context.Context, userID int64, targetUserID int64) (bool, error) {
	r.gotExistsUserID = userID
	r.gotExistsTargetID = targetUserID
	if r.err != nil {
		return false, r.err
	}
	return r.followExistsByPair[followExistsKey(userID, targetUserID)], nil
}

func (r *fakeUserCountReadRepoForService) GetByUserID(_ context.Context, userID int64) (*model.UserCount, error) {
	r.called++
	r.gotUserID = userID
	if r.err != nil {
		return nil, r.err
	}
	return r.counts[userID], nil
}

func (r *fakeUserFollowReadRepoForService) ListFollowingUserIDs(_ context.Context, userID int64) ([]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	ids := r.followingUserIDsByUser[userID]
	out := make([]int64, len(ids))
	copy(out, ids)
	return out, nil
}

func (r *fakeUserFollowReadRepoForService) ListFollowerUserIDs(_ context.Context, targetUserID int64) ([]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	ids := r.followerUserIDsByUser[targetUserID]
	out := make([]int64, len(ids))
	copy(out, ids)
	return out, nil
}

func (r *fakeUserFollowReadRepoForService) ListFollowingRelations(_ context.Context, userID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return filterFakeFollowRelations(r.followingRelationsByUser[userID], lastFollowID, limit), nil
}

func (r *fakeUserFollowReadRepoForService) ListFollowerRelations(_ context.Context, targetUserID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return filterFakeFollowRelations(r.followerRelationsByUser[targetUserID], lastFollowID, limit), nil
}

func filterFakeFollowRelations(items []*model.Follow, lastFollowID int64, limit int) []*model.Follow {
	filtered := make([]*model.Follow, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		if lastFollowID > 0 && item.ID >= lastFollowID {
			continue
		}
		filtered = append(filtered, item)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func followExistsKey(userID int64, targetUserID int64) string {
	return fmt.Sprintf("%d:%d", userID, targetUserID)
}

func TestUserServiceGetUserProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name               string
		userID             int64
		userRepo           *fakeUserReadRepoForService
		postRepo           *fakeUserPostReadRepoForService
		followRepo         *fakeUserFollowReadRepoForService
		userCountRepo      *fakeUserCountReadRepoForService
		viewerUserID       int64
		wantErr            *xerror.Error
		wantFollowingCount int64
		wantFollowerCount  int64
		wantPostCount      int64
		wantIsFollowing    bool
		wantCheckFollow    bool
	}{
		{
			name:       "bad request when user id invalid",
			userID:     0,
			userRepo:   &fakeUserReadRepoForService{},
			postRepo:   &fakeUserPostReadRepoForService{},
			followRepo: &fakeUserFollowReadRepoForService{},
			wantErr:    xerror.ErrBadRequest,
		},
		{
			name:         "bad request when viewer user id invalid",
			userID:       1001,
			viewerUserID: -1,
			userRepo:     &fakeUserReadRepoForService{},
			postRepo:     &fakeUserPostReadRepoForService{},
			followRepo:   &fakeUserFollowReadRepoForService{},
			wantErr:      xerror.ErrBadRequest,
		},
		{
			name:       "internal error when post repo is not configured",
			userID:     1001,
			userRepo:   &fakeUserReadRepoForService{},
			postRepo:   nil,
			followRepo: &fakeUserFollowReadRepoForService{},
			wantErr:    xerror.ErrInternal,
		},
		{
			name:       "internal error when follow repo is not configured",
			userID:     1001,
			userRepo:   &fakeUserReadRepoForService{},
			postRepo:   &fakeUserPostReadRepoForService{},
			followRepo: nil,
			wantErr:    xerror.ErrInternal,
		},
		{
			name:   "internal error when user repo fails",
			userID: 1001,
			userRepo: &fakeUserReadRepoForService{
				err: errors.New("query user failed"),
			},
			postRepo:   &fakeUserPostReadRepoForService{},
			followRepo: &fakeUserFollowReadRepoForService{},
			wantErr:    xerror.ErrInternal,
		},
		{
			name:   "not found when user missing",
			userID: 1001,
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{},
			},
			postRepo:   &fakeUserPostReadRepoForService{},
			followRepo: &fakeUserFollowReadRepoForService{},
			wantErr:    xerror.ErrNotFound,
		},
		{
			name:   "internal error when following count fails",
			userID: 1001,
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {ID: 1001, Username: "alice"},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{},
			followRepo: &fakeUserFollowReadRepoForService{
				err: errors.New("count follow failed"),
			},
			wantErr: xerror.ErrInternal,
		},
		{
			name:   "internal error when user count repo fails",
			userID: 1001,
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {ID: 1001, Username: "alice"},
				},
			},
			postRepo:   &fakeUserPostReadRepoForService{},
			followRepo: &fakeUserFollowReadRepoForService{},
			userCountRepo: &fakeUserCountReadRepoForService{
				err: errors.New("query user count failed"),
			},
			wantErr: xerror.ErrInternal,
		},
		{
			name:   "internal error when post count fails",
			userID: 1001,
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {ID: 1001, Username: "alice"},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{
				countErr: errors.New("count post failed"),
			},
			followRepo: &fakeUserFollowReadRepoForService{
				followingCountByUser: map[int64]int64{1001: 3},
				followerCountByUser:  map[int64]int64{1001: 8},
			},
			wantErr: xerror.ErrInternal,
		},
		{
			name:   "success",
			userID: 1001,
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {
						ID:       1001,
						Username: "alice",
						Nickname: "Alice",
						Avatar:   "https://example.com/a.png",
						Bio:      "hello",
					},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{
				postsByUser: map[int64][]*model.Post{
					1001: {
						{ID: 9, UserID: 1001, Status: model.PostStatusPublished},
						{ID: 8, UserID: 1001, Status: model.PostStatusDeleted},
						{ID: 7, UserID: 1001, Status: model.PostStatusPublished},
					},
				},
			},
			followRepo: &fakeUserFollowReadRepoForService{
				followingCountByUser: map[int64]int64{1001: 4},
				followerCountByUser:  map[int64]int64{1001: 9},
				followExistsByPair:   map[string]bool{},
			},
			wantFollowingCount: 4,
			wantFollowerCount:  9,
			wantPostCount:      2,
		},
		{
			name:   "success prefers user counts snapshot",
			userID: 1001,
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {
						ID:       1001,
						Username: "alice",
						Nickname: "Alice",
					},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{
				postsByUser: map[int64][]*model.Post{
					1001: {
						{ID: 9, UserID: 1001, Status: model.PostStatusPublished},
					},
				},
			},
			followRepo: &fakeUserFollowReadRepoForService{
				followingCountByUser: map[int64]int64{1001: 999},
				followerCountByUser:  map[int64]int64{1001: 999},
			},
			userCountRepo: &fakeUserCountReadRepoForService{
				counts: map[int64]*model.UserCount{
					1001: {
						UserID:         1001,
						FollowingCount: 6,
						FollowerCount:  12,
						PostCount:      3,
					},
				},
			},
			wantFollowingCount: 6,
			wantFollowerCount:  12,
			wantPostCount:      3,
		},
		{
			name:         "success with viewer following author",
			userID:       1001,
			viewerUserID: 2002,
			userRepo: &fakeUserReadRepoForService{
				users: map[int64]*model.User{
					1001: {
						ID:       1001,
						Username: "alice",
						Nickname: "Alice",
					},
				},
			},
			postRepo: &fakeUserPostReadRepoForService{
				postsByUser: map[int64][]*model.Post{
					1001: {
						{ID: 9, UserID: 1001, Status: model.PostStatusPublished},
					},
				},
			},
			followRepo: &fakeUserFollowReadRepoForService{
				followingCountByUser: map[int64]int64{1001: 4},
				followerCountByUser:  map[int64]int64{1001: 9},
				followExistsByPair: map[string]bool{
					followExistsKey(2002, 1001): true,
				},
			},
			wantFollowingCount: 4,
			wantFollowerCount:  9,
			wantPostCount:      1,
			wantIsFollowing:    true,
			wantCheckFollow:    true,
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
			if tc.followRepo != nil {
				svc = svc.WithFollowRepository(tc.followRepo)
			}
			if tc.userCountRepo != nil {
				svc = svc.WithUserCountRepository(tc.userCountRepo)
			}

			got, gotErr := svc.GetUserProfile(ctx, GetUserProfileRequest{
				UserID:       tc.userID,
				ViewerUserID: tc.viewerUserID,
			})
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
			if got.UserID != tc.userID {
				t.Fatalf("unexpected user id: got=%d want=%d", got.UserID, tc.userID)
			}
			if got.FollowingCount != tc.wantFollowingCount {
				t.Fatalf("unexpected following count: got=%d want=%d", got.FollowingCount, tc.wantFollowingCount)
			}
			if got.FollowerCount != tc.wantFollowerCount {
				t.Fatalf("unexpected follower count: got=%d want=%d", got.FollowerCount, tc.wantFollowerCount)
			}
			if got.PostCount != tc.wantPostCount {
				t.Fatalf("unexpected post count: got=%d want=%d", got.PostCount, tc.wantPostCount)
			}
			if got.IsFollowing != tc.wantIsFollowing {
				t.Fatalf("unexpected is_following: got=%v want=%v", got.IsFollowing, tc.wantIsFollowing)
			}
			if tc.followRepo.gotFollowingUserID != tc.userID || tc.followRepo.gotFollowerUserID != tc.userID {
				if tc.userCountRepo == nil || tc.userCountRepo.counts[tc.userID] == nil {
					t.Fatalf("unexpected follow repo user ids: following=%d follower=%d want=%d", tc.followRepo.gotFollowingUserID, tc.followRepo.gotFollowerUserID, tc.userID)
				}
			}
			if tc.postRepo.gotUserID != tc.userID {
				if tc.userCountRepo == nil || tc.userCountRepo.counts[tc.userID] == nil {
					t.Fatalf("unexpected post repo user id: got=%d want=%d", tc.postRepo.gotUserID, tc.userID)
				}
			}
			if tc.userCountRepo != nil && tc.userCountRepo.called > 0 && tc.userCountRepo.gotUserID != tc.userID {
				t.Fatalf("unexpected user count repo user id: got=%d want=%d", tc.userCountRepo.gotUserID, tc.userID)
			}
			if tc.wantCheckFollow {
				if tc.followRepo.gotExistsUserID != tc.viewerUserID || tc.followRepo.gotExistsTargetID != tc.userID {
					t.Fatalf("unexpected exists check pair: got=%d->%d want=%d->%d", tc.followRepo.gotExistsUserID, tc.followRepo.gotExistsTargetID, tc.viewerUserID, tc.userID)
				}
			}
		})
	}
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

func TestUserServiceGetUserFollowLists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("get following success", func(t *testing.T) {
		t.Parallel()

		userRepo := &fakeUserReadRepoForService{
			users: map[int64]*model.User{
				1001: {ID: 1001, Username: "alice", Nickname: "Alice"},
				1002: {ID: 1002, Username: "bob", Nickname: "Bob", Bio: "backend"},
				1003: {ID: 1003, Username: "cathy", Nickname: "Cathy"},
			},
		}
		followRepo := &fakeUserFollowReadRepoForService{
			followingRelationsByUser: map[int64][]*model.Follow{
				1001: {
					{ID: 9, UserID: 1001, TargetUserID: 1002},
					{ID: 8, UserID: 1001, TargetUserID: 1003},
					{ID: 7, UserID: 1001, TargetUserID: 1004},
				},
			},
		}

		svc := NewUserService(userRepo).WithFollowRepository(followRepo)
		got, gotErr := svc.GetUserFollowing(ctx, UserFollowListRequest{UserID: 1001, Limit: 2})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 2 {
			t.Fatalf("unexpected result: %+v", got)
		}
		if got.Items[0].UserID != 1002 || got.Items[1].UserID != 1003 {
			t.Fatalf("unexpected user order: %+v", got.Items)
		}
		if got.Items[0].IsFollowing || got.Items[1].IsFollowing {
			t.Fatalf("guest follow state should be false: %+v", got.Items)
		}
		if !got.HasMore || got.NextCursor != 8 {
			t.Fatalf("unexpected pagination: %+v", got)
		}
	})

	t.Run("get followers success", func(t *testing.T) {
		t.Parallel()

		userRepo := &fakeUserReadRepoForService{
			users: map[int64]*model.User{
				1001: {ID: 1001, Username: "alice", Nickname: "Alice"},
				2001: {ID: 2001, Username: "tom", Nickname: "Tom"},
				2002: {ID: 2002, Username: "jerry", Nickname: "Jerry"},
			},
		}
		followRepo := &fakeUserFollowReadRepoForService{
			followerRelationsByUser: map[int64][]*model.Follow{
				1001: {
					{ID: 11, UserID: 2001, TargetUserID: 1001},
					{ID: 10, UserID: 2002, TargetUserID: 1001},
				},
			},
		}

		svc := NewUserService(userRepo).WithFollowRepository(followRepo)
		got, gotErr := svc.GetUserFollowers(ctx, UserFollowListRequest{UserID: 1001})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 2 {
			t.Fatalf("unexpected result: %+v", got)
		}
		if got.Items[0].UserID != 2001 || got.Items[1].UserID != 2002 {
			t.Fatalf("unexpected user order: %+v", got.Items)
		}
		if got.Items[0].IsFollowing || got.Items[1].IsFollowing {
			t.Fatalf("guest follow state should be false: %+v", got.Items)
		}
		if got.HasMore || got.NextCursor != 10 {
			t.Fatalf("unexpected pagination: %+v", got)
		}
	})

	t.Run("get followers with cursor success", func(t *testing.T) {
		t.Parallel()

		userRepo := &fakeUserReadRepoForService{
			users: map[int64]*model.User{
				1001: {ID: 1001, Username: "alice", Nickname: "Alice"},
				2002: {ID: 2002, Username: "jerry", Nickname: "Jerry"},
				2003: {ID: 2003, Username: "rose", Nickname: "Rose"},
			},
		}
		followRepo := &fakeUserFollowReadRepoForService{
			followerRelationsByUser: map[int64][]*model.Follow{
				1001: {
					{ID: 11, UserID: 2001, TargetUserID: 1001},
					{ID: 10, UserID: 2002, TargetUserID: 1001},
					{ID: 9, UserID: 2003, TargetUserID: 1001},
				},
			},
		}

		svc := NewUserService(userRepo).WithFollowRepository(followRepo)
		got, gotErr := svc.GetUserFollowers(ctx, UserFollowListRequest{UserID: 1001, LastFollowID: 10, Limit: 2})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 1 {
			t.Fatalf("unexpected result: %+v", got)
		}
		if got.Items[0].UserID != 2003 {
			t.Fatalf("unexpected user order: %+v", got.Items)
		}
		if got.HasMore || got.NextCursor != 9 {
			t.Fatalf("unexpected pagination: %+v", got)
		}
	})

	t.Run("get following with viewer follow state", func(t *testing.T) {
		t.Parallel()

		userRepo := &fakeUserReadRepoForService{
			users: map[int64]*model.User{
				1001: {ID: 1001, Username: "alice", Nickname: "Alice"},
				1002: {ID: 1002, Username: "bob", Nickname: "Bob"},
				1003: {ID: 1003, Username: "cathy", Nickname: "Cathy"},
			},
		}
		followRepo := &fakeUserFollowReadRepoForService{
			followingRelationsByUser: map[int64][]*model.Follow{
				1001: {
					{ID: 9, UserID: 1001, TargetUserID: 1002},
					{ID: 8, UserID: 1001, TargetUserID: 1003},
				},
			},
			followExistsByPair: map[string]bool{
				followExistsKey(3001, 1002): true,
			},
		}

		svc := NewUserService(userRepo).WithFollowRepository(followRepo)
		got, gotErr := svc.GetUserFollowing(ctx, UserFollowListRequest{
			UserID:       1001,
			ViewerUserID: 3001,
			Limit:        2,
		})
		if gotErr != nil {
			t.Fatalf("unexpected error: %v", gotErr)
		}
		if got == nil || len(got.Items) != 2 {
			t.Fatalf("unexpected result: %+v", got)
		}
		if !got.Items[0].IsFollowing || got.Items[1].IsFollowing {
			t.Fatalf("unexpected follow state payload: %+v", got.Items)
		}
	})

	t.Run("bad request when user id invalid", func(t *testing.T) {
		t.Parallel()

		svc := NewUserService(&fakeUserReadRepoForService{}).WithFollowRepository(&fakeUserFollowReadRepoForService{})
		got, gotErr := svc.GetUserFollowers(ctx, UserFollowListRequest{UserID: 0})
		if gotErr != xerror.ErrBadRequest {
			t.Fatalf("unexpected error: got=%v want=%v", gotErr, xerror.ErrBadRequest)
		}
		if got != nil {
			t.Fatalf("expected nil result on error, got=%+v", got)
		}
	})
}
