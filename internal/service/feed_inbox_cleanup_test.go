package service

import (
	"context"
	"errors"
	"testing"
)

type fakeFeedInboxCleanupWriter struct {
	removeErr     error
	calledUserIDs []int64
	gotPostID     int64
}

func (f *fakeFeedInboxCleanupWriter) RemovePostFromInbox(_ context.Context, userID int64, postID int64) error {
	f.calledUserIDs = append(f.calledUserIDs, userID)
	f.gotPostID = postID
	return f.removeErr
}

type fakeFeedInboxBatchCleanupWriter struct {
	batchRemoveErr error
	batchUserIDs   [][]int64
	gotPostID      int64
}

func (f *fakeFeedInboxBatchCleanupWriter) RemovePostFromInbox(_ context.Context, _ int64, _ int64) error {
	return nil
}

func (f *fakeFeedInboxBatchCleanupWriter) BatchRemovePostFromInboxes(_ context.Context, userIDs []int64, postID int64) error {
	f.batchUserIDs = append(f.batchUserIDs, append([]int64(nil), userIDs...))
	f.gotPostID = postID
	return f.batchRemoveErr
}

func TestFeedInboxCleanupRemovePostFromFollowers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("error when dependencies missing", func(t *testing.T) {
		t.Parallel()

		cleanup := NewFeedInboxCleanup(nil)
		if err := cleanup.RemovePostFromFollowers(ctx, []int64{2001}, 3001); err == nil {
			t.Fatal("expected missing dependency error")
		}
	})

	t.Run("single mode should remove unique valid followers", func(t *testing.T) {
		t.Parallel()

		repo := &fakeFeedInboxCleanupWriter{}
		cleanup := NewFeedInboxCleanup(repo)
		if err := cleanup.RemovePostFromFollowers(ctx, []int64{2001, 2001, 0, 2002}, 3001); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if repo.gotPostID != 3001 {
			t.Fatalf("unexpected post id: got=%d want=3001", repo.gotPostID)
		}
		if len(repo.calledUserIDs) != 2 {
			t.Fatalf("unexpected remove call count: got=%d want=2", len(repo.calledUserIDs))
		}
	})

	t.Run("single mode should return first removal error", func(t *testing.T) {
		t.Parallel()

		repo := &fakeFeedInboxCleanupWriter{removeErr: errors.New("redis timeout")}
		cleanup := NewFeedInboxCleanup(repo)
		if err := cleanup.RemovePostFromFollowers(ctx, []int64{2001}, 3001); err == nil {
			t.Fatal("expected cleanup error")
		}
	})

	t.Run("batch mode should split followers by chunk", func(t *testing.T) {
		t.Parallel()

		repo := &fakeFeedInboxBatchCleanupWriter{}
		cleanup := NewFeedInboxCleanup(repo).WithBatchOptions(2, 2)
		if err := cleanup.RemovePostFromFollowers(ctx, []int64{2001, 2002, 2003}, 3001); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(repo.batchUserIDs) != 2 {
			t.Fatalf("unexpected batch chunk count: got=%d want=2", len(repo.batchUserIDs))
		}
	})

	t.Run("batch mode should return batch error", func(t *testing.T) {
		t.Parallel()

		repo := &fakeFeedInboxBatchCleanupWriter{batchRemoveErr: errors.New("pipeline failed")}
		cleanup := NewFeedInboxCleanup(repo).WithBatchOptions(2, 1)
		if err := cleanup.RemovePostFromFollowers(ctx, []int64{2001, 2002}, 3001); err == nil {
			t.Fatal("expected batch cleanup error")
		}
	})
}
