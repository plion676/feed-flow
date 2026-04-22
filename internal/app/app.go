package app

import (
	"fmt"
	"log"

	"github.com/gin-gonic/gin"

	"github.com/plion676/feed-flow/internal/handler"
	"github.com/plion676/feed-flow/internal/middleware"
	jwtpkg "github.com/plion676/feed-flow/internal/pkg/jwt"
	"github.com/plion676/feed-flow/internal/repository"
	"github.com/plion676/feed-flow/internal/router"
	"github.com/plion676/feed-flow/internal/service"
)

// App is the composition root for the feed-flow HTTP service.
type App struct {
	Config *Config
	Engine *gin.Engine
}

// New builds the application with the shared middleware chain and routes.
func New(cfg *Config) *App {
	gin.SetMode(gin.DebugMode)
	if cfg.App.Env != "local" {
		gin.SetMode(gin.ReleaseMode)
	}

	engine := gin.New()
	engine.Use(
		middleware.RequestID(),
		middleware.Logger(),
		middleware.Recovery(),
	)
	db, err := NewMySQLDB(cfg)
	if err != nil {
		log.Fatalf("connect mysql failed: %v", err)
	}
	jwtManager, err := jwtpkg.NewManager(jwtpkg.Config{
		Secret:      cfg.JWT.Secret,
		ExpireHours: cfg.JWT.ExpireHours,
	})
	if err != nil {
		log.Fatalf("init jwt manager failed: %v", err)
	}
	userRepo := repository.NewUserRepository(db)
	userCountRepo := repository.NewUserCountRepository(db)
	postRepo := repository.NewPostRepository(db)
	followRepo := repository.NewFollowRepository(db)
	var feedCacheRepo *repository.FeedCacheRepository
	var feedCacheInvalidator *repository.FeedCacheInvalidatorRepository
	var feedInvalidationEventPub *repository.FeedInvalidationEventRepository
	redisClient, err := NewRedisClient(cfg)
	if err != nil {
		log.Printf("connect redis failed, fallback to db-only feed: %v", err)
	} else {
		feedCacheRepo = repository.NewFeedCacheRepository(redisClient)
		feedCacheInvalidator = repository.NewFeedCacheInvalidatorRepository(redisClient)
		feedInvalidationEventPub = repository.NewFeedInvalidationEventRepository(redisClient)
	}

	authService := service.NewAuthService(db, userRepo, userCountRepo, jwtManager)
	userService := service.NewUserService(userRepo)
	postService := service.NewPostService(postRepo)
	followService := service.NewFollowService(followRepo, userRepo)
	feedService := service.NewFeedService(followRepo, postRepo)
	if feedCacheRepo != nil {
		feedService = feedService.WithCache(feedCacheRepo)
	}
	if feedCacheInvalidator != nil {
		postService = postService.WithFeedCacheInvalidator(feedCacheInvalidator)
		followService = followService.WithFeedCacheInvalidator(feedCacheInvalidator)
	}
	if feedInvalidationEventPub != nil {
		postService = postService.WithFeedInvalidationEventPublisher(feedInvalidationEventPub)
	}

	authHandler := handler.NewAuthHandler(authService)
	userHandler := handler.NewUserHandler(userService)
	postHandler := handler.NewPostHandler(postService)
	followHandler := handler.NewFollowHandler(followService)
	feedHandler := handler.NewFeedHandler(feedService)
	authMiddleware := middleware.AuthJWT(jwtManager)

	router.RegisterRoutes(engine, authHandler, userHandler, postHandler, followHandler, feedHandler, authMiddleware)

	return &App{
		Config: cfg,
		Engine: engine,
	}
}

// Run starts the HTTP server.
func (a *App) Run() error {
	return a.Engine.Run(fmt.Sprintf(":%d", a.Config.App.Port))
}
