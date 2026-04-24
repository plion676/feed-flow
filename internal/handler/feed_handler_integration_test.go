package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/middleware"
	"github.com/plion676/feed-flow/internal/model"
	jwtpkg "github.com/plion676/feed-flow/internal/pkg/jwt"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/service"
)

type fakeFeedFollowRepoForHandler struct {
	followingByUser map[int64][]int64
}

func (r *fakeFeedFollowRepoForHandler) ListFollowingUserIDs(_ context.Context, userID int64) ([]int64, error) {
	ids := r.followingByUser[userID]
	out := make([]int64, len(ids))
	copy(out, ids)
	return out, nil
}

type fakeFeedPostRepoForHandler struct {
	posts []*model.Post
}

func (r *fakeFeedPostRepoForHandler) ListByUserIDs(_ context.Context, userIDs []int64, lastPostID int64, limit int) ([]*model.Post, error) {
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
		if len(filtered) >= limit {
			break
		}
	}

	return filtered, nil
}

func (r *fakeFeedPostRepoForHandler) ListByIDs(_ context.Context, postIDs []int64) ([]*model.Post, error) {
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

type feedAPIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestFeedGetHomeFeedSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "feed-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	now := time.Date(2026, 4, 22, 11, 0, 0, 0, time.UTC)
	followRepo := &fakeFeedFollowRepoForHandler{
		followingByUser: map[int64][]int64{
			1001: {1002},
		},
	}
	postRepo := &fakeFeedPostRepoForHandler{
		posts: []*model.Post{
			{ID: 10, UserID: 1002, Content: "p10", Status: 1, CreatedAt: now.Add(-1 * time.Minute)},
			{ID: 9, UserID: 1001, Content: "p9", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
			{ID: 8, UserID: 1002, Content: "p8", Status: 1, CreatedAt: now.Add(-3 * time.Minute)},
			{ID: 7, UserID: 1001, Content: "p7", Status: 1, CreatedAt: now.Add(-4 * time.Minute)},
		},
	}

	feedService := service.NewFeedService(followRepo, postRepo)
	feedHandler := NewFeedHandler(feedService)

	router := gin.New()
	router.GET("/api/v1/feed", middleware.AuthJWT(jwtManager), feedHandler.GetHomeFeed)

	token, err := jwtManager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed?limit=3", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusOK)
	}

	var body feedAPIResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeOK {
		t.Fatalf("unexpected response code: got=%d want=%d", body.Code, xerror.CodeOK)
	}

	var result service.FeedResult
	if err := json.Unmarshal(body.Data, &result); err != nil {
		t.Fatalf("unmarshal data failed: %v", err)
	}

	if len(result.Items) != 3 {
		t.Fatalf("unexpected item length: got=%d want=%d", len(result.Items), 3)
	}
	if result.Items[0].PostID != 10 || result.Items[1].PostID != 9 || result.Items[2].PostID != 8 {
		t.Fatalf("unexpected feed order: got=%+v", result.Items)
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true when there is one more record")
	}
	if result.NextCursor != 8 {
		t.Fatalf("unexpected next cursor: got=%d want=%d", result.NextCursor, 8)
	}
}

func TestFeedGetHomeFeedUnauthorized(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "feed-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	feedService := service.NewFeedService(
		&fakeFeedFollowRepoForHandler{followingByUser: map[int64][]int64{}},
		&fakeFeedPostRepoForHandler{},
	)
	feedHandler := NewFeedHandler(feedService)

	router := gin.New()
	router.GET("/api/v1/feed", middleware.AuthJWT(jwtManager), feedHandler.GetHomeFeed)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed?limit=3", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusUnauthorized)
	}

	var body feedAPIResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeUnauthorized {
		t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeUnauthorized)
	}
}

func TestFeedGetHomeFeedBadQueryParams(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "feed-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	feedService := service.NewFeedService(
		&fakeFeedFollowRepoForHandler{followingByUser: map[int64][]int64{}},
		&fakeFeedPostRepoForHandler{},
	)
	feedHandler := NewFeedHandler(feedService)

	router := gin.New()
	router.GET("/api/v1/feed", middleware.AuthJWT(jwtManager), feedHandler.GetHomeFeed)

	token, err := jwtManager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	testURLs := []string{
		"/api/v1/feed?limit=0",
		"/api/v1/feed?limit=-1",
		"/api/v1/feed?limit=abc",
		"/api/v1/feed?last_post_id=-1",
		"/api/v1/feed?last_post_id=abc",
	}

	for _, url := range testURLs {
		url := url
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("Authorization", "Bearer "+token)

			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusBadRequest)
			}

			var body feedAPIResponse
			if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
				t.Fatalf("unmarshal response failed: %v", err)
			}
			if body.Code != xerror.CodeBadRequest {
				t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeBadRequest)
			}
		})
	}
}
