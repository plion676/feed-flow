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
	postInteractionRepo := repository.NewPostInteractionRepository(db)
	followRepo := repository.NewFollowRepository(db)
	feedEventOutboxRepo := repository.NewFeedEventOutboxRepository(db)
	feedDLQOperatorRepo := repository.NewFeedDLQOperatorRepository(db)
	var feedCacheRepo *repository.FeedCacheRepository
	var feedCacheInvalidator *repository.FeedCacheInvalidatorRepository
	var feedInboxRepo *repository.FeedInboxRepository
	var feedOutboxRepo *repository.FeedOutboxRepository
	var feedExposureRepo *repository.FeedExposureRepository
	redisClient, err := NewRedisClient(cfg)
	if err != nil {
		log.Printf("connect redis failed, fallback to db-only feed: %v", err)
	} else {
		feedCacheRepo = repository.NewFeedCacheRepository(redisClient)
		feedCacheInvalidator = repository.NewFeedCacheInvalidatorRepository(redisClient)
		feedInboxRepo = repository.NewFeedInboxRepository(redisClient)
		if cfg.Feed.Outbox.Enabled {
			feedOutboxRepo = repository.NewFeedOutboxRepository(redisClient)
		}
		if cfg.Feed.Exposure.Enabled {
			feedExposureRepo = repository.NewFeedExposureRepository(redisClient, repository.FeedExposureRepositoryOptions{
				KeyTTL: time.Duration(cfg.Feed.Exposure.KeyTTLHours) * time.Hour,
			})
		}
	}

	authService := service.NewAuthService(db, userRepo, userCountRepo, jwtManager)
	userService := service.NewUserService(userRepo).
		WithPostRepository(postRepo).
		WithFollowRepository(followRepo).
		WithUserCountRepository(userCountRepo)
	postService := service.NewPostServiceWithTransaction(service.NewGormTransactionRunner(db), postRepo, userCountRepo)
	postInteractionService := service.NewPostInteractionService(postRepo, postInteractionRepo)
	followService := service.NewFollowServiceWithTransaction(service.NewGormTransactionRunner(db), followRepo, userRepo, userCountRepo)
	feedService := service.NewFeedService(followRepo, postRepo)
	if feedCacheRepo != nil {
		feedService = feedService.WithCache(feedCacheRepo)
	}
	if feedInboxRepo != nil && cfg.Feed.Inbox.Enabled {
		feedService = feedService.WithInbox(feedInboxRepo)
	}
	if feedOutboxRepo != nil {
		feedService = feedService.WithOutbox(
			feedOutboxRepo,
			userCountRepo,
			service.NewFeedHybridPolicy(cfg.Feed.Hybrid.PushFollowerThreshold),
			service.FeedOutboxOptions{
				Enabled:           cfg.Feed.Outbox.Enabled,
				ReadChunkSize:     cfg.Feed.Outbox.ReadChunkSize,
				MaxAuthorsPerRead: cfg.Feed.Outbox.MaxAuthorsPerRead,
				DBFallbackEnabled: cfg.Feed.Outbox.DBFallbackEnabled,
			},
		)
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
	if feedOutboxRepo != nil {
		postService = postService.WithFeedOutbox(feedOutboxRepo, cfg.Feed.Outbox.MaxItems)
	}
	if feedInboxRepo != nil && cfg.Feed.Inbox.Enabled {
		followService = followService.WithInboxAuthorCleanup(
			service.NewFeedInboxAuthorCleanup(feedInboxRepo, postRepo),
		)
	}
	postService = postService.WithEventOutbox(feedEventOutboxRepo)

	authHandler := handler.NewAuthHandler(authService)
	userHandler := handler.NewUserHandler(userService)
	postHandler := handler.NewPostHandler(postService)
	postInteractionHandler := handler.NewPostInteractionHandler(postInteractionService)
	followHandler := handler.NewFollowHandler(followService)
	feedHandler := handler.NewFeedHandler(feedService)
	var feedDLQHandler *handler.FeedDLQHandler
	if redisClient != nil {
		feedInvalidationEventPub := repository.NewFeedInvalidationEventRepository(redisClient)
		feedDLQService := service.NewFeedDLQService(feedInvalidationEventPub)
		feedDLQAccessService := service.NewFeedDLQAccessService(feedDLQOperatorRepo)
		feedDLQHandler = handler.NewFeedDLQHandler(feedDLQService, feedDLQAccessService)
	}
	authMiddleware := middleware.AuthJWT(jwtManager)
	optionalAuthMiddleware := middleware.OptionalAuthJWT(jwtManager)

	router.RegisterRoutes(engine, authHandler, userHandler, postHandler, postInteractionHandler, followHandler, feedHandler, feedDLQHandler, authMiddleware, optionalAuthMiddleware)

	return &App{
		Config: cfg,
		Engine: engine,
	}
}

// Run starts the HTTP server.
func (a *App) Run() error {
	return a.Engine.Run(fmt.Sprintf(":%d", a.Config.App.Port))
}
