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
const defaultFeedInvalidationDLQStreamKey = "feed:invalidation:dlq"
const defaultFeedInvalidationOutboxPublishLockTTL = 7 * 24 * time.Hour

const (
	FeedInvalidationEventTypePostCreated = "post_created"
	FeedInvalidationEventTypePostDeleted = "post_deleted"
)

const (
	defaultFeedInvalidationGroupName = "feed-invalidation-group"
	defaultFeedInvalidationCount     = 20
	defaultFeedInvalidationBlock     = 2 * time.Second
	defaultReclaimMinIdle            = 30 * time.Second
	defaultIdleLogInterval           = 30 * time.Second
	defaultReclaimBatchPerLoop       = 5
	defaultRetryMaxAttempts          = 5
	defaultRetryCounterTTL           = 24 * time.Hour
	defaultDLQReplayLockTTL          = 7 * 24 * time.Hour
)

var ErrDLQEventNotFound = errors.New("dlq event not found")

// FeedInvalidationEvent is the payload for async feed cache invalidation jobs.
type FeedInvalidationEvent struct {
	StreamID   string `json:"-"`
	Type       string `json:"type"`
	AuthorID   int64  `json:"author_id"`
	PostID     int64  `json:"post_id"`
	OccurredAt int64  `json:"occurred_at"`
}

type FeedInvalidationDLQRecord struct {
	MessageID  string                `json:"message_id"`
	StreamID   string                `json:"stream_id"`
	Source     string                `json:"source"`
	RetryCount int64                 `json:"retry_count"`
	FailedAt   int64                 `json:"failed_at"`
	LastError  string                `json:"last_error"`
	Event      FeedInvalidationEvent `json:"event"`
	Payload    string                `json:"payload"`
}

type ReplayDLQResult struct {
	DLQRecord        FeedInvalidationDLQRecord `json:"dlq_record"`
	ReplayedStreamID string                    `json:"replayed_stream_id"`
	AlreadyReplayed  bool                      `json:"already_replayed"`
}

type feedInvalidationDLQEvent struct {
	StreamID   string                `json:"stream_id"`
	Source     string                `json:"source"`
	RetryCount int64                 `json:"retry_count"`
	FailedAt   int64                 `json:"failed_at"`
	LastError  string                `json:"last_error"`
	Event      FeedInvalidationEvent `json:"event"`
	Payload    string                `json:"payload"`
}

// FeedInvalidationEventRepository publishes invalidation events to Redis stream.
type FeedInvalidationEventRepository struct {
	client         *redis.Client
	groupName      string
	consumerName   string
	count          int64
	block          time.Duration
	reclaimIdle    time.Duration
	idleLogAfter   time.Duration
	reclaimBatches int
	retryMax       int
	retryTTL       time.Duration
	dlqStreamKey   string
}

type FeedInvalidationConsumerConfig struct {
	ReclaimMinIdle  time.Duration
	IdleLogInterval time.Duration
	ReclaimBatches  int
	RetryMax        int
	RetryTTL        time.Duration
	DLQStreamKey    string
}

func NewFeedInvalidationEventRepository(client *redis.Client) *FeedInvalidationEventRepository {
	return &FeedInvalidationEventRepository{
		client:         client,
		groupName:      defaultFeedInvalidationGroupName,
		consumerName:   fmt.Sprintf("feed-worker-%d", os.Getpid()),
		count:          defaultFeedInvalidationCount,
		block:          defaultFeedInvalidationBlock,
		reclaimIdle:    defaultReclaimMinIdle,
		idleLogAfter:   defaultIdleLogInterval,
		reclaimBatches: defaultReclaimBatchPerLoop,
		retryMax:       defaultRetryMaxAttempts,
		retryTTL:       defaultRetryCounterTTL,
		dlqStreamKey:   defaultFeedInvalidationDLQStreamKey,
	}
}

func (r *FeedInvalidationEventRepository) WithConsumerConfig(cfg FeedInvalidationConsumerConfig) *FeedInvalidationEventRepository {
	if r == nil {
		return r
	}
	if cfg.ReclaimMinIdle > 0 {
		r.reclaimIdle = cfg.ReclaimMinIdle
	}
	if cfg.IdleLogInterval > 0 {
		r.idleLogAfter = cfg.IdleLogInterval
	}
	if cfg.ReclaimBatches > 0 {
		r.reclaimBatches = cfg.ReclaimBatches
	}
	if cfg.RetryMax > 0 {
		r.retryMax = cfg.RetryMax
	}
	if cfg.RetryTTL > 0 {
		r.retryTTL = cfg.RetryTTL
	}
	if strings.TrimSpace(cfg.DLQStreamKey) != "" {
		r.dlqStreamKey = strings.TrimSpace(cfg.DLQStreamKey)
	}
	return r
}

func (r *FeedInvalidationEventRepository) logFeedEventf(
	action string,
	readSource string,
	streamID string,
	event FeedInvalidationEvent,
	extraFormat string,
	extraArgs ...any,
) {
	format := "feed_event action=%s source=%s stream_id=%s group=%s consumer=%s author_id=%d post_id=%d"
	args := []any{
		action,
		readSource,
		streamID,
		r.groupName,
		r.consumerName,
		event.AuthorID,
		event.PostID,
	}
	if strings.TrimSpace(extraFormat) != "" {
		format += " " + extraFormat
		args = append(args, extraArgs...)
	}
	log.Printf(format, args...)
}

func (r *FeedInvalidationEventRepository) PublishPostCreated(ctx context.Context, authorUserID int64) error {
	return r.PublishPostCreatedEvent(ctx, authorUserID, 0)
}

func (r *FeedInvalidationEventRepository) PublishPostCreatedEvent(ctx context.Context, authorUserID int64, postID int64) error {
	return r.publishPostEvent(ctx, FeedInvalidationEventTypePostCreated, authorUserID, postID)
}

func (r *FeedInvalidationEventRepository) PublishPostDeletedEvent(ctx context.Context, authorUserID int64, postID int64) error {
	return r.publishPostEvent(ctx, FeedInvalidationEventTypePostDeleted, authorUserID, postID)
}

func (r *FeedInvalidationEventRepository) PublishEventPayload(ctx context.Context, outboxEventID int64, payload string) error {
	if outboxEventID <= 0 {
		return fmt.Errorf("outbox event id must be positive")
	}
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return fmt.Errorf("payload cannot be empty")
	}

	lockKey := buildOutboxPublishLockKey(outboxEventID)
	locked, err := r.client.SetNX(ctx, lockKey, "1", defaultFeedInvalidationOutboxPublishLockTTL).Result()
	if err != nil {
		return fmt.Errorf("setnx outbox publish lock key=%s outbox_event_id=%d: %w", lockKey, outboxEventID, err)
	}
	if !locked {
		return nil
	}

	if err := r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: feedInvalidationStreamKey,
		Values: map[string]any{
			"payload": payload,
		},
	}).Err(); err != nil {
		_ = r.client.Del(ctx, lockKey).Err()
		return fmt.Errorf("xadd feed invalidation stream outbox_event_id=%d: %w", outboxEventID, err)
	}
	return nil
}

func (r *FeedInvalidationEventRepository) publishPostEvent(ctx context.Context, eventType string, authorUserID int64, postID int64) error {
	if authorUserID <= 0 {
		return fmt.Errorf("author user id must be positive")
	}
	if strings.TrimSpace(eventType) == "" {
		return fmt.Errorf("feed invalidation event type cannot be empty")
	}

	event := FeedInvalidationEvent{
		Type:       eventType,
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
				"feed_event action=waiting source=stream stream_id= group=%s consumer=%s author_id=0 post_id=0 block=%s",
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
	reclaimBatches := r.reclaimBatches
	if reclaimBatches <= 0 {
		reclaimBatches = defaultReclaimBatchPerLoop
	}
	for i := 0; i < reclaimBatches; i++ {
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
		clearRetryCounter := false
		event := FeedInvalidationEvent{}

		decodedEvent, ok := decodeFeedInvalidationEvent(msg.Values["payload"])
		if !ok {
			// Drop malformed messages to prevent poison-message infinite retry.
			shouldAck = true
			r.logFeedEventf("drop_malformed", readSource, msg.ID, FeedInvalidationEvent{}, "")
		} else {
			event = decodedEvent
			event.StreamID = msg.ID
			if !isSupportedFeedInvalidationEventType(event.Type) || event.AuthorID <= 0 {
				// Unknown type/invalid payload: ack and skip.
				shouldAck = true
				r.logFeedEventf("skip_invalid", readSource, msg.ID, event, "type=%s", event.Type)
			} else if err := handler(ctx, event); err == nil {
				shouldAck = true
				clearRetryCounter = true
			} else {
				shouldAck, clearRetryCounter, err = r.handleHandlerFailure(ctx, msg, readSource, event, err)
				if err != nil {
					return err
				}
			}
		}

		if shouldAck {
			if err := r.client.XAck(ctx, feedInvalidationStreamKey, r.groupName, msg.ID).Err(); err != nil {
				r.logFeedEventf("ack_failed", readSource, msg.ID, event, "err=%v", err)
				return fmt.Errorf(
					"ack feed event source=%s stream_id=%s author_id=%d post_id=%d: %w",
					readSource,
					msg.ID,
					event.AuthorID,
					event.PostID,
					err,
				)
			}
			if clearRetryCounter {
				r.clearRetryCounter(ctx, msg.ID)
			}
		}
	}
	return nil
}

func isSupportedFeedInvalidationEventType(eventType string) bool {
	switch eventType {
	case FeedInvalidationEventTypePostCreated, FeedInvalidationEventTypePostDeleted:
		return true
	default:
		return false
	}
}

func (r *FeedInvalidationEventRepository) handleHandlerFailure(
	ctx context.Context,
	msg redis.XMessage,
	readSource string,
	event FeedInvalidationEvent,
	handlerErr error,
) (ack bool, clearRetryCounter bool, err error) {
	retryCount, err := r.bumpRetryCounter(ctx, msg.ID)
	if err != nil {
		return false, false, err
	}

	r.logFeedEventf(
		"handle_failed",
		readSource,
		msg.ID,
		event,
		"retry=%d/%d err=%v",
		retryCount,
		r.retryMax,
		handlerErr,
	)

	if retryCount < int64(r.retryMax) {
		return false, false, nil
	}

	if err := r.pushToDLQ(ctx, msg, readSource, event, retryCount, handlerErr); err != nil {
		return false, false, err
	}

	r.logFeedEventf(
		"move_to_dlq",
		readSource,
		msg.ID,
		event,
		"retry=%d dlq_stream=%s",
		retryCount,
		r.dlqStreamKey,
	)
	// ACK original message only after DLQ write succeeds.
	return true, true, nil
}

func (r *FeedInvalidationEventRepository) bumpRetryCounter(ctx context.Context, streamID string) (int64, error) {
	if strings.TrimSpace(streamID) == "" {
		return 0, fmt.Errorf("stream id cannot be empty")
	}
	key := buildRetryCounterKey(streamID)

	retryCount, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("incr retry counter key=%s stream_id=%s: %w", key, streamID, err)
	}
	if r.retryTTL > 0 {
		if err := r.client.Expire(ctx, key, r.retryTTL).Err(); err != nil {
			return 0, fmt.Errorf("expire retry counter key=%s stream_id=%s: %w", key, streamID, err)
		}
	}
	return retryCount, nil
}

func (r *FeedInvalidationEventRepository) clearRetryCounter(ctx context.Context, streamID string) {
	if strings.TrimSpace(streamID) == "" {
		return
	}
	key := buildRetryCounterKey(streamID)
	if err := r.client.Del(ctx, key).Err(); err != nil {
		log.Printf("feed_event action=clear_retry_failed source=retry stream_id=%s group=%s consumer=%s author_id=0 post_id=0 key=%s err=%v", streamID, r.groupName, r.consumerName, key, err)
	}
}

func (r *FeedInvalidationEventRepository) pushToDLQ(
	ctx context.Context,
	msg redis.XMessage,
	readSource string,
	event FeedInvalidationEvent,
	retryCount int64,
	handlerErr error,
) error {
	if strings.TrimSpace(r.dlqStreamKey) == "" {
		return fmt.Errorf("dlq stream key is empty")
	}

	dlqEvent := feedInvalidationDLQEvent{
		StreamID:   msg.ID,
		Source:     readSource,
		RetryCount: retryCount,
		FailedAt:   time.Now().Unix(),
		LastError:  handlerErr.Error(),
		Event:      event,
		Payload:    stringifyPayload(msg.Values["payload"]),
	}

	payloadBytes, err := json.Marshal(dlqEvent)
	if err != nil {
		return fmt.Errorf("marshal dlq event stream_id=%s: %w", msg.ID, err)
	}
	if err := r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: r.dlqStreamKey,
		Values: map[string]any{
			"payload": string(payloadBytes),
		},
	}).Err(); err != nil {
		return fmt.Errorf("xadd dlq stream=%s stream_id=%s: %w", r.dlqStreamKey, msg.ID, err)
	}
	return nil
}

func buildRetryCounterKey(streamID string) string {
	return fmt.Sprintf("feed:invalidation:retry:%s", streamID)
}

func stringifyPayload(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		bytes, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(bytes)
	}
}

func (r *FeedInvalidationEventRepository) ListDLQEvents(ctx context.Context, count int64) ([]FeedInvalidationDLQRecord, error) {
	if count <= 0 {
		count = 20
	}
	entries, err := r.client.XRevRangeN(ctx, r.dlqStreamKey, "+", "-", count).Result()
	if err != nil {
		return nil, fmt.Errorf("xrevrange dlq stream=%s count=%d: %w", r.dlqStreamKey, count, err)
	}

	records := make([]FeedInvalidationDLQRecord, 0, len(entries))
	for _, entry := range entries {
		record, err := decodeDLQRecord(entry)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (r *FeedInvalidationEventRepository) ReplayDLQEvent(
	ctx context.Context,
	dlqMessageID string,
	deleteAfterReplay bool,
) (*ReplayDLQResult, error) {
	dlqMessageID = strings.TrimSpace(dlqMessageID)
	if dlqMessageID == "" {
		return nil, fmt.Errorf("dlq message id cannot be empty")
	}

	entries, err := r.client.XRangeN(ctx, r.dlqStreamKey, dlqMessageID, dlqMessageID, 1).Result()
	if err != nil {
		return nil, fmt.Errorf("xrange dlq stream=%s message_id=%s: %w", r.dlqStreamKey, dlqMessageID, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: message_id=%s", ErrDLQEventNotFound, dlqMessageID)
	}

	record, err := decodeDLQRecord(entries[0])
	if err != nil {
		return nil, err
	}

	replayPayload := strings.TrimSpace(record.Payload)
	if replayPayload == "" {
		payloadBytes, marshalErr := json.Marshal(record.Event)
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal dlq replay payload message_id=%s: %w", dlqMessageID, marshalErr)
		}
		replayPayload = string(payloadBytes)
	}

	lockKey := buildDLQReplayLockKey(dlqMessageID)
	locked, err := r.client.SetNX(ctx, lockKey, "1", defaultDLQReplayLockTTL).Result()
	if err != nil {
		return nil, fmt.Errorf("setnx replay lock key=%s message_id=%s: %w", lockKey, dlqMessageID, err)
	}
	if !locked {
		return &ReplayDLQResult{
			DLQRecord:       record,
			AlreadyReplayed: true,
		}, nil
	}

	replayedStreamID, err := r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: feedInvalidationStreamKey,
		Values: map[string]any{
			"payload": replayPayload,
		},
	}).Result()
	if err != nil {
		// Roll back idempotency lock to allow retry when replay write actually failed.
		_ = r.client.Del(ctx, lockKey).Err()
		return nil, fmt.Errorf("xadd replay main stream message_id=%s: %w", dlqMessageID, err)
	}

	if deleteAfterReplay {
		if err := r.client.XDel(ctx, r.dlqStreamKey, dlqMessageID).Err(); err != nil {
			return nil, fmt.Errorf("xdel dlq stream=%s message_id=%s: %w", r.dlqStreamKey, dlqMessageID, err)
		}
	}

	return &ReplayDLQResult{
		DLQRecord:        record,
		ReplayedStreamID: replayedStreamID,
	}, nil
}

func decodeDLQRecord(msg redis.XMessage) (FeedInvalidationDLQRecord, error) {
	payload := stringifyPayload(msg.Values["payload"])
	if strings.TrimSpace(payload) == "" {
		return FeedInvalidationDLQRecord{}, fmt.Errorf("decode dlq event message_id=%s: payload missing", msg.ID)
	}

	var dlqEvent feedInvalidationDLQEvent
	if err := json.Unmarshal([]byte(payload), &dlqEvent); err != nil {
		return FeedInvalidationDLQRecord{}, fmt.Errorf("decode dlq event message_id=%s: %w", msg.ID, err)
	}

	return FeedInvalidationDLQRecord{
		MessageID:  msg.ID,
		StreamID:   dlqEvent.StreamID,
		Source:     dlqEvent.Source,
		RetryCount: dlqEvent.RetryCount,
		FailedAt:   dlqEvent.FailedAt,
		LastError:  dlqEvent.LastError,
		Event:      dlqEvent.Event,
		Payload:    dlqEvent.Payload,
	}, nil
}

func buildDLQReplayLockKey(dlqMessageID string) string {
	return fmt.Sprintf("feed:invalidation:dlq:replay:%s", dlqMessageID)
}

func buildOutboxPublishLockKey(outboxEventID int64) string {
	return fmt.Sprintf("feed:invalidation:outbox:published:%d", outboxEventID)
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
	event.StreamID = ""
	return event, true
}
