package main

import (
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/app"
)

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
