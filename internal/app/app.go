package app

import (
	"fmt"
	"log"
	"time"

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
	feedDLQOperatorRepo := repository.NewFeedDLQOperatorRepository(db)
	var feedCacheRepo *repository.FeedCacheRepository
	var feedCacheInvalidator *repository.FeedCacheInvalidatorRepository
	var feedInvalidationEventPub *repository.FeedInvalidationEventRepository
	var feedInboxRepo *repository.FeedInboxRepository
	var feedExposureRepo *repository.FeedExposureRepository
	redisClient, err := NewRedisClient(cfg)
	if err != nil {
		log.Printf("connect redis failed, fallback to db-only feed: %v", err)
	} else {
		feedCacheRepo = repository.NewFeedCacheRepository(redisClient)
		feedCacheInvalidator = repository.NewFeedCacheInvalidatorRepository(redisClient)
		feedInvalidationEventPub = repository.NewFeedInvalidationEventRepository(redisClient)
		feedInboxRepo = repository.NewFeedInboxRepository(redisClient)
		if cfg.Feed.Exposure.Enabled {
			feedExposureRepo = repository.NewFeedExposureRepository(redisClient, repository.FeedExposureRepositoryOptions{
				KeyTTL: time.Duration(cfg.Feed.Exposure.KeyTTLHours) * time.Hour,
			})
		}
	}

	authService := service.NewAuthService(db, userRepo, userCountRepo, jwtManager)
	userService := service.NewUserService(userRepo).WithPostRepository(postRepo)
	postService := service.NewPostService(postRepo)
	followService := service.NewFollowService(followRepo, userRepo)
	feedService := service.NewFeedService(followRepo, postRepo)
	if feedCacheRepo != nil {
		feedService = feedService.WithCache(feedCacheRepo)
	}
	if feedInboxRepo != nil && cfg.Feed.Inbox.Enabled {
		feedService = feedService.WithInbox(feedInboxRepo)
	}
	if feedExposureRepo != nil {
		feedService = feedService.WithExposure(feedExposureRepo, service.FeedExposureOptions{
			WindowTTL:       time.Duration(cfg.Feed.Exposure.WindowHours) * time.Hour,
			BatchMultiplier: cfg.Feed.Exposure.BatchMultiplier,
		})
	}
	feedService = feedService.WithMixPolicy(service.FeedMixOptions{
		PushRatioNumerator:   cfg.Feed.Hybrid.Mix.PushRatioNumerator,
		PushRatioDenominator: cfg.Feed.Hybrid.Mix.PushRatioDenominator,
		MinPullItems:         cfg.Feed.Hybrid.Mix.MinPullItems,
		MaxConsecutiveAuthor: cfg.Feed.Hybrid.Mix.MaxConsecutiveAuthor,
		AuthorCooldownWindow: cfg.Feed.Hybrid.Mix.AuthorCooldownWindow,
		MaxConsecutiveSource: cfg.Feed.Hybrid.Mix.MaxConsecutiveSource,
	})
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
	var feedDLQHandler *handler.FeedDLQHandler
	if feedInvalidationEventPub != nil {
		feedDLQService := service.NewFeedDLQService(feedInvalidationEventPub)
		feedDLQAccessService := service.NewFeedDLQAccessService(feedDLQOperatorRepo)
		feedDLQHandler = handler.NewFeedDLQHandler(feedDLQService, feedDLQAccessService)
	}
	authMiddleware := middleware.AuthJWT(jwtManager)

	router.RegisterRoutes(engine, authHandler, userHandler, postHandler, followHandler, feedHandler, feedDLQHandler, authMiddleware)

	return &App{
		Config: cfg,
		Engine: engine,
	}
}

// Run starts the HTTP server.
func (a *App) Run() error {
	return a.Engine.Run(fmt.Sprintf(":%d", a.Config.App.Port))
}
