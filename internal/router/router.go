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
	feedDLQHandler *handler.FeedDLQHandler,
	authMiddleware gin.HandlerFunc,
	optionalAuthMiddleware gin.HandlerFunc,
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
		userGroup.Use(optionalAuthMiddleware)
		{
			userGroup.GET("/:id", userHandler.GetByID)
			userGroup.GET("/:id/posts", userHandler.GetUserPosts)
			userGroup.GET("/:id/followers", userHandler.GetFollowers)
			userGroup.GET("/:id/following", userHandler.GetFollowing)
		}

		authUserGroup := apiV1.Group("/users")
		authUserGroup.Use(authMiddleware)
		{
			authUserGroup.GET("/me", userHandler.Me)
		}

		postGroup := apiV1.Group("/posts")
		{
			postGroup.GET("/:id", postHandler.GetByID)
			postGroup.POST("", authMiddleware, postHandler.Create)
			postGroup.DELETE("/:id", authMiddleware, postHandler.Delete)
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
			if feedDLQHandler != nil {
				feedGroup.GET("/dlq", feedDLQHandler.List)
				feedGroup.POST("/dlq/replay", feedDLQHandler.Replay)
			}
		}
	}
}
