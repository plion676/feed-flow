package handler

import (
	"bytes"
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
	"gorm.io/gorm"
)

type fakePostRepoForHandler struct {
	posts  map[int64]*model.Post
	nextID int64
}

func (r *fakePostRepoForHandler) Create(_ context.Context, post *model.Post) error {
	if r.posts == nil {
		r.posts = make(map[int64]*model.Post)
	}
	if r.nextID <= 0 {
		r.nextID = 1
	}

	post.ID = r.nextID
	post.CreatedAt = time.Date(2026, 4, 20, 13, 0, 0, 0, time.UTC)
	post.UpdatedAt = post.CreatedAt

	copied := *post
	r.posts[post.ID] = &copied
	r.nextID++
	return nil
}

func (r *fakePostRepoForHandler) CreateTx(ctx context.Context, _ *gorm.DB, post *model.Post) error {
	return r.Create(ctx, post)
}

func (r *fakePostRepoForHandler) GetByID(_ context.Context, postID int64) (*model.Post, error) {
	if r.posts == nil {
		return nil, nil
	}
	post, exists := r.posts[postID]
	if !exists {
		return nil, nil
	}

	copied := *post
	return &copied, nil
}

func (r *fakePostRepoForHandler) SoftDeleteByIDAndUserID(_ context.Context, postID int64, userID int64) (bool, error) {
	if r.posts == nil {
		return false, nil
	}
	post, exists := r.posts[postID]
	if !exists || post.UserID != userID || post.Status != model.PostStatusPublished {
		return false, nil
	}
	post.Status = model.PostStatusDeleted
	post.UpdatedAt = time.Date(2026, 4, 20, 13, 30, 0, 0, time.UTC)
	return true, nil
}

func (r *fakePostRepoForHandler) SoftDeleteByIDAndUserIDTx(ctx context.Context, _ *gorm.DB, postID int64, userID int64) (bool, error) {
	return r.SoftDeleteByIDAndUserID(ctx, postID, userID)
}

type postAPIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestPostCreateAndGetByIDFlow(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "post-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	postRepo := &fakePostRepoForHandler{}
	postService := service.NewPostService(postRepo)
	postHandler := NewPostHandler(postService)

	router := gin.New()
	router.POST("/api/v1/posts", middleware.AuthJWT(jwtManager), postHandler.Create)
	router.GET("/api/v1/posts/:id", postHandler.GetByID)
	router.DELETE("/api/v1/posts/:id", middleware.AuthJWT(jwtManager), postHandler.Delete)

	token, err := jwtManager.GenerateToken(1001)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	createPayload := map[string]string{"content": "  你好 feed-flow  "}
	bodyBytes, err := json.Marshal(createPayload)
	if err != nil {
		t.Fatalf("marshal create payload failed: %v", err)
	}

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewBuffer(bodyBytes))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")

	createResp := httptest.NewRecorder()
	router.ServeHTTP(createResp, createReq)

	if createResp.Code != http.StatusOK {
		t.Fatalf("unexpected create status: got=%d want=%d", createResp.Code, http.StatusOK)
	}

	var createBody postAPIResponse
	if err := json.Unmarshal(createResp.Body.Bytes(), &createBody); err != nil {
		t.Fatalf("unmarshal create response failed: %v", err)
	}
	if createBody.Code != xerror.CodeOK {
		t.Fatalf("unexpected create code: got=%d want=%d", createBody.Code, xerror.CodeOK)
	}

	var created service.PostResult
	if err := json.Unmarshal(createBody.Data, &created); err != nil {
		t.Fatalf("unmarshal create data failed: %v", err)
	}
	if created.PostID <= 0 {
		t.Fatalf("invalid post id: %d", created.PostID)
	}
	if created.UserID != 1001 {
		t.Fatalf("unexpected created user id: got=%d want=%d", created.UserID, 1001)
	}
	if created.Content != "你好 feed-flow" {
		t.Fatalf("unexpected created content: got=%q want=%q", created.Content, "你好 feed-flow")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+strconv.FormatInt(created.PostID, 10), nil)
	getResp := httptest.NewRecorder()
	router.ServeHTTP(getResp, getReq)

	if getResp.Code != http.StatusOK {
		t.Fatalf("unexpected get status: got=%d want=%d", getResp.Code, http.StatusOK)
	}

	var getBody postAPIResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("unmarshal get response failed: %v", err)
	}
	if getBody.Code != xerror.CodeOK {
		t.Fatalf("unexpected get code: got=%d want=%d", getBody.Code, xerror.CodeOK)
	}

	var got service.PostResult
	if err := json.Unmarshal(getBody.Data, &got); err != nil {
		t.Fatalf("unmarshal get data failed: %v", err)
	}
	if got.PostID != created.PostID || got.UserID != created.UserID {
		t.Fatalf("unexpected get identity: got=%+v created=%+v", got, created)
	}
	if got.Content != created.Content {
		t.Fatalf("unexpected get content: got=%q want=%q", got.Content, created.Content)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/"+strconv.FormatInt(created.PostID, 10), nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteResp := httptest.NewRecorder()
	router.ServeHTTP(deleteResp, deleteReq)

	if deleteResp.Code != http.StatusOK {
		t.Fatalf("unexpected delete status: got=%d want=%d", deleteResp.Code, http.StatusOK)
	}

	var deleteBody postAPIResponse
	if err := json.Unmarshal(deleteResp.Body.Bytes(), &deleteBody); err != nil {
		t.Fatalf("unmarshal delete response failed: %v", err)
	}
	if deleteBody.Code != xerror.CodeOK {
		t.Fatalf("unexpected delete code: got=%d want=%d", deleteBody.Code, xerror.CodeOK)
	}

	getDeletedReq := httptest.NewRequest(http.MethodGet, "/api/v1/posts/"+strconv.FormatInt(created.PostID, 10), nil)
	getDeletedResp := httptest.NewRecorder()
	router.ServeHTTP(getDeletedResp, getDeletedReq)

	if getDeletedResp.Code != http.StatusNotFound {
		t.Fatalf("deleted post should not be readable: got=%d want=%d", getDeletedResp.Code, http.StatusNotFound)
	}
}

func TestPostCreateUnauthorized(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "post-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	postService := service.NewPostService(&fakePostRepoForHandler{})
	postHandler := NewPostHandler(postService)

	router := gin.New()
	router.POST("/api/v1/posts", middleware.AuthJWT(jwtManager), postHandler.Create)

	bodyBytes, err := json.Marshal(map[string]string{"content": "hello"})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/posts", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusUnauthorized)
	}

	var body postAPIResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeUnauthorized {
		t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeUnauthorized)
	}
}

func TestPostGetByIDNotFound(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	postService := service.NewPostService(&fakePostRepoForHandler{})
	postHandler := NewPostHandler(postService)

	router := gin.New()
	router.GET("/api/v1/posts/:id", postHandler.GetByID)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/posts/999", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: got=%d want=%d", resp.Code, http.StatusNotFound)
	}

	var body postAPIResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodePostNotFound {
		t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodePostNotFound)
	}
}

func TestPostDeleteForbiddenForNonAuthor(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      "post-integration-secret",
		ExpireHours: 1,
	})
	if err != nil {
		t.Fatalf("new jwt manager failed: %v", err)
	}

	postRepo := &fakePostRepoForHandler{
		posts: map[int64]*model.Post{
			1: {
				ID:        1,
				UserID:    1001,
				Content:   "author only",
				Status:    model.PostStatusPublished,
				CreatedAt: time.Date(2026, 4, 20, 13, 0, 0, 0, time.UTC),
			},
		},
	}
	postService := service.NewPostService(postRepo)
	postHandler := NewPostHandler(postService)

	router := gin.New()
	router.DELETE("/api/v1/posts/:id", middleware.AuthJWT(jwtManager), postHandler.Delete)

	token, err := jwtManager.GenerateToken(2002)
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/posts/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("unexpected delete status: got=%d want=%d", resp.Code, http.StatusForbidden)
	}

	var body postAPIResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if body.Code != xerror.CodeForbidden {
		t.Fatalf("unexpected error code: got=%d want=%d", body.Code, xerror.CodeForbidden)
	}
}
