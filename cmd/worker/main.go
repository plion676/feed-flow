package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/plion676/feed-flow/internal/app"
	"github.com/plion676/feed-flow/internal/repository"
	"github.com/plion676/feed-flow/internal/service"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	db, err := app.NewMySQLDB(cfg)
	if err != nil {
		log.Fatalf("connect mysql failed: %v", err)
	}

	redisClient, err := app.NewRedisClient(cfg)
	if err != nil {
		log.Fatalf("connect redis failed: %v", err)
	}

	followRepo := repository.NewFollowRepository(db)
	feedInvalidator := repository.NewFeedCacheInvalidatorRepository(redisClient)
	eventRepo := repository.NewFeedInvalidationEventRepository(redisClient)
	worker := service.NewFeedInvalidationWorker(followRepo, feedInvalidator).
		WithHybridPolicy(service.NewFeedHybridPolicy(cfg.Feed.Hybrid.PushFollowerThreshold))
	if cfg.Feed.Inbox.Enabled {
		inboxRepo := repository.NewFeedInboxRepository(redisClient)
		worker = worker.WithInboxFanout(service.NewFeedInboxFanout(inboxRepo, cfg.Feed.Inbox.MaxItems))
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Println("feed invalidation worker started")
	err = eventRepo.ConsumePostCreatedEvents(ctx, func(ctx context.Context, event repository.FeedInvalidationEvent) error {
		return worker.HandlePostCreatedEvent(ctx, service.PostCreatedEvent{
			AuthorUserID: event.AuthorID,
			PostID:       event.PostID,
			OccurredAt:   event.OccurredAt,
		})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("consume post created events failed: %v", err)
	}
	log.Println("feed invalidation worker stopped")
}
