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

// UserHandler provides user profile related endpoints.
type UserHandler struct {
	userService *service.UserService
}

func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{userService: userService}
}

func (h *UserHandler) Me(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}

	result, bizErr := h.userService.GetMe(c.Request.Context(), userID)
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, result)
}

func (h *UserHandler) GetUserPosts(c *gin.Context) {
	userID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || userID <= 0 {
		response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
		return
	}

	var lastPostID int64
	if raw := c.Query("last_post_id"); raw != "" {
		parsed, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || parsed < 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		lastPostID = parsed
	}

	var limit int
	if raw := c.Query("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 {
			response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
			return
		}
		limit = parsed
	}

	result, bizErr := h.userService.GetUserPosts(c.Request.Context(), service.GetUserPostsRequest{
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
