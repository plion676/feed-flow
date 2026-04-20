package router

import (
	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/handler"
)

// RegisterRoutes wires the shared HTTP routes.
func RegisterRoutes(
	engine *gin.Engine,
	authHandler *handler.AuthHandler,
	userHandler *handler.UserHandler,
	postHandler *handler.PostHandler,
	followHandler *handler.FollowHandler,
	feedHandler *handler.FeedHandler,
	authMiddleware gin.HandlerFunc,
) {
	healthHandler := handler.NewHealthHandler()

	engine.GET("/health", healthHandler.Ping)

	apiV1 := engine.Group("/api/v1")
	{
		apiV1.GET("/health", healthHandler.Ping)

		authGroup := apiV1.Group("/auth")
		{
			authGroup.POST("/register", authHandler.Register)
			authGroup.POST("/login", authHandler.Login)
		}

		userGroup := apiV1.Group("/users")
		userGroup.Use(authMiddleware)
		{
			userGroup.GET("/me", userHandler.Me)
		}

		postGroup := apiV1.Group("/posts")
		{
			postGroup.GET("/:id", postHandler.GetByID)
			postGroup.POST("", authMiddleware, postHandler.Create)
		}

		followGroup := apiV1.Group("/follows")
		followGroup.Use(authMiddleware)
		{
			followGroup.POST("/:target_user_id", followHandler.Follow)
			followGroup.DELETE("/:target_user_id", followHandler.Unfollow)
		}

		feedGroup := apiV1.Group("/feed")
		feedGroup.Use(authMiddleware)
		{
			feedGroup.GET("", feedHandler.GetHomeFeed)
		}
	}
}
