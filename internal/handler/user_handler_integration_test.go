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
	user *model.User
	err  error
}

func (r *fakeUserReadRepo) GetByID(_ context.Context, _ int64) (*model.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.user, nil
}

type fakeUserPostRepo struct {
	postsByUser map[int64][]*model.Post
	err         error
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
	})
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
