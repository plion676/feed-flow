package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
)

type fakeFeedEventOutboxSource struct {
	rows            [][]*model.FeedEventOutbox
	claimCalled     int
	markSentCalls   []int64
	markFailedCalls []fakeOutboxFailedCall
	claimErr        error
	markSentErr     error
	markFailedErr   error
}

type fakeOutboxFailedCall struct {
	id          int64
	retryCount  int32
	nextRetryAt time.Time
	lastError   string
}

type fakeFeedEventOutboxPublisher struct {
	called       int
	lastOutboxID int64
	lastPayload  string
	err          error
}

func (f *fakeFeedEventOutboxSource) ClaimPending(_ context.Context, _ time.Time, _ int) ([]*model.FeedEventOutbox, error) {
	f.claimCalled++
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if len(f.rows) == 0 {
		return []*model.FeedEventOutbox{}, nil
	}
	current := f.rows[0]
	f.rows = f.rows[1:]
	return current, nil
}

func (f *fakeFeedEventOutboxSource) MarkSent(_ context.Context, id int64, _ time.Time) error {
	f.markSentCalls = append(f.markSentCalls, id)
	return f.markSentErr
}

func (f *fakeFeedEventOutboxSource) MarkPublishFailed(_ context.Context, id int64, retryCount int32, nextRetryAt time.Time, lastError string) error {
	f.markFailedCalls = append(f.markFailedCalls, fakeOutboxFailedCall{
		id:          id,
		retryCount:  retryCount,
		nextRetryAt: nextRetryAt,
		lastError:   lastError,
	})
	return f.markFailedErr
}

func (f *fakeFeedEventOutboxPublisher) PublishEventPayload(_ context.Context, outboxEventID int64, payload string) error {
	f.called++
	f.lastOutboxID = outboxEventID
	f.lastPayload = payload
	return f.err
}

func TestFeedEventOutboxRelayRunPublishesAndMarksSent(t *testing.T) {
	t.Parallel()

	source := &fakeFeedEventOutboxSource{
		rows: [][]*model.FeedEventOutbox{
			{
				{
					ID:         1,
					Payload:    `{"type":"post_created","author_id":1001,"post_id":3001}`,
					RetryCount: 0,
				},
			},
		},
	}
	publisher := &fakeFeedEventOutboxPublisher{}
	relay := NewFeedEventOutboxRelay(source, publisher).
		WithIdleSleep(5 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- relay.Run(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected relay stop error: %v", err)
	}
	if publisher.called != 1 {
		t.Fatalf("expected publisher called once, got=%d", publisher.called)
	}
	if publisher.lastOutboxID != 1 {
		t.Fatalf("unexpected published outbox id: %d", publisher.lastOutboxID)
	}
	if len(source.markSentCalls) != 1 || source.markSentCalls[0] != 1 {
		t.Fatalf("expected mark sent once for id=1, got=%v", source.markSentCalls)
	}
	if len(source.markFailedCalls) != 0 {
		t.Fatalf("unexpected mark failed calls: %+v", source.markFailedCalls)
	}
}

func TestFeedEventOutboxRelayRunMarksPublishFailed(t *testing.T) {
	t.Parallel()

	source := &fakeFeedEventOutboxSource{
		rows: [][]*model.FeedEventOutbox{
			{
				{
					ID:         9,
					Payload:    `{"type":"post_deleted","author_id":1001,"post_id":3001}`,
					RetryCount: 2,
				},
			},
		},
	}
	publisher := &fakeFeedEventOutboxPublisher{err: errors.New("redis down")}
	relay := NewFeedEventOutboxRelay(source, publisher).
		WithRetryBackoff(10*time.Millisecond, 80*time.Millisecond).
		WithIdleSleep(5 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- relay.Run(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected relay stop error: %v", err)
	}
	if len(source.markSentCalls) != 0 {
		t.Fatalf("should not mark sent on publish failure, got=%v", source.markSentCalls)
	}
	if len(source.markFailedCalls) != 1 {
		t.Fatalf("expected one mark failed call, got=%d", len(source.markFailedCalls))
	}
	call := source.markFailedCalls[0]
	if call.id != 9 || call.retryCount != 3 {
		t.Fatalf("unexpected failed call: %+v", call)
	}
	if call.lastError != "redis down" {
		t.Fatalf("unexpected failed error text: %q", call.lastError)
	}
}

func TestFeedEventOutboxRelayResolveRetryBackoff(t *testing.T) {
	t.Parallel()

	relay := NewFeedEventOutboxRelay(&fakeFeedEventOutboxSource{}, &fakeFeedEventOutboxPublisher{}).
		WithRetryBackoff(time.Second, 8*time.Second)

	tests := []struct {
		retryCount int32
		want       time.Duration
	}{
		{retryCount: 1, want: time.Second},
		{retryCount: 2, want: 2 * time.Second},
		{retryCount: 3, want: 4 * time.Second},
		{retryCount: 4, want: 8 * time.Second},
		{retryCount: 5, want: 8 * time.Second},
	}

	for _, tc := range tests {
		if got := relay.resolveRetryBackoff(tc.retryCount); got != tc.want {
			t.Fatalf("unexpected backoff for retry=%d: got=%s want=%s", tc.retryCount, got, tc.want)
		}
	}
}
