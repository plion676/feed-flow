package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
	batchSizes  []int
	lastPostID  int64
	lastMaxItem int64
	gotUserIDs  []int64
}

type fakeFeedInboxFanoutLogger struct {
	mu    sync.Mutex
	lines []string
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
	f.batchSizes = append(f.batchSizes, len(userIDs))
	f.lastPostID = postID
	f.lastMaxItem = maxItems
	f.gotUserIDs = append(f.gotUserIDs, userIDs...)
	return f.batchErr
}

func (f *fakeFeedInboxFanoutLogger) Printf(format string, v ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lines = append(f.lines, fmt.Sprintf(format, v...))
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
		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001, 0, -1, 1002, 1001}, 3001, 1713950400); err != nil {
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
		if writer.lastBatchN != 2 {
			t.Fatalf("unexpected batch size after normalization: got=%d want=2", writer.lastBatchN)
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

	t.Run("batch path should log start and done", func(t *testing.T) {
		t.Parallel()

		writer := &fakeFeedInboxBatchWriter{}
		logger := &fakeFeedInboxFanoutLogger{}
		fanout := NewFeedInboxFanout(writer, 1000).WithLogger(logger)
		logCtx := withFeedEventLogFields(ctx, feedEventLogFields{
			StreamID:     "1740000000000-0",
			AuthorUserID: 1001,
			PostID:       3001,
		})

		if err := fanout.FanoutPostToFollowers(logCtx, []int64{1001, 1002, 1003}, 3001, 1713950400); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !containsLogLine(logger.lines, "stream_id=1740000000000-0 author_id=1001 post_id=3001 feed inbox fanout start mode=batch") {
			t.Fatalf("missing batch start log: %v", logger.lines)
		}
		if !containsLogLine(logger.lines, "stream_id=1740000000000-0 author_id=1001 post_id=3001 feed inbox fanout done mode=batch") {
			t.Fatalf("missing batch done log: %v", logger.lines)
		}
	})

	t.Run("batch writer should split large follower list into chunks", func(t *testing.T) {
		t.Parallel()

		writer := &fakeFeedInboxBatchWriter{}
		followerIDs := []int64{1001, 1002, 1003, 1004, 1005}
		fanout := NewFeedInboxFanout(writer, 1000).WithBatchOptions(2, 1)

		if err := fanout.FanoutPostToFollowers(ctx, followerIDs, 3001, 1713950400); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if writer.batchCalls != 3 {
			t.Fatalf("unexpected batch call count: got=%d want=3", writer.batchCalls)
		}
		wantSizes := []int{2, 2, 1}
		if len(writer.batchSizes) != len(wantSizes) {
			t.Fatalf("unexpected batch sizes count: got=%d want=%d", len(writer.batchSizes), len(wantSizes))
		}
		for i, want := range wantSizes {
			if writer.batchSizes[i] != want {
				t.Fatalf("unexpected batch size at %d: got=%d want=%d", i, writer.batchSizes[i], want)
			}
		}
	})

	t.Run("fallback path should dedupe follower ids before single writes", func(t *testing.T) {
		t.Parallel()

		writer := &fakeFeedInboxWriter{}
		fanout := NewFeedInboxFanout(writer, 1000)

		if err := fanout.FanoutPostToFollowers(ctx, []int64{1001, 1002, 1001, 0, -1, 1002}, 3001, 1713950400); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if writer.addCalls != 2 {
			t.Fatalf("unexpected add calls after dedupe: got=%d want=2", writer.addCalls)
		}
		if writer.trimCalls != 2 {
			t.Fatalf("unexpected trim calls after dedupe: got=%d want=2", writer.trimCalls)
		}
	})

	t.Run("single path should log start and done", func(t *testing.T) {
		t.Parallel()

		writer := &fakeFeedInboxWriter{}
		logger := &fakeFeedInboxFanoutLogger{}
		fanout := NewFeedInboxFanout(writer, 1000).WithLogger(logger)
		logCtx := withFeedEventLogFields(ctx, feedEventLogFields{
			StreamID:     "1740000000001-0",
			AuthorUserID: 1002,
			PostID:       3001,
		})

		if err := fanout.FanoutPostToFollowers(logCtx, []int64{1001, 1002}, 3001, 1713950400); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !containsLogLine(logger.lines, "stream_id=1740000000001-0 author_id=1002 post_id=3001 feed inbox fanout start mode=single") {
			t.Fatalf("missing single start log: %v", logger.lines)
		}
		if !containsLogLine(logger.lines, "stream_id=1740000000001-0 author_id=1002 post_id=3001 feed inbox fanout done mode=single") {
			t.Fatalf("missing single done log: %v", logger.lines)
		}
	})

	t.Run("failed batch path should log failure", func(t *testing.T) {
		t.Parallel()

		writer := &fakeFeedInboxBatchWriter{batchErr: errors.New("pipeline failed")}
		logger := &fakeFeedInboxFanoutLogger{}
		fanout := NewFeedInboxFanout(writer, 1000).WithLogger(logger)
		logCtx := withFeedEventLogFields(ctx, feedEventLogFields{
			StreamID:     "1740000000002-0",
			AuthorUserID: 1003,
			PostID:       3001,
		})

		if err := fanout.FanoutPostToFollowers(logCtx, []int64{1001, 1002}, 3001, 1713950400); err == nil {
			t.Fatal("expected batch error")
		}
		if !containsLogLine(logger.lines, "stream_id=1740000000002-0 author_id=1003 post_id=3001 feed inbox fanout failed mode=batch") {
			t.Fatalf("missing batch failure log: %v", logger.lines)
		}
	})
}

func containsLogLine(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}
