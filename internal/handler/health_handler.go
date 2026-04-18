package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/pkg/response"
)

// HealthHandler exposes the health-check endpoint.
type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) Ping(c *gin.Context) {
	response.Success(c, http.StatusOK, gin.H{
		"status": "ok",
		"service": "feed-flow",
	})
}
