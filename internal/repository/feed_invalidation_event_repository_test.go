package repository

import (
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type fakeRedisError string

func (e fakeRedisError) Error() string { return string(e) }
func (e fakeRedisError) RedisError()   {}

func TestIsBusyGroupError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     error
		wantHit bool
	}{
		{
			name:    "nil error",
			err:     nil,
			wantHit: false,
		},
		{
			name:    "busy group redis error",
			err:     fakeRedisError("BUSYGROUP Consumer Group name already exists"),
			wantHit: true,
		},
		{
			name:    "non busy redis error",
			err:     fakeRedisError("NOGROUP No such key"),
			wantHit: false,
		},
		{
			name:    "normal go error",
			err:     errors.New("random error"),
			wantHit: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := isBusyGroupError(tc.err); got != tc.wantHit {
				t.Fatalf("unexpected busy-group detection: got=%v want=%v err=%v", got, tc.wantHit, tc.err)
			}
		})
	}
}

func TestDecodeFeedInvalidationEvent(t *testing.T) {
	t.Parallel()

	validJSON := `{"type":"post_created","author_id":1001,"post_id":3001,"occurred_at":1700000000}`

	tests := []struct {
		name     string
		payload  any
		wantOK   bool
		wantType string
		wantID   int64
		wantPost int64
	}{
		{
			name:     "string payload",
			payload:  validJSON,
			wantOK:   true,
			wantType: "post_created",
			wantID:   1001,
			wantPost: 3001,
		},
		{
			name:     "byte payload",
			payload:  []byte(validJSON),
			wantOK:   true,
			wantType: "post_created",
			wantID:   1001,
			wantPost: 3001,
		},
		{
			name:    "invalid json",
			payload: `{"type":"post_created"`,
			wantOK:  false,
		},
		{
			name:    "unsupported payload type",
			payload: 12345,
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := decodeFeedInvalidationEvent(tc.payload)
			if ok != tc.wantOK {
				t.Fatalf("unexpected decode result: got=%v want=%v payload=%v", ok, tc.wantOK, tc.payload)
			}
			if !tc.wantOK {
				return
			}
			if got.Type != tc.wantType || got.AuthorID != tc.wantID || got.PostID != tc.wantPost {
				t.Fatalf("unexpected event: got=%+v", got)
			}
		})
	}
}

func TestWithConsumerConfig(t *testing.T) {
	t.Parallel()

	repo := &FeedInvalidationEventRepository{
		reclaimIdle:    defaultReclaimMinIdle,
		idleLogAfter:   defaultIdleLogInterval,
		reclaimBatches: defaultReclaimBatchPerLoop,
		retryMax:       defaultRetryMaxAttempts,
		retryTTL:       defaultRetryCounterTTL,
		dlqStreamKey:   defaultFeedInvalidationDLQStreamKey,
	}

	repo = repo.WithConsumerConfig(FeedInvalidationConsumerConfig{
		ReclaimMinIdle:  45 * time.Second,
		IdleLogInterval: 15 * time.Second,
		ReclaimBatches:  9,
		RetryMax:        7,
		RetryTTL:        10 * time.Hour,
		DLQStreamKey:    "custom:dlq:stream",
	})

	if repo.reclaimIdle != 45*time.Second {
		t.Fatalf("unexpected reclaim idle: %s", repo.reclaimIdle)
	}
	if repo.idleLogAfter != 15*time.Second {
		t.Fatalf("unexpected idle log interval: %s", repo.idleLogAfter)
	}
	if repo.reclaimBatches != 9 {
		t.Fatalf("unexpected reclaim batches: %d", repo.reclaimBatches)
	}
	if repo.retryMax != 7 {
		t.Fatalf("unexpected retry max: %d", repo.retryMax)
	}
	if repo.retryTTL != 10*time.Hour {
		t.Fatalf("unexpected retry ttl: %s", repo.retryTTL)
	}
	if repo.dlqStreamKey != "custom:dlq:stream" {
		t.Fatalf("unexpected dlq stream key: %s", repo.dlqStreamKey)
	}
}

func TestBuildRetryCounterKey(t *testing.T) {
	t.Parallel()

	got := buildRetryCounterKey("1777196829098-0")
	want := "feed:invalidation:retry:1777196829098-0"
	if got != want {
		t.Fatalf("unexpected key: got=%s want=%s", got, want)
	}
}

func TestStringifyPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload any
		want    string
	}{
		{name: "string", payload: "abc", want: "abc"},
		{name: "bytes", payload: []byte("xyz"), want: "xyz"},
		{name: "object", payload: map[string]any{"a": 1}, want: `{"a":1}`},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stringifyPayload(tc.payload)
			if got != tc.want {
				t.Fatalf("unexpected payload stringify: got=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestDecodeDLQRecord(t *testing.T) {
	t.Parallel()

	msg := redis.XMessage{
		ID: "1740000000000-0",
		Values: map[string]any{
			"payload": `{"stream_id":"1739999999999-0","source":"xreadgroup","retry_count":5,"failed_at":1740000000,"last_error":"redis timeout","event":{"type":"post_created","author_id":1001,"post_id":3001,"occurred_at":1739999999},"payload":"{\"type\":\"post_created\",\"author_id\":1001,\"post_id\":3001,\"occurred_at\":1739999999}"}`,
		},
	}

	got, err := decodeDLQRecord(msg)
	if err != nil {
		t.Fatalf("unexpected decode error: %v", err)
	}
	if got.MessageID != "1740000000000-0" {
		t.Fatalf("unexpected message id: %s", got.MessageID)
	}
	if got.StreamID != "1739999999999-0" || got.Event.PostID != 3001 {
		t.Fatalf("unexpected decoded record: %+v", got)
	}
}

func TestDecodeDLQRecordInvalidPayload(t *testing.T) {
	t.Parallel()

	msg := redis.XMessage{
		ID: "1740000000000-0",
		Values: map[string]any{
			"payload": `{"stream_id":"broken"`,
		},
	}

	if _, err := decodeDLQRecord(msg); err == nil {
		t.Fatal("expected decode error for broken payload")
	}
}

func TestBuildDLQReplayLockKey(t *testing.T) {
	t.Parallel()

	got := buildDLQReplayLockKey("1740000000000-0")
	want := "feed:invalidation:dlq:replay:1740000000000-0"
	if got != want {
		t.Fatalf("unexpected replay lock key: got=%s want=%s", got, want)
	}
}
