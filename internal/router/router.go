package router

import (
	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/handler"
)

// RegisterRoutes wires the shared HTTP routes.
func RegisterRoutes(engine *gin.Engine, authHandler *handler.AuthHandler) {
	healthHandler := handler.NewHealthHandler()

	engine.GET("/health", healthHandler.Ping)

	apiV1 := engine.Group("/api/v1")
	{
		apiV1.GET("/health", healthHandler.Ping)

		authGroup := apiV1.Group("/auth")
		{
			authGroup.POST("/register", authHandler.Register)
		}
	}
}
