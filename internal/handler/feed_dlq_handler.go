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

type FeedDLQHandler struct {
	dlqService    *service.FeedDLQService
	accessService *service.FeedDLQAccessService
}

func NewFeedDLQHandler(
	dlqService *service.FeedDLQService,
	accessService *service.FeedDLQAccessService,
) *FeedDLQHandler {
	return &FeedDLQHandler{
		dlqService:    dlqService,
		accessService: accessService,
	}
}

func (h *FeedDLQHandler) List(c *gin.Context) {
	if h == nil || h.dlqService == nil || h.accessService == nil {
		response.Fail(c, http.StatusInternalServerError, xerror.ErrInternal)
		return
	}

	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}
	if bizErr := h.accessService.AuthorizeList(c.Request.Context(), userID); bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	limit := 0
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		limit = parsed
	}

	result, bizErr := h.dlqService.ListEvents(c.Request.Context(), service.ListDLQEventsRequest{
		Limit: limit,
	})
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, result)
}

func (h *FeedDLQHandler) Replay(c *gin.Context) {
	if h == nil || h.dlqService == nil || h.accessService == nil {
		response.Fail(c, http.StatusInternalServerError, xerror.ErrInternal)
		return
	}

	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}
	if bizErr := h.accessService.AuthorizeReplay(c.Request.Context(), userID); bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	var req service.ReplayDLQEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, responseBindError())
		return
	}

	result, bizErr := h.dlqService.ReplayEvent(c.Request.Context(), req)
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, result)
}
