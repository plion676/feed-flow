package handler

import (
	"net/http"

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
