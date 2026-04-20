package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/middleware"
	"github.com/plion676/feed-flow/internal/model"
	jwtpkg "github.com/plion676/feed-flow/internal/pkg/jwt"
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
