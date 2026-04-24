package service

import (
	"context"
	"errors"
	"testing"
)

type fakeFeedInboxWriter struct {
	addErr  error
	trimErr error

	addCalls  int
	trimCalls int
}

type fakeFeedInboxBatchWriter struct {
	fakeFeedInboxWriter
	batchErr    error
	batchCalls  int
	lastBatchN  int
	lastPostID  int64
	lastMaxItem int64
}

func (f *fakeFeedInboxWriter) AddPostToInbox(_ context.Context, _ int64, _ int64, _ int64) error {
	f.addCalls++
	return f.addErr
}

func (f *fakeFeedInboxWriter) TrimInbox(_ context.Context, _ int64, _ int64) error {
	f.trimCalls++
	return f.trimErr
}

func (f *fakeFeedInboxBatchWriter) BatchAddPostToInboxes(_ context.Context, userIDs []int64, postID int64, _ int64, maxItems int64) error {
	f.batchCalls++
	f.lastBatchN = len(userIDs)
	f.lastPostID = postID
	f.lastMaxItem = maxItems
	return f.batchErr
}

func TestFeedInboxFanoutFanoutPostToFollowers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("error when dependencies missing", func(t *testing.T) {
		t.Parallel()
		fanout := NewFeedInboxFanout(nil, 1000)
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001}, 3001, 1713950400); err == nil {
			t.Fatal("expected error when dependencies are missing")
		}
	})

	t.Run("error when post id invalid", func(t *testing.T) {
		t.Parallel()
		fanout := NewFeedInboxFanout(&fakeFeedInboxWriter{}, 1000)
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001}, 0, 1713950400); err == nil {
			t.Fatal("expected error when post id is invalid")
		}
	})

	t.Run("error when add fails", func(t *testing.T) {
		t.Parallel()
		writer := &fakeFeedInboxWriter{addErr: errors.New("redis add failed")}
		fanout := NewFeedInboxFanout(writer, 1000)
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001}, 3001, 1713950400); err == nil {
			t.Fatal("expected add error")
		}
	})

	t.Run("error when trim fails", func(t *testing.T) {
		t.Parallel()
		writer := &fakeFeedInboxWriter{trimErr: errors.New("redis trim failed")}
		fanout := NewFeedInboxFanout(writer, 1000)
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001}, 3001, 1713950400); err == nil {
			t.Fatal("expected trim error")
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		writer := &fakeFeedInboxWriter{}
		fanout := NewFeedInboxFanout(writer, 1000)
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001, 1002}, 3001, 1713950400); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if writer.addCalls != 2 {
			t.Fatalf("unexpected add calls: got=%d want=2", writer.addCalls)
		}
		if writer.trimCalls != 2 {
			t.Fatalf("unexpected trim calls: got=%d want=2", writer.trimCalls)
		}
	})

	t.Run("batch writer should be preferred when supported", func(t *testing.T) {
		t.Parallel()
		writer := &fakeFeedInboxBatchWriter{}
		fanout := NewFeedInboxFanout(writer, 1000)
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001, 0, -1, 1002}, 3001, 1713950400); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if writer.batchCalls == 0 {
			t.Fatal("expected batch writer to be called")
		}
		if writer.addCalls != 0 || writer.trimCalls != 0 {
			t.Fatalf("single write path should not be used when batch is available, add=%d trim=%d", writer.addCalls, writer.trimCalls)
		}
		if writer.lastPostID != 3001 {
			t.Fatalf("unexpected post id in batch: got=%d", writer.lastPostID)
		}
		if writer.lastMaxItem != 1000 {
			t.Fatalf("unexpected max items in batch: got=%d", writer.lastMaxItem)
		}
	})

	t.Run("batch writer error should fail request", func(t *testing.T) {
		t.Parallel()
		writer := &fakeFeedInboxBatchWriter{batchErr: errors.New("pipeline failed")}
		fanout := NewFeedInboxFanout(writer, 1000)
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001, 1002}, 3001, 1713950400); err == nil {
			t.Fatal("expected batch error")
		}
	})
}
