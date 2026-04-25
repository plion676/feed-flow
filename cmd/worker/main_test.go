package main

import (
	"testing"
	"time"
)

func TestNextRetryBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		current time.Duration
		max     time.Duration
		want    time.Duration
	}{
		{
			name:    "double under max",
			current: 1 * time.Second,
			max:     30 * time.Second,
			want:    2 * time.Second,
		},
		{
			name:    "cap at max",
			current: 20 * time.Second,
			max:     30 * time.Second,
			want:    30 * time.Second,
		},
		{
			name:    "non-positive current returns initial",
			current: 0,
			max:     30 * time.Second,
			want:    1 * time.Second,
		},
		{
			name:    "non-positive max returns current",
			current: 3 * time.Second,
			max:     0,
			want:    3 * time.Second,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := nextRetryBackoff(tc.current, tc.max)
			if got != tc.want {
				t.Fatalf("unexpected backoff: got=%s want=%s", got, tc.want)
			}
		})
	}
}
