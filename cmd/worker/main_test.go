package main

import (
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/app"
)

func intPtr(v int) *int {
	return &v
}

func TestNextRetryBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current time.Duration
		initial time.Duration
		max     time.Duration
		want    time.Duration
	}{
		{
			name:    "double under max",
			current: 1 * time.Second,
			initial: 1 * time.Second,
			max:     30 * time.Second,
			want:    2 * time.Second,
		},
		{
			name:    "cap at max",
			current: 20 * time.Second,
			initial: 1 * time.Second,
			max:     30 * time.Second,
			want:    30 * time.Second,
		},
		{
			name:    "non-positive current returns initial",
			current: 0,
			initial: 2 * time.Second,
			max:     30 * time.Second,
			want:    2 * time.Second,
		},
		{
			name:    "non-positive max returns current",
			current: 3 * time.Second,
			initial: 1 * time.Second,
			max:     0,
			want:    3 * time.Second,
		},
		{
			name:    "initial capped by max",
			current: 0,
			initial: 10 * time.Second,
			max:     3 * time.Second,
			want:    3 * time.Second,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := nextRetryBackoff(tc.current, tc.initial, tc.max)
			if got != tc.want {
				t.Fatalf("unexpected backoff: got=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestClampPercent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "negative", input: -1, want: 0},
		{name: "normal", input: 20, want: 20},
		{name: "over max", input: 999, want: 100},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clampPercent(tc.input)
			if got != tc.want {
				t.Fatalf("unexpected clamp: got=%d want=%d", got, tc.want)
			}
		})
	}
}

func TestBuildEventConsumerConfig(t *testing.T) {
	t.Parallel()

	cfg := &app.Config{
		Feed: app.FeedConfig{
			Hybrid: app.FeedHybridConfig{
				PushFollowerThreshold: 100,
				Mix: app.FeedMixConfig{
					PushRatioNumerator:   2,
					PushRatioDenominator: 3,
					MinPullItems:         intPtr(1),
					MaxConsecutiveAuthor: 2,
				},
			},
			Worker: app.FeedWorkerConfig{
				ReclaimMinIdleSeconds:  40,
				IdleLogIntervalSeconds: 12,
				ReclaimBatchPerLoop:    8,
				RetryMaxAttempts:       6,
				RetryCounterTTLSeconds: 7200,
				DLQStreamKey:           "feed:invalidation:dlq:custom",
			},
		},
	}

	got := buildEventConsumerConfig(cfg)
	if got.ReclaimMinIdle != 40*time.Second {
		t.Fatalf("unexpected reclaim idle: %s", got.ReclaimMinIdle)
	}
	if got.IdleLogInterval != 12*time.Second {
		t.Fatalf("unexpected idle log interval: %s", got.IdleLogInterval)
	}
	if got.ReclaimBatches != 8 {
		t.Fatalf("unexpected reclaim batches: %d", got.ReclaimBatches)
	}
	if got.RetryMax != 6 {
		t.Fatalf("unexpected retry max: %d", got.RetryMax)
	}
	if got.RetryTTL != 7200*time.Second {
		t.Fatalf("unexpected retry ttl: %s", got.RetryTTL)
	}
	if got.DLQStreamKey != "feed:invalidation:dlq:custom" {
		t.Fatalf("unexpected dlq stream key: %s", got.DLQStreamKey)
	}
}

func TestBuildInboxFanoutOptions(t *testing.T) {
	t.Parallel()

	t.Run("should map inbox fanout config", func(t *testing.T) {
		t.Parallel()

		cfg := &app.Config{
			Feed: app.FeedConfig{
				Inbox: app.FeedInboxConfig{
					Enabled:   true,
					MaxItems:  1000,
					BatchSize: 256,
					Workers:   12,
				},
			},
		}

		got := buildInboxFanoutOptions(cfg)
		if got.MaxItems != 1000 {
			t.Fatalf("unexpected max items: got=%d want=1000", got.MaxItems)
		}
		if got.BatchSize != 256 {
			t.Fatalf("unexpected batch size: got=%d want=256", got.BatchSize)
		}
		if got.Workers != 12 {
			t.Fatalf("unexpected workers: got=%d want=12", got.Workers)
		}
	})

	t.Run("nil config should return zero options", func(t *testing.T) {
		t.Parallel()

		got := buildInboxFanoutOptions(nil)
		if got != (inboxFanoutOptions{}) {
			t.Fatalf("unexpected zero options: %+v", got)
		}
	})
}
