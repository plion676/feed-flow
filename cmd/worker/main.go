package main

import (
	"context"
	"errors"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plion676/feed-flow/internal/app"
	"github.com/plion676/feed-flow/internal/repository"
	"github.com/plion676/feed-flow/internal/service"
)

const (
	defaultConsumeRetryInitialBackoff = 1 * time.Second
	defaultConsumeRetryMaxBackoff     = 30 * time.Second
	defaultConsumeRetryJitterPercent  = 20
)

func main() {
	rand.Seed(time.Now().UnixNano())

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
	eventRepo = eventRepo.WithConsumerConfig(buildEventConsumerConfig(cfg))
	worker := service.NewFeedInvalidationWorker(followRepo, feedInvalidator).
		WithHybridPolicy(service.NewFeedHybridPolicy(cfg.Feed.Hybrid.PushFollowerThreshold))
	if cfg.Feed.Inbox.Enabled {
		inboxRepo := repository.NewFeedInboxRepository(redisClient)
		inboxCfg := buildInboxFanoutOptions(cfg)
		inboxFanout := service.NewFeedInboxFanout(inboxRepo, inboxCfg.MaxItems).
			WithBatchOptions(inboxCfg.BatchSize, inboxCfg.Workers)
		worker = worker.WithInboxFanout(inboxFanout)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Println("feed invalidation worker started")
	retryCfg := buildRetryConfig(cfg)
	retryCount := 0
	backoff := retryCfg.InitialBackoff
	for {
		err = eventRepo.ConsumePostCreatedEvents(ctx, func(ctx context.Context, event repository.FeedInvalidationEvent) error {
			return worker.HandlePostCreatedEvent(ctx, service.PostCreatedEvent{
				StreamID:     event.StreamID,
				AuthorUserID: event.AuthorID,
				PostID:       event.PostID,
				OccurredAt:   event.OccurredAt,
			})
		})
		if err == nil || errors.Is(err, context.Canceled) {
			break
		}

		retryCount++
		sleepBackoff := jitterBackoff(backoff, retryCfg.JitterPercent)
		log.Printf(
			"consume post created events failed retry=%d backoff=%s sleep=%s jitter_percent=%d err=%v",
			retryCount,
			backoff,
			sleepBackoff,
			retryCfg.JitterPercent,
			err,
		)

		timer := time.NewTimer(sleepBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			err = ctx.Err()
		case <-timer.C:
		}
		if errors.Is(err, context.Canceled) {
			break
		}
		backoff = nextRetryBackoff(backoff, retryCfg.InitialBackoff, retryCfg.MaxBackoff)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("consume post created events failed after retries=%d: %v", retryCount, err)
	}
	log.Println("feed invalidation worker stopped")
}

type workerRetryConfig struct {
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	JitterPercent  int
}

type inboxFanoutOptions struct {
	MaxItems  int64
	BatchSize int
	Workers   int
}

func buildRetryConfig(cfg *app.Config) workerRetryConfig {
	initial := defaultConsumeRetryInitialBackoff
	maxBackoff := defaultConsumeRetryMaxBackoff
	jitterPercent := defaultConsumeRetryJitterPercent

	if cfg != nil {
		if ms := cfg.Feed.Worker.RetryInitialBackoffMS; ms > 0 {
			initial = time.Duration(ms) * time.Millisecond
		}
		if ms := cfg.Feed.Worker.RetryMaxBackoffMS; ms > 0 {
			maxBackoff = time.Duration(ms) * time.Millisecond
		}
		if cfg.Feed.Worker.RetryJitterPercent >= 0 {
			jitterPercent = cfg.Feed.Worker.RetryJitterPercent
		}
	}

	if maxBackoff < initial {
		maxBackoff = initial
	}
	return workerRetryConfig{
		InitialBackoff: initial,
		MaxBackoff:     maxBackoff,
		JitterPercent:  clampPercent(jitterPercent),
	}
}

func buildInboxFanoutOptions(cfg *app.Config) inboxFanoutOptions {
	if cfg == nil {
		return inboxFanoutOptions{}
	}
	return inboxFanoutOptions{
		MaxItems:  cfg.Feed.Inbox.MaxItems,
		BatchSize: cfg.Feed.Inbox.BatchSize,
		Workers:   cfg.Feed.Inbox.Workers,
	}
}

func buildEventConsumerConfig(cfg *app.Config) repository.FeedInvalidationConsumerConfig {
	result := repository.FeedInvalidationConsumerConfig{}
	if cfg == nil {
		return result
	}
	if sec := cfg.Feed.Worker.ReclaimMinIdleSeconds; sec > 0 {
		result.ReclaimMinIdle = time.Duration(sec) * time.Second
	}
	if sec := cfg.Feed.Worker.IdleLogIntervalSeconds; sec > 0 {
		result.IdleLogInterval = time.Duration(sec) * time.Second
	}
	if batches := cfg.Feed.Worker.ReclaimBatchPerLoop; batches > 0 {
		result.ReclaimBatches = batches
	}
	if maxAttempts := cfg.Feed.Worker.RetryMaxAttempts; maxAttempts > 0 {
		result.RetryMax = maxAttempts
	}
	if ttlSeconds := cfg.Feed.Worker.RetryCounterTTLSeconds; ttlSeconds > 0 {
		result.RetryTTL = time.Duration(ttlSeconds) * time.Second
	}
	if cfg.Feed.Worker.DLQStreamKey != "" {
		result.DLQStreamKey = cfg.Feed.Worker.DLQStreamKey
	}
	return result
}

func nextRetryBackoff(current time.Duration, initial time.Duration, max time.Duration) time.Duration {
	if initial <= 0 {
		initial = defaultConsumeRetryInitialBackoff
	}
	if max <= 0 {
		return current
	}
	if current <= 0 {
		if initial > max {
			return max
		}
		return initial
	}
	next := current * 2
	if next > max {
		return max
	}
	return next
}

func jitterBackoff(base time.Duration, jitterPercent int) time.Duration {
	if base <= 0 {
		return 0
	}
	jitterPercent = clampPercent(jitterPercent)
	if jitterPercent == 0 {
		return base
	}

	deltaMax := int64(base) * int64(jitterPercent) / 100
	if deltaMax <= 0 {
		return base
	}

	offset := rand.Int63n(deltaMax*2+1) - deltaMax
	result := int64(base) + offset
	if result <= 0 {
		return time.Millisecond
	}
	return time.Duration(result)
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
