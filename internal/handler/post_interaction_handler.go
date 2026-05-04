package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/middleware"
	"github.com/plion676/feed-flow/internal/pkg/response"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/service"
)

type PostInteractionHandler struct {
	service *service.PostInteractionService
}

type createCommentHTTPBody struct {
	Content string `json:"content" binding:"required,min=1,max=300"`
}

func NewPostInteractionHandler(service *service.PostInteractionService) *PostInteractionHandler {
	return &PostInteractionHandler{service: service}
}

func (h *PostInteractionHandler) Like(c *gin.Context) {
	h.togglePostInteraction(c, h.service.Like)
}

func (h *PostInteractionHandler) Unlike(c *gin.Context) {
	h.togglePostInteraction(c, h.service.Unlike)
}

func (h *PostInteractionHandler) Collect(c *gin.Context) {
	h.togglePostInteraction(c, h.service.Collect)
}

func (h *PostInteractionHandler) Uncollect(c *gin.Context) {
	h.togglePostInteraction(c, h.service.Uncollect)
}

func (h *PostInteractionHandler) CreateComment(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}
	postID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}

	var body createCommentHTTPBody
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Fail(c, http.StatusBadRequest, responseBindError())
		return
	}

	result, bizErr := h.service.CreateComment(c.Request.Context(), service.CreateCommentRequest{
		UserID:  userID,
		PostID:  postID,
		Content: body.Content,
	})
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}
	response.Success(c, http.StatusOK, result)
}

func (h *PostInteractionHandler) ListComments(c *gin.Context) {
	postID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}

	var lastCommentID int64
	if raw := c.Query("last_comment_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		lastCommentID = parsed
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

	result, bizErr := h.service.ListComments(c.Request.Context(), postID, lastCommentID, limit)
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}
	response.Success(c, http.StatusOK, result)
}

func (h *PostInteractionHandler) GetStatuses(c *gin.Context) {
	userID, _ := middleware.CurrentUserID(c)
	postIDs, ok := parsePostIDsQuery(c.Query("post_ids"))
	if !ok {
		response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
		return
	}

	result, bizErr := h.service.GetStatuses(c.Request.Context(), userID, postIDs)
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}
	response.Success(c, http.StatusOK, result)
}

func (h *PostInteractionHandler) togglePostInteraction(
	c *gin.Context,
	fn func(context.Context, int64, int64) (*service.PostInteractionResult, *xerror.Error),
) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}
	postID, ok := parsePositiveIDParam(c, "id")
	if !ok {
		return
	}

	result, bizErr := fn(c.Request.Context(), userID, postID)
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}
	response.Success(c, http.StatusOK, result)
}

func parsePositiveIDParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
		return 0, false
	}
	return id, true
}

func parsePostIDsQuery(raw string) ([]int64, bool) {
	if strings.TrimSpace(raw) == "" {
		return nil, false
	}
	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		parsed, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || parsed <= 0 {
			return nil, false
		}
		ids = append(ids, parsed)
	}
	return ids, true
}
