package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
)

// FeedInvalidationEvent is the payload for async feed cache invalidation jobs.
type FeedInvalidationEvent struct {
	Type       string `json:"type"`
	AuthorID   int64  `json:"author_id"`
	OccurredAt int64  `json:"occurred_at"`
}

// FeedInvalidationEventRepository publishes invalidation events to Redis stream.
type FeedInvalidationEventRepository struct {
	client       *redis.Client
	groupName    string
	consumerName string
	count        int64
	block        time.Duration
}

func NewFeedInvalidationEventRepository(client *redis.Client) *FeedInvalidationEventRepository {
	return &FeedInvalidationEventRepository{
		client:       client,
		groupName:    defaultFeedInvalidationGroupName,
		consumerName: fmt.Sprintf("feed-worker-%d", os.Getpid()),
		count:        defaultFeedInvalidationCount,
		block:        defaultFeedInvalidationBlock,
	}
}

func (r *FeedInvalidationEventRepository) PublishPostCreated(ctx context.Context, authorUserID int64) error {
	if authorUserID <= 0 {
		return fmt.Errorf("author user id must be positive")
	}

	event := FeedInvalidationEvent{
		Type:       "post_created",
		AuthorID:   authorUserID,
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
	if err := r.ensureConsumerGroup(ctx); err != nil {
		return err
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		// 1) Retry this consumer's pending messages first.
		if err := r.consumeByStreamID(ctx, "0", 0, handler); err != nil {
			return err
		}
		// 2) Then block for new messages.
		if err := r.consumeByStreamID(ctx, ">", r.block, handler); err != nil {
			return err
		}
	}
}

func (r *FeedInvalidationEventRepository) consumeByStreamID(
	ctx context.Context,
	streamID string,
	block time.Duration,
	handler func(context.Context, int64) error,
) error {
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
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read group message stream_id=%s: %w", streamID, err)
	}

	for _, stream := range streams {
		for _, msg := range stream.Messages {
			shouldAck := false

			event, ok := decodeFeedInvalidationEvent(msg.Values["payload"])
			if !ok {
				// Drop malformed messages to prevent poison-message infinite retry.
				shouldAck = true
			} else if event.Type != "post_created" || event.AuthorID <= 0 {
				// Unknown type/invalid payload: ack and skip.
				shouldAck = true
			} else if err := handler(ctx, event.AuthorID); err == nil {
				shouldAck = true
			}

			if shouldAck {
				if err := r.client.XAck(ctx, feedInvalidationStreamKey, r.groupName, msg.ID).Err(); err != nil {
					return fmt.Errorf("ack message %s: %w", msg.ID, err)
				}
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
