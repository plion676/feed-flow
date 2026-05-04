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

func (r *fakeFeedPostRepoForHandler) ListPublished(_ context.Context, lastPostID int64, limit int) ([]*model.Post, error) {
	filtered := make([]*model.Post, 0, len(r.posts))
	for _, p := range r.posts {
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

func TestFeedGetDiscoverFeedSuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "feed-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	feedService := service.NewFeedService(
		&fakeFeedFollowRepoForHandler{followingByUser: map[int64][]int64{}},
		&fakeFeedPostRepoForHandler{
			posts: []*model.Post{
				{ID: 12, UserID: 1003, Content: "p12", Status: 1, CreatedAt: now},
				{ID: 11, UserID: 1002, Content: "p11", Status: 1, CreatedAt: now.Add(-time.Minute)},
				{ID: 10, UserID: 1001, Content: "p10", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
			},
		},
	)
	feedHandler := NewFeedHandler(feedService)

	router := gin.New()
	router.GET("/api/v1/feed/discover", middleware.AuthJWT(jwtManager), feedHandler.GetDiscoverFeed)

	token, err := jwtManager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed/discover?limit=2", nil)
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
	if len(result.Items) != 2 {
		t.Fatalf("unexpected item length: got=%d want=%d", len(result.Items), 2)
	}
	if result.Items[0].PostID != 12 || result.Items[1].PostID != 11 {
		t.Fatalf("unexpected discover order: got=%+v", result.Items)
	}
	if !result.HasMore {
		t.Fatal("expected has_more=true")
	}
	if result.NextCursor != 11 {
		t.Fatalf("unexpected next cursor: got=%d want=%d", result.NextCursor, 11)
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
		"/api/v1/feed?last_post_id=9&cursor=abc",
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

func TestFeedGetHomeFeedRefreshQuery(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "feed-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	now := time.Date(2026, 5, 4, 11, 0, 0, 0, time.UTC)
	followRepo := &fakeFeedFollowRepoForHandler{
		followingByUser: map[int64][]int64{
			1001: {1002},
		},
	}
	postRepo := &fakeFeedPostRepoForHandler{
		posts: []*model.Post{
			{ID: 12, UserID: 1002, Content: "p12", Status: 1, CreatedAt: now},
			{ID: 11, UserID: 1001, Content: "p11", Status: 1, CreatedAt: now.Add(-time.Minute)},
			{ID: 10, UserID: 1002, Content: "p10", Status: 1, CreatedAt: now.Add(-2 * time.Minute)},
		},
	}
	exposureRepo := &serviceFeedExposureRepoForHandler{
		seenPostIDs: map[int64]map[int64]struct{}{
			1001: {
				12: {},
				11: {},
				10: {},
			},
		},
	}

	feedService := service.NewFeedService(followRepo, postRepo).WithExposure(exposureRepo, service.FeedExposureOptions{})
	feedHandler := NewFeedHandler(feedService)

	router := gin.New()
	router.GET("/api/v1/feed", middleware.AuthJWT(jwtManager), feedHandler.GetHomeFeed)

	token, err := jwtManager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed?limit=3&refresh=1", nil)
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
	if result.Items[0].PostID != 12 || result.Items[1].PostID != 11 || result.Items[2].PostID != 10 {
		t.Fatalf("unexpected refresh fallback order: got=%+v", result.Items)
	}
	if result.FallbackMode != "latest" {
		t.Fatalf("unexpected fallback mode: got=%q want=%q", result.FallbackMode, "latest")
	}
}

type serviceFeedExposureRepoForHandler struct {
	seenPostIDs map[int64]map[int64]struct{}
}

func (r *serviceFeedExposureRepoForHandler) FilterUnseenPostIDs(_ context.Context, userID int64, postIDs []int64, _ time.Duration) ([]int64, error) {
	userSeen := r.seenPostIDs[userID]
	filtered := make([]int64, 0, len(postIDs))
	for _, postID := range postIDs {
		if _, ok := userSeen[postID]; ok {
			continue
		}
		filtered = append(filtered, postID)
	}
	return filtered, nil
}

func (r *serviceFeedExposureRepoForHandler) MarkSeenPostIDs(_ context.Context, userID int64, postIDs []int64, _ time.Duration) error {
	if r.seenPostIDs == nil {
		r.seenPostIDs = make(map[int64]map[int64]struct{})
	}
	if r.seenPostIDs[userID] == nil {
		r.seenPostIDs[userID] = make(map[int64]struct{})
	}
	for _, postID := range postIDs {
		r.seenPostIDs[userID][postID] = struct{}{}
	}
	return nil
}
