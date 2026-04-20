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

// FollowHandler exposes follow and unfollow endpoints.
type FollowHandler struct {
	followService *service.FollowService
}

func NewFollowHandler(followService *service.FollowService) *FollowHandler {
	return &FollowHandler{followService: followService}
}

func (h *FollowHandler) Follow(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}

	targetUserID, err := strconv.ParseInt(c.Param("target_user_id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
		return
	}

	if bizErr := h.followService.Follow(c.Request.Context(), userID, targetUserID); bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"followed": true})
}

func (h *FollowHandler) Unfollow(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}

	targetUserID, err := strconv.ParseInt(c.Param("target_user_id"), 10, 64)
	if err != nil || targetUserID <= 0 {
		response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
		return
	}

	if bizErr := h.followService.Unfollow(c.Request.Context(), userID, targetUserID); bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"followed": false})
}
