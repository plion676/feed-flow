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

type PostHandler struct {
	postService *service.PostService
}

type CreatePostRequest struct {
	Content string `json:"content" binding:"required,min=1,max=500"`
}

func NewPostHandler(postService *service.PostService) *PostHandler {
	return &PostHandler{postService: postService}
}

func (h *PostHandler) Create(c *gin.Context) {
	userID, ok := middleware.CurrentUserID(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, xerror.ErrUnauthorized)
		return
	}

	var req CreatePostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, responseBindError())
		return
	}

	result, bizErr := h.postService.Create(c.Request.Context(), service.CreatePostRequest{
		UserID:  userID,
		Content: req.Content,
	})
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, result)
}

func (h *PostHandler) GetByID(c *gin.Context) {
	postID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || postID <= 0 {
		response.Fail(c, http.StatusBadRequest, xerror.ErrBadRequest)
		return
	}

	result, bizErr := h.postService.GetByID(c.Request.Context(), postID)
	if bizErr != nil {
		response.Fail(c, httpStatusFromError(bizErr), bizErr)
		return
	}

	response.Success(c, http.StatusOK, result)
}
