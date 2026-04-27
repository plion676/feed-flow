package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/repository"
	"github.com/plion676/feed-flow/internal/service"
)

type fakeDLQHandlerRepo struct {
	listItems []repository.FeedInvalidationDLQRecord
	listErr   error
	replayRes *repository.ReplayDLQResult
	replayErr error
}

func (f *fakeDLQHandlerRepo) ListDLQEvents(_ context.Context, _ int64) ([]repository.FeedInvalidationDLQRecord, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]repository.FeedInvalidationDLQRecord, len(f.listItems))
	copy(out, f.listItems)
	return out, nil
}

func (f *fakeDLQHandlerRepo) ReplayDLQEvent(_ context.Context, _ string, _ bool) (*repository.ReplayDLQResult, error) {
	if f.replayErr != nil {
		return nil, f.replayErr
	}
	return f.replayRes, nil
}

type fakeDLQOperatorRepo struct {
	operatorByUserID map[int64]*model.FeedDLQOperator
	err              error
}

func (f *fakeDLQOperatorRepo) GetByUserID(_ context.Context, userID int64) (*model.FeedDLQOperator, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.operatorByUserID[userID], nil
}

func buildDLQHandler(
	dlqRepo *fakeDLQHandlerRepo,
	operatorByUserID map[int64]*model.FeedDLQOperator,
) *FeedDLQHandler {
	dlqService := service.NewFeedDLQService(dlqRepo)
	accessService := service.NewFeedDLQAccessService(&fakeDLQOperatorRepo{
		operatorByUserID: operatorByUserID,
	})
	return NewFeedDLQHandler(dlqService, accessService)
}

func TestFeedDLQHandlerList(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	repo := &fakeDLQHandlerRepo{
		listItems: []repository.FeedInvalidationDLQRecord{
			{MessageID: "1740000000000-0", RetryCount: 5},
		},
	}
	h := buildDLQHandler(repo, map[int64]*model.FeedDLQOperator{
		1: {
			UserID: 1,
			Role:   model.FeedDLQOperatorRoleOperator,
			Status: model.FeedDLQOperatorStatusActive,
		},
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("current_user_id", int64(1))
		c.Next()
	})
	router.GET("/api/v1/feed/dlq", h.List)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed/dlq?limit=1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestFeedDLQHandlerReplayBadRequest(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	repo := &fakeDLQHandlerRepo{}
	h := buildDLQHandler(repo, map[int64]*model.FeedDLQOperator{
		1: {
			UserID: 1,
			Role:   model.FeedDLQOperatorRoleAdmin,
			Status: model.FeedDLQOperatorStatusActive,
		},
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("current_user_id", int64(1))
		c.Next()
	})
	router.POST("/api/v1/feed/dlq/replay", h.Replay)

	body := []byte(`{"dlq_message_id":""}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feed/dlq/replay", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestFeedDLQHandlerReplaySuccess(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	repo := &fakeDLQHandlerRepo{
		replayRes: &repository.ReplayDLQResult{
			ReplayedStreamID: "1740000001111-0",
		},
	}
	h := buildDLQHandler(repo, map[int64]*model.FeedDLQOperator{
		1: {
			UserID: 1,
			Role:   model.FeedDLQOperatorRoleAdmin,
			Status: model.FeedDLQOperatorStatusActive,
		},
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("current_user_id", int64(1))
		c.Next()
	})
	router.POST("/api/v1/feed/dlq/replay", h.Replay)

	reqBody, _ := json.Marshal(service.ReplayDLQEventRequest{
		DLQMessageID:      "1740000000000-0",
		DeleteAfterReplay: true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feed/dlq/replay", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestFeedDLQHandlerReplayForbiddenForOperator(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	repo := &fakeDLQHandlerRepo{}
	h := buildDLQHandler(repo, map[int64]*model.FeedDLQOperator{
		1: {
			UserID: 1,
			Role:   model.FeedDLQOperatorRoleOperator,
			Status: model.FeedDLQOperatorStatusActive,
		},
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("current_user_id", int64(1))
		c.Next()
	})
	router.POST("/api/v1/feed/dlq/replay", h.Replay)

	reqBody, _ := json.Marshal(service.ReplayDLQEventRequest{
		DLQMessageID: "1740000000000-0",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/feed/dlq/replay", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestFeedDLQHandlerListForbiddenWhenNotOperator(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	repo := &fakeDLQHandlerRepo{}
	h := buildDLQHandler(repo, map[int64]*model.FeedDLQOperator{})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("current_user_id", int64(2))
		c.Next()
	})
	router.GET("/api/v1/feed/dlq", h.List)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/feed/dlq?limit=1", nil)
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d body=%s", resp.Code, resp.Body.String())
	}
}
