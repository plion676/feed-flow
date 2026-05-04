package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/middleware"
	"github.com/plion676/feed-flow/internal/model"
	jwtpkg "github.com/plion676/feed-flow/internal/pkg/jwt"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/service"
)

type fakeUserReadRepo struct {
	user     *model.User
	usersByID map[int64]*model.User
	err      error
}

func (r *fakeUserReadRepo) GetByID(_ context.Context, userID int64) (*model.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.usersByID != nil {
		return r.usersByID[userID], nil
	}
	return r.user, nil
}

func (r *fakeUserReadRepo) GetByIDs(_ context.Context, userIDs []int64) ([]*model.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	items := make([]*model.User, 0, len(userIDs))
	for _, userID := range userIDs {
		if r.usersByID != nil {
			if user := r.usersByID[userID]; user != nil {
				items = append(items, user)
			}
			continue
		}
		if r.user != nil && r.user.ID == userID {
			items = append(items, r.user)
		}
	}
	return items, nil
}

type fakeUserPostRepo struct {
	postsByUser map[int64][]*model.Post
	err         error
	countErr    error
}

func (r *fakeUserPostRepo) ListByUserID(_ context.Context, userID int64, lastPostID int64, limit int) ([]*model.Post, error) {
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

func (r *fakeUserPostRepo) CountPublishedByUserID(_ context.Context, userID int64) (int64, error) {
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

type fakeUserFollowRepo struct {
	followingCountByUser map[int64]int64
	followerCountByUser  map[int64]int64
	followExistsByPair   map[string]bool
	followingUserIDsByUser map[int64][]int64
	followerUserIDsByUser  map[int64][]int64
	followingRelationsByUser map[int64][]*model.Follow
	followerRelationsByUser  map[int64][]*model.Follow
	err                  error
}

func (r *fakeUserFollowRepo) CountFollowing(_ context.Context, userID int64) (int64, error) {
	if r.err != nil {
		return 0, r.err
	}
	return r.followingCountByUser[userID], nil
}

func (r *fakeUserFollowRepo) CountFollowers(_ context.Context, userID int64) (int64, error) {
	if r.err != nil {
		return 0, r.err
	}
	return r.followerCountByUser[userID], nil
}

func (r *fakeUserFollowRepo) Exists(_ context.Context, userID int64, targetUserID int64) (bool, error) {
	if r.err != nil {
		return false, r.err
	}
	return r.followExistsByPair[handlerFollowExistsKey(userID, targetUserID)], nil
}

func (r *fakeUserFollowRepo) ListFollowingUserIDs(_ context.Context, userID int64) ([]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	ids := r.followingUserIDsByUser[userID]
	out := make([]int64, len(ids))
	copy(out, ids)
	return out, nil
}

func (r *fakeUserFollowRepo) ListFollowerUserIDs(_ context.Context, targetUserID int64) ([]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	ids := r.followerUserIDsByUser[targetUserID]
	out := make([]int64, len(ids))
	copy(out, ids)
	return out, nil
}

func (r *fakeUserFollowRepo) ListFollowingRelations(_ context.Context, userID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return filterHandlerFakeFollowRelations(r.followingRelationsByUser[userID], lastFollowID, limit), nil
}

func (r *fakeUserFollowRepo) ListFollowerRelations(_ context.Context, targetUserID int64, lastFollowID int64, limit int) ([]*model.Follow, error) {
	if r.err != nil {
		return nil, r.err
	}
	return filterHandlerFakeFollowRelations(r.followerRelationsByUser[targetUserID], lastFollowID, limit), nil
}

func filterHandlerFakeFollowRelations(items []*model.Follow, lastFollowID int64, limit int) []*model.Follow {
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

func handlerFollowExistsKey(userID int64, targetUserID int64) string {
	return strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(targetUserID, 10)
}

type meResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestUserMeWithAuthMiddleware(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	userService := service.NewUserService(&fakeUserReadRepo{
		user: &model.User{
			ID:       1001,
			Username: "alice",
			Nickname: "Alice",
			Avatar:   "https://example.com/avatar.png",
			Bio:      "hello",
		},
	})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/me", middleware.AuthJWT(jwtManager), userHandler.Me)

	token, err := jwtManager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("unexpected response code: got=%d want=0", body.Code)
	}

	var me service.MeResult
	if err := json.Unmarshal(body.Data, &me); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if me.UserID != 1001 || me.Username != "alice" || me.Nickname != "Alice" {
		t.Fatalf("unexpected me payload: %+v", me)
	}
}

func TestUserGetByIDSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{
		user: &model.User{
			ID:       1002,
			Username: "bob",
			Nickname: "Bob",
			Avatar:   "https://example.com/bob.png",
			Bio:      "hello bob",
		},
	}).
		WithPostRepository(&fakeUserPostRepo{
			postsByUser: map[int64][]*model.Post{
				1002: {
					{ID: 3, UserID: 1002, Status: model.PostStatusPublished},
					{ID: 2, UserID: 1002, Status: model.PostStatusDeleted},
					{ID: 1, UserID: 1002, Status: model.PostStatusPublished},
				},
			},
		}).
		WithFollowRepository(&fakeUserFollowRepo{
			followingCountByUser: map[int64]int64{1002: 11},
			followerCountByUser:  map[int64]int64{1002: 23},
		})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id", userHandler.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/1002", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeOK {
		t.Fatalf("unexpected response code: got=%d want=%d", body.Code, xerror.CodeOK)
	}

	var result service.UserProfileResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if result.UserID != 1002 || result.Username != "bob" || result.Nickname != "Bob" {
		t.Fatalf("unexpected profile identity: %+v", result)
	}
	if result.FollowingCount != 11 || result.FollowerCount != 23 || result.PostCount != 2 {
		t.Fatalf("unexpected profile counts: %+v", result)
	}
	if result.IsFollowing {
		t.Fatalf("guest request should not be following: %+v", result)
	}
}

func TestUserGetByIDWithOptionalAuthFollowing(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "user-profile-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	userService := service.NewUserService(&fakeUserReadRepo{
		user: &model.User{
			ID:       1002,
			Username: "bob",
			Nickname: "Bob",
		},
	}).
		WithPostRepository(&fakeUserPostRepo{
			postsByUser: map[int64][]*model.Post{
				1002: {
					{ID: 3, UserID: 1002, Status: model.PostStatusPublished},
				},
			},
		}).
		WithFollowRepository(&fakeUserFollowRepo{
			followingCountByUser: map[int64]int64{1002: 5},
			followerCountByUser:  map[int64]int64{1002: 6},
			followExistsByPair: map[string]bool{
				handlerFollowExistsKey(2001, 1002): true,
			},
		})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.Use(middleware.OptionalAuthJWT(jwtManager))
	router.GET("/api/v1/users/:id", userHandler.GetByID)

	token, err := jwtManager.GenerateToken(2001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/1002", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeOK {
		t.Fatalf("unexpected response code: got=%d want=%d", body.Code, xerror.CodeOK)
	}

	var result service.UserProfileResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if !result.IsFollowing {
		t.Fatalf("expected is_following=true, got %+v", result)
	}
}

func TestUserGetByIDBadParams(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{
		user: &model.User{ID: 1001, Username: "alice"},
	}).WithPostRepository(&fakeUserPostRepo{}).WithFollowRepository(&fakeUserFollowRepo{})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id", userHandler.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/abc", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusBadRequest)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeBadRequest {
		t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeBadRequest)
	}
}

func TestUserGetByIDNotFound(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{}).
		WithPostRepository(&fakeUserPostRepo{}).
		WithFollowRepository(&fakeUserFollowRepo{})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id", userHandler.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/999", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusNotFound)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeNotFound {
		t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeNotFound)
	}
}

func TestUserGetUserPostsSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	userService := service.NewUserService(&fakeUserReadRepo{
		user: &model.User{
			ID:       1002,
			Username: "bob",
			Nickname: "Bob",
		},
	}).WithPostRepository(&fakeUserPostRepo{
		postsByUser: map[int64][]*model.Post{
			1002: {
				{ID: 9, UserID: 1002, Content: "p9", Status: model.PostStatusPublished, CreatedAt: now},
				{ID: 8, UserID: 1002, Content: "p8", Status: model.PostStatusPublished, CreatedAt: now.Add(-1 * time.Minute)},
				{ID: 7, UserID: 1002, Content: "deleted", Status: model.PostStatusDeleted, CreatedAt: now.Add(-2 * time.Minute)},
				{ID: 6, UserID: 1002, Content: "p6", Status: model.PostStatusPublished, CreatedAt: now.Add(-3 * time.Minute)},
			},
		},
	}).WithFollowRepository(&fakeUserFollowRepo{})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id/posts", userHandler.GetUserPosts)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/1002/posts?limit=2", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeOK {
		t.Fatalf("unexpected response code: got=%d want=%d", body.Code, xerror.CodeOK)
	}

	var result service.UserPostsResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}

	if len(result.Items) != 2 {
		t.Fatalf("unexpected item length: got=%d want=%d", len(result.Items), 2)
	}
	if result.Items[0].PostID != 9 || result.Items[1].PostID != 8 {
		t.Fatalf("unexpected post order: %+v", result.Items)
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true when there is one more published post")
	}
	if result.NextCursor != 8 {
		t.Fatalf("unexpected next cursor: got=%d want=%d", result.NextCursor, 8)
	}
}

func TestUserGetUserPostsBadParams(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{
		user: &model.User{ID: 1001, Username: "alice"},
	}).WithPostRepository(&fakeUserPostRepo{})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id/posts", userHandler.GetUserPosts)

	testURLs := []string{
		"/api/v1/users/abc/posts",
		"/api/v1/users/1001/posts?last_post_id=-1",
		"/api/v1/users/1001/posts?last_post_id=abc",
		"/api/v1/users/1001/posts?limit=0",
		"/api/v1/users/1001/posts?limit=abc",
	}

	for _, url := range testURLs {
		url := url
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, url, nil)
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusBadRequest)
			}

			var body meResponse
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response failed: %v", err)
			}
			if body.Code != xerror.CodeBadRequest {
				t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeBadRequest)
			}
		})
	}
}

func TestUserGetUserPostsNotFound(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{}).WithPostRepository(&fakeUserPostRepo{})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id/posts", userHandler.GetUserPosts)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+strconv.FormatInt(999, 10)+"/posts", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusNotFound)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeNotFound {
		t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeNotFound)
	}
}

func TestUserGetFollowersSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{
		usersByID: map[int64]*model.User{
			1001: {ID: 1001, Username: "alice", Nickname: "Alice"},
			2001: {ID: 2001, Username: "tom", Nickname: "Tom", Bio: "follower one"},
			2002: {ID: 2002, Username: "jerry", Nickname: "Jerry", Bio: "follower two"},
			2003: {ID: 2003, Username: "rose", Nickname: "Rose", Bio: "follower three"},
		},
	}).WithFollowRepository(&fakeUserFollowRepo{
		followerRelationsByUser: map[int64][]*model.Follow{
			1001: {
				{ID: 9, UserID: 2001, TargetUserID: 1001},
				{ID: 8, UserID: 2002, TargetUserID: 1001},
				{ID: 7, UserID: 2003, TargetUserID: 1001},
			},
		},
	})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id/followers", userHandler.GetFollowers)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/1001/followers?limit=2", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeOK {
		t.Fatalf("unexpected response code: got=%d want=%d", body.Code, xerror.CodeOK)
	}

	var result service.UserFollowListResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].UserID != 2001 || result.Items[1].UserID != 2002 {
		t.Fatalf("unexpected follow list payload: %+v", result)
	}
	if !result.HasMore || result.NextCursor != 8 {
		t.Fatalf("unexpected pagination payload: %+v", result)
	}
}

func TestUserGetFollowingSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{
		usersByID: map[int64]*model.User{
			1001: {ID: 1001, Username: "alice", Nickname: "Alice"},
			1002: {ID: 1002, Username: "bob", Nickname: "Bob"},
			1003: {ID: 1003, Username: "cathy", Nickname: "Cathy"},
		},
	}).WithFollowRepository(&fakeUserFollowRepo{
		followingRelationsByUser: map[int64][]*model.Follow{
			1001: {
				{ID: 6, UserID: 1001, TargetUserID: 1002},
				{ID: 5, UserID: 1001, TargetUserID: 1003},
			},
		},
	})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id/following", userHandler.GetFollowing)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/1001/following?limit=2", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeOK {
		t.Fatalf("unexpected response code: got=%d want=%d", body.Code, xerror.CodeOK)
	}

	var result service.UserFollowListResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if len(result.Items) != 2 || result.Items[0].UserID != 1002 || result.Items[1].UserID != 1003 {
		t.Fatalf("unexpected follow list payload: %+v", result)
	}
	if result.HasMore || result.NextCursor != 5 {
		t.Fatalf("unexpected pagination payload: %+v", result)
	}
	if result.Items[0].IsFollowing || result.Items[1].IsFollowing {
		t.Fatalf("guest follow state should be false: %+v", result.Items)
	}
}

func TestUserGetFollowingWithOptionalAuthFollowState(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "follow-list-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	userService := service.NewUserService(&fakeUserReadRepo{
		usersByID: map[int64]*model.User{
			1001: {ID: 1001, Username: "alice", Nickname: "Alice"},
			1002: {ID: 1002, Username: "bob", Nickname: "Bob"},
			1003: {ID: 1003, Username: "cathy", Nickname: "Cathy"},
		},
	}).WithFollowRepository(&fakeUserFollowRepo{
		followingRelationsByUser: map[int64][]*model.Follow{
			1001: {
				{ID: 6, UserID: 1001, TargetUserID: 1002},
				{ID: 5, UserID: 1001, TargetUserID: 1003},
			},
		},
		followExistsByPair: map[string]bool{
			handlerFollowExistsKey(3001, 1002): true,
		},
	})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.Use(middleware.OptionalAuthJWT(jwtManager))
	router.GET("/api/v1/users/:id/following", userHandler.GetFollowing)

	token, err := jwtManager.GenerateToken(3001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/1001/following?limit=2", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body meResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeOK {
		t.Fatalf("unexpected response code: got=%d want=%d", body.Code, xerror.CodeOK)
	}

	var result service.UserFollowListResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}
	if !result.Items[0].IsFollowing || result.Items[1].IsFollowing {
		t.Fatalf("unexpected follow state payload: %+v", result.Items)
	}
}

func TestUserGetFollowListBadQuery(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	userService := service.NewUserService(&fakeUserReadRepo{
		user: &model.User{ID: 1001, Username: "alice"},
	}).WithFollowRepository(&fakeUserFollowRepo{})
	userHandler := NewUserHandler(userService)

	router := gin.New()
	router.GET("/api/v1/users/:id/followers", userHandler.GetFollowers)

	testURLs := []string{
		"/api/v1/users/1001/followers?last_follow_id=-1",
		"/api/v1/users/1001/followers?last_follow_id=abc",
		"/api/v1/users/1001/followers?limit=0",
		"/api/v1/users/1001/followers?limit=abc",
	}

	for _, url := range testURLs {
		url := url
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, url, nil)
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusBadRequest)
			}
		})
	}
}
