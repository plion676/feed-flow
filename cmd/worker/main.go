package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plion676/feed-flow/internal/app"
	"github.com/plion676/feed-flow/internal/repository"
	"github.com/plion676/feed-flow/internal/service"
)

const (
	consumeRetryInitialBackoff = 1 * time.Second
	consumeRetryMaxBackoff     = 30 * time.Second
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
	retryCount := 0
	backoff := consumeRetryInitialBackoff
	for {
		err = eventRepo.ConsumePostCreatedEvents(ctx, func(ctx context.Context, event repository.FeedInvalidationEvent) error {
			return worker.HandlePostCreatedEvent(ctx, service.PostCreatedEvent{
				AuthorUserID: event.AuthorID,
				PostID:       event.PostID,
				OccurredAt:   event.OccurredAt,
			})
		})
		if err == nil || errors.Is(err, context.Canceled) {
			break
		}

		retryCount++
		log.Printf("consume post created events failed retry=%d backoff=%s err=%v", retryCount, backoff, err)

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			err = ctx.Err()
		case <-timer.C:
		}
		if errors.Is(err, context.Canceled) {
			break
		}
		backoff = nextRetryBackoff(backoff, consumeRetryMaxBackoff)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("consume post created events failed after retries=%d: %v", retryCount, err)
	}
	log.Println("feed invalidation worker stopped")
}

func nextRetryBackoff(current time.Duration, max time.Duration) time.Duration {
	if max <= 0 {
		return current
	}
	if current <= 0 {
		if consumeRetryInitialBackoff > max {
			return max
		}
		return consumeRetryInitialBackoff
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}
