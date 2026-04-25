package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const feedInvalidationStreamKey = "feed:invalidation:events"

const (
	defaultFeedInvalidationGroupName = "feed-invalidation-group"
	defaultFeedInvalidationCount     = 20
	defaultFeedInvalidationBlock     = 2 * time.Second
	defaultReclaimMinIdle            = 30 * time.Second
	defaultIdleLogInterval           = 30 * time.Second
	defaultReclaimBatchPerLoop       = 5
)

// FeedInvalidationEvent is the payload for async feed cache invalidation jobs.
type FeedInvalidationEvent struct {
	Type       string `json:"type"`
	AuthorID   int64  `json:"author_id"`
	PostID     int64  `json:"post_id"`
	OccurredAt int64  `json:"occurred_at"`
}

// FeedInvalidationEventRepository publishes invalidation events to Redis stream.
type FeedInvalidationEventRepository struct {
	client       *redis.Client
	groupName    string
	consumerName string
	count        int64
	block        time.Duration
	reclaimIdle  time.Duration
	idleLogAfter time.Duration
}

func NewFeedInvalidationEventRepository(client *redis.Client) *FeedInvalidationEventRepository {
	return &FeedInvalidationEventRepository{
		client:       client,
		groupName:    defaultFeedInvalidationGroupName,
		consumerName: fmt.Sprintf("feed-worker-%d", os.Getpid()),
		count:        defaultFeedInvalidationCount,
		block:        defaultFeedInvalidationBlock,
		reclaimIdle:  defaultReclaimMinIdle,
		idleLogAfter: defaultIdleLogInterval,
	}
}

func (r *FeedInvalidationEventRepository) PublishPostCreated(ctx context.Context, authorUserID int64) error {
	return r.PublishPostCreatedEvent(ctx, authorUserID, 0)
}

func (r *FeedInvalidationEventRepository) PublishPostCreatedEvent(ctx context.Context, authorUserID int64, postID int64) error {
	if authorUserID <= 0 {
		return fmt.Errorf("author user id must be positive")
	}

	event := FeedInvalidationEvent{
		Type:       "post_created",
		AuthorID:   authorUserID,
		PostID:     postID,
		OccurredAt: time.Now().Unix(),
	}
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal feed invalidation event: %w", err)
	}

	return r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: feedInvalidationStreamKey,
		Values: map[string]any{
			"payload": string(payloadBytes),
		},
	}).Err()
}

func (r *FeedInvalidationEventRepository) ConsumePostCreated(ctx context.Context, handler func(context.Context, int64) error) error {
	if handler == nil {
		return fmt.Errorf("post created handler cannot be nil")
	}
	return r.ConsumePostCreatedEvents(ctx, func(ctx context.Context, event FeedInvalidationEvent) error {
		return handler(ctx, event.AuthorID)
	})
}

func (r *FeedInvalidationEventRepository) ConsumePostCreatedEvents(
	ctx context.Context,
	handler func(context.Context, FeedInvalidationEvent) error,
) error {
	if handler == nil {
		return fmt.Errorf("post created handler cannot be nil")
	}
	if err := r.ensureConsumerGroup(ctx); err != nil {
		return err
	}

	lastProgress := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		processed := 0

		// 1) Reclaim orphan pending from crashed consumers.
		n, err := r.reclaimPending(ctx, handler)
		if err != nil {
			return err
		}
		processed += n

		// 2) Retry this consumer's pending messages first.
		n, err = r.consumeByStreamID(ctx, "0", 0, handler)
		if err != nil {
			return err
		}
		processed += n

		// 3) Then block for new messages.
		n, err = r.consumeByStreamID(ctx, ">", r.block, handler)
		if err != nil {
			return err
		}
		processed += n

		if processed > 0 {
			lastProgress = time.Now()
			continue
		}
		if time.Since(lastProgress) >= r.idleLogAfter {
			log.Printf(
				"feed invalidation stream waiting group=%s consumer=%s block=%s",
				r.groupName,
				r.consumerName,
				r.block,
			)
			lastProgress = time.Now()
		}
	}
}

func (r *FeedInvalidationEventRepository) consumeByStreamID(
	ctx context.Context,
	streamID string,
	block time.Duration,
	handler func(context.Context, FeedInvalidationEvent) error,
) (int, error) {
	streams, err := r.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    r.groupName,
		Consumer: r.consumerName,
		Streams:  []string{feedInvalidationStreamKey, streamID},
		Count:    r.count,
		Block:    block,
		NoAck:    false,
	}).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 0, fmt.Errorf(
			"read group message stream_id=%s group=%s consumer=%s: %w",
			streamID,
			r.groupName,
			r.consumerName,
			err,
		)
	}

	processed := 0
	for _, stream := range streams {
		if err := r.handleMessages(ctx, stream.Messages, "xreadgroup", handler); err != nil {
			return processed, err
		}
		processed += len(stream.Messages)
	}

	return processed, nil
}

func (r *FeedInvalidationEventRepository) reclaimPending(
	ctx context.Context,
	handler func(context.Context, FeedInvalidationEvent) error,
) (int, error) {
	if r.reclaimIdle <= 0 {
		return 0, nil
	}

	processed := 0
	start := "0-0"
	for i := 0; i < defaultReclaimBatchPerLoop; i++ {
		messages, nextStart, err := r.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   feedInvalidationStreamKey,
			Group:    r.groupName,
			Consumer: r.consumerName,
			MinIdle:  r.reclaimIdle,
			Start:    start,
			Count:    r.count,
		}).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return processed, nil
			}
			if ctx.Err() != nil {
				return processed, ctx.Err()
			}
			return processed, fmt.Errorf(
				"xautoclaim pending group=%s consumer=%s start=%s: %w",
				r.groupName,
				r.consumerName,
				start,
				err,
			)
		}
		if len(messages) == 0 {
			return processed, nil
		}

		if err := r.handleMessages(ctx, messages, "xautoclaim", handler); err != nil {
			return processed, err
		}
		processed += len(messages)

		if nextStart == "" || nextStart == "0-0" || nextStart == start {
			return processed, nil
		}
		start = nextStart
	}

	return processed, nil
}

func (r *FeedInvalidationEventRepository) handleMessages(
	ctx context.Context,
	messages []redis.XMessage,
	readSource string,
	handler func(context.Context, FeedInvalidationEvent) error,
) error {
	for _, msg := range messages {
		shouldAck := false
		event := FeedInvalidationEvent{}

		decodedEvent, ok := decodeFeedInvalidationEvent(msg.Values["payload"])
		if !ok {
			// Drop malformed messages to prevent poison-message infinite retry.
			shouldAck = true
			log.Printf(
				"drop malformed feed event source=%s stream_id=%s group=%s consumer=%s",
				readSource,
				msg.ID,
				r.groupName,
				r.consumerName,
			)
		} else {
			event = decodedEvent
			if event.Type != "post_created" || event.AuthorID <= 0 {
				// Unknown type/invalid payload: ack and skip.
				shouldAck = true
				log.Printf(
					"skip invalid feed event source=%s stream_id=%s type=%s author_id=%d post_id=%d",
					readSource,
					msg.ID,
					event.Type,
					event.AuthorID,
					event.PostID,
				)
			} else if err := handler(ctx, event); err == nil {
				shouldAck = true
			} else {
				log.Printf(
					"handle feed event failed source=%s stream_id=%s author_id=%d post_id=%d err=%v",
					readSource,
					msg.ID,
					event.AuthorID,
					event.PostID,
					err,
				)
			}
		}

		if shouldAck {
			if err := r.client.XAck(ctx, feedInvalidationStreamKey, r.groupName, msg.ID).Err(); err != nil {
				log.Printf(
					"ack feed event failed source=%s stream_id=%s author_id=%d post_id=%d err=%v",
					readSource,
					msg.ID,
					event.AuthorID,
					event.PostID,
					err,
				)
				return fmt.Errorf(
					"ack feed event source=%s stream_id=%s author_id=%d post_id=%d: %w",
					readSource,
					msg.ID,
					event.AuthorID,
					event.PostID,
					err,
				)
			}
		}
	}
	return nil
}

func (r *FeedInvalidationEventRepository) ensureConsumerGroup(ctx context.Context) error {
	err := r.client.XGroupCreateMkStream(ctx, feedInvalidationStreamKey, r.groupName, "0").Err()
	if err == nil {
		return nil
	}
	if isBusyGroupError(err) {
		return nil
	}
	return fmt.Errorf("create consumer group: %w", err)
}

func isBusyGroupError(err error) bool {
	if err == nil {
		return false
	}

	var redisErr redis.Error
	if !errors.As(err, &redisErr) {
		return false
	}
	return strings.HasPrefix(redisErr.Error(), "BUSYGROUP ")
}

func decodeFeedInvalidationEvent(payloadValue any) (FeedInvalidationEvent, bool) {
	var payloadBytes []byte
	switch value := payloadValue.(type) {
	case string:
		payloadBytes = []byte(value)
	case []byte:
		payloadBytes = value
	default:
		return FeedInvalidationEvent{}, false
	}

	var event FeedInvalidationEvent
	if err := json.Unmarshal(payloadBytes, &event); err != nil {
		return FeedInvalidationEvent{}, false
	}
	return event, true
}
