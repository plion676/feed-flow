package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/middleware"
	"github.com/plion676/feed-flow/internal/pkg/response"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/service"
)

// FeedHandler exposes the home feed endpoint.
type FeedHandler struct {
	feedService *service.FeedService
}

func NewFeedHandler(feedService *service.FeedService) *FeedHandler {
	return &FeedHandler{feedService: feedService}
}

func (h *FeedHandler) GetHomeFeed(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}

	cursor := c.Query("cursor")
	var lastPostID int64
	if raw := c.Query("last_post_id"); raw != "" {
		if cursor != "" {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		lastPostID = parsed
	}

	var limit int
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		limit = parsed
	}

	refresh := c.Query("refresh") == "1"

	result, bizErr := h.feedService.GetHomeFeed(c.Request.Context(), service.GetFeedRequest{
		UserID:     userID,
		LastPostID: lastPostID,
		Cursor:     cursor,
		Limit:      limit,
		Refresh:    refresh,
	})
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, result)
}

func (h *FeedHandler) GetDiscoverFeed(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}

	var lastPostID int64
	if raw := c.Query("last_post_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		lastPostID = parsed
	}

	var limit int
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		limit = parsed
	}

	result, bizErr := h.feedService.GetDiscoverFeed(c.Request.Context(), service.GetFeedRequest{
		UserID:     userID,
		LastPostID: lastPostID,
		Limit:      limit,
	})
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, result)
}
