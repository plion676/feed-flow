package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeWorkerFollowRepo struct {
	followerIDs []int64
	err         error

	called       int
	gotTargetUID int64
}

func (r *fakeWorkerFollowRepo) ListFollowerUserIDs(_ context.Context, targetUserID int64) ([]int64, error) {
	r.called++
	r.gotTargetUID = targetUserID
	if r.err != nil {
		return nil, r.err
	}
	out := make([]int64, len(r.followerIDs))
	copy(out, r.followerIDs)
	return out, nil
}

type fakeWorkerInvalidator struct {
	failByFollower map[int64]error
	delay          time.Duration

	mu          sync.Mutex
	calledIDs   []int64
	activeCount int
	maxActive   int
}

type fakeWorkerInboxFanout struct {
	called         int
	gotFollowerIDs []int64
	gotPostID      int64
	gotOccurredAt  int64
	gotStreamID    string
	err            error
}

type fakeWorkerInboxCleanup struct {
	called         int
	gotFollowerIDs []int64
	gotPostID      int64
	gotStreamID    string
	err            error
}

type fakeWorkerOutboxRepo struct {
	addCalled    int
	removeCalled int
	trimCalled   int
	gotAuthorID  int64
	gotPostID    int64
	gotMaxItems  int64
	addErr       error
	removeErr    error
	trimErr      error
}

func (f *fakeWorkerInboxFanout) FanoutPostToFollowers(ctx context.Context, followerIDs []int64, postID int64, occurredAt int64) error {
	f.called++
	f.gotFollowerIDs = append([]int64{}, followerIDs...)
	f.gotPostID = postID
	f.gotOccurredAt = occurredAt
	f.gotStreamID = getFeedEventLogFields(ctx).StreamID
	return f.err
}

func (f *fakeWorkerInboxCleanup) RemovePostFromFollowers(ctx context.Context, followerIDs []int64, postID int64) error {
	f.called++
	f.gotFollowerIDs = append([]int64{}, followerIDs...)
	f.gotPostID = postID
	f.gotStreamID = getFeedEventLogFields(ctx).StreamID
	return f.err
}

func (f *fakeWorkerOutboxRepo) AddPostToOutbox(_ context.Context, authorUserID int64, postID int64) error {
	f.addCalled++
	f.gotAuthorID = authorUserID
	f.gotPostID = postID
	return f.addErr
}

func (f *fakeWorkerOutboxRepo) RemovePostFromOutbox(_ context.Context, authorUserID int64, postID int64) error {
	f.removeCalled++
	f.gotAuthorID = authorUserID
	f.gotPostID = postID
	return f.removeErr
}

func (f *fakeWorkerOutboxRepo) TrimOutbox(_ context.Context, authorUserID int64, maxItems int64) error {
	f.trimCalled++
	f.gotAuthorID = authorUserID
	f.gotMaxItems = maxItems
	return f.trimErr
}

func (f *fakeWorkerInvalidator) InvalidateHomeFeed(_ context.Context, userID int64) error {
	f.mu.Lock()
	f.calledIDs = append(f.calledIDs, userID)
	f.activeCount++
	if f.activeCount > f.maxActive {
		f.maxActive = f.activeCount
	}
	f.mu.Unlock()

	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.activeCount--

	if f.failByFollower != nil {
		if err, ok := f.failByFollower[userID]; ok {
			return err
		}
	}
	return nil
}

func (f *fakeWorkerInvalidator) CalledCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calledIDs)
}

func (f *fakeWorkerInvalidator) MaxActive() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxActive
}

func TestFeedInvalidationWorkerHandlePostCreated(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("no-op when author id invalid", func(t *testing.T) {
		t.Parallel()

		worker := NewFeedInvalidationWorker(&fakeWorkerFollowRepo{}, &fakeWorkerInvalidator{})
		if err := worker.HandlePostCreated(ctx, 0); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("error when dependencies missing", func(t *testing.T) {
		t.Parallel()

		worker := NewFeedInvalidationWorker(nil, nil)
		if err := worker.HandlePostCreated(ctx, 1001); err == nil {
			t.Fatal("expected error when dependencies are missing")
		}
	})

	t.Run("return error when follow repository fails", func(t *testing.T) {
		t.Parallel()

		worker := NewFeedInvalidationWorker(
			&fakeWorkerFollowRepo{err: errors.New("query failed")},
			&fakeWorkerInvalidator{},
		)

		if err := worker.HandlePostCreated(ctx, 1001); err == nil {
			t.Fatal("expected repository error")
		}
	})

	t.Run("no-op when author has no followers", func(t *testing.T) {
		t.Parallel()

		followRepo := &fakeWorkerFollowRepo{followerIDs: []int64{}}
		invalidator := &fakeWorkerInvalidator{}
		worker := NewFeedInvalidationWorker(followRepo, invalidator)

		if err := worker.HandlePostCreated(ctx, 1001); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if invalidator.CalledCount() != 0 {
			t.Fatalf("expected no invalidation calls, got=%d", invalidator.CalledCount())
		}
	})

	t.Run("success invalidates all follower home feeds", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002, 2003, 2004}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{}
		worker := NewFeedInvalidationWorker(followRepo, invalidator)

		if err := worker.HandlePostCreated(ctx, 1001); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if invalidator.CalledCount() != len(followerIDs) {
			t.Fatalf("unexpected invalidation call count: got=%d want=%d", invalidator.CalledCount(), len(followerIDs))
		}
	})

	t.Run("collect partial failures", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002, 2003}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{
			failByFollower: map[int64]error{
				2002: errors.New("redis timeout"),
			},
		}
		worker := NewFeedInvalidationWorker(followRepo, invalidator)

		err := worker.HandlePostCreated(ctx, 1001)
		if err == nil {
			t.Fatal("expected partial failure error")
		}
		if invalidator.CalledCount() != len(followerIDs) {
			t.Fatalf("all followers should still be attempted, got=%d want=%d", invalidator.CalledCount(), len(followerIDs))
		}
	})

	t.Run("bounded concurrency should not exceed worker limit", func(t *testing.T) {
		t.Parallel()

		followerIDs := make([]int64, 50)
		for i := range followerIDs {
			followerIDs[i] = int64(3000 + i)
		}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{delay: 20 * time.Millisecond}
		worker := NewFeedInvalidationWorker(followRepo, invalidator)

		if err := worker.HandlePostCreated(ctx, 1001); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if invalidator.MaxActive() > defaultFollowerInvalidationWorkers {
			t.Fatalf("max active workers exceeded limit: got=%d limit=%d", invalidator.MaxActive(), defaultFollowerInvalidationWorkers)
		}
	})

	t.Run("push mode should call inbox fanout when event has post id", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{}
		fanout := &fakeWorkerInboxFanout{}
		outbox := &fakeWorkerOutboxRepo{}
		worker := NewFeedInvalidationWorker(followRepo, invalidator).
			WithHybridPolicy(NewFeedHybridPolicy(100)).
			WithInboxFanout(fanout).
			WithOutbox(outbox, 500)

		err := worker.HandlePostCreatedEvent(ctx, PostCreatedEvent{
			StreamID:     "1740000000000-0",
			AuthorUserID: 1001,
			PostID:       3001,
			OccurredAt:   1713950400,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fanout.called != 1 {
			t.Fatalf("expected fanout called once, got=%d", fanout.called)
		}
		if fanout.gotPostID != 3001 {
			t.Fatalf("unexpected fanout post id: got=%d", fanout.gotPostID)
		}
		if fanout.gotStreamID != "1740000000000-0" {
			t.Fatalf("unexpected fanout stream id: got=%q", fanout.gotStreamID)
		}
		if outbox.addCalled != 1 {
			t.Fatalf("expected outbox add called once, got=%d", outbox.addCalled)
		}
		if outbox.trimCalled != 1 {
			t.Fatalf("expected outbox trim called once, got=%d", outbox.trimCalled)
		}
		if outbox.gotAuthorID != 1001 || outbox.gotPostID != 3001 {
			t.Fatalf("unexpected outbox write args: author=%d post=%d", outbox.gotAuthorID, outbox.gotPostID)
		}
		if outbox.gotMaxItems != 500 {
			t.Fatalf("unexpected outbox trim max items: got=%d want=500", outbox.gotMaxItems)
		}
		if len(fanout.gotFollowerIDs) != len(followerIDs) {
			t.Fatalf("unexpected fanout follower count: got=%d want=%d", len(fanout.gotFollowerIDs), len(followerIDs))
		}
	})

	t.Run("pull mode should skip inbox fanout", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{}
		fanout := &fakeWorkerInboxFanout{}
		outbox := &fakeWorkerOutboxRepo{}
		worker := NewFeedInvalidationWorker(followRepo, invalidator).
			WithHybridPolicy(NewFeedHybridPolicy(1)).
			WithInboxFanout(fanout).
			WithOutbox(outbox, 500)

		err := worker.HandlePostCreatedEvent(ctx, PostCreatedEvent{
			AuthorUserID: 1001,
			PostID:       3001,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fanout.called != 0 {
			t.Fatalf("expected fanout not called in pull mode, got=%d", fanout.called)
		}
		if outbox.addCalled != 1 {
			t.Fatalf("expected outbox add in pull mode, got=%d", outbox.addCalled)
		}
	})

	t.Run("post deleted should invalidate followers without inbox fanout", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{}
		fanout := &fakeWorkerInboxFanout{}
		cleanup := &fakeWorkerInboxCleanup{}
		outbox := &fakeWorkerOutboxRepo{}
		worker := NewFeedInvalidationWorker(followRepo, invalidator).
			WithHybridPolicy(NewFeedHybridPolicy(100)).
			WithInboxFanout(fanout).
			WithInboxCleanup(cleanup).
			WithOutbox(outbox, 500)

		err := worker.HandlePostDeletedEvent(ctx, PostLifecycleEvent{
			StreamID:     "1740000000001-0",
			AuthorUserID: 1001,
			PostID:       3001,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if fanout.called != 0 {
			t.Fatalf("delete event should not fanout inbox writes, got=%d", fanout.called)
		}
		if cleanup.called != 1 {
			t.Fatalf("delete event should cleanup inbox once, got=%d", cleanup.called)
		}
		if cleanup.gotPostID != 3001 {
			t.Fatalf("unexpected cleanup post id: got=%d want=%d", cleanup.gotPostID, 3001)
		}
		if cleanup.gotStreamID != "1740000000001-0" {
			t.Fatalf("unexpected cleanup stream id: got=%q", cleanup.gotStreamID)
		}
		if outbox.removeCalled != 1 {
			t.Fatalf("expected outbox remove called once, got=%d", outbox.removeCalled)
		}
		if outbox.gotAuthorID != 1001 || outbox.gotPostID != 3001 {
			t.Fatalf("unexpected outbox remove args: author=%d post=%d", outbox.gotAuthorID, outbox.gotPostID)
		}
		if invalidator.CalledCount() != len(followerIDs) {
			t.Fatalf("unexpected invalidation call count: got=%d want=%d", invalidator.CalledCount(), len(followerIDs))
		}
	})

	t.Run("outbox add failure should return early", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{}
		outbox := &fakeWorkerOutboxRepo{addErr: errors.New("redis down")}
		worker := NewFeedInvalidationWorker(followRepo, invalidator).
			WithOutbox(outbox, 500)

		err := worker.HandlePostCreatedEvent(ctx, PostLifecycleEvent{
			AuthorUserID: 1001,
			PostID:       3001,
		})
		if err == nil {
			t.Fatal("expected outbox add error")
		}
		if followRepo.called != 0 {
			t.Fatalf("follow repo should not be called after outbox failure, got=%d", followRepo.called)
		}
		if invalidator.CalledCount() != 0 {
			t.Fatalf("invalidator should not be called after outbox failure, got=%d", invalidator.CalledCount())
		}
	})

	t.Run("outbox remove failure should return early", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{}
		outbox := &fakeWorkerOutboxRepo{removeErr: errors.New("redis down")}
		worker := NewFeedInvalidationWorker(followRepo, invalidator).
			WithOutbox(outbox, 500)

		err := worker.HandlePostDeletedEvent(ctx, PostLifecycleEvent{
			AuthorUserID: 1001,
			PostID:       3001,
		})
		if err == nil {
			t.Fatal("expected outbox remove error")
		}
		if followRepo.called != 0 {
			t.Fatalf("follow repo should not be called after outbox remove failure, got=%d", followRepo.called)
		}
		if invalidator.CalledCount() != 0 {
			t.Fatalf("invalidator should not be called after outbox remove failure, got=%d", invalidator.CalledCount())
		}
	})

	t.Run("delete cleanup failure should still attempt feed invalidation and return error", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{}
		cleanup := &fakeWorkerInboxCleanup{err: errors.New("cleanup failed")}
		worker := NewFeedInvalidationWorker(followRepo, invalidator).
			WithInboxCleanup(cleanup)

		err := worker.HandlePostDeletedEvent(ctx, PostLifecycleEvent{
			AuthorUserID: 1001,
			PostID:       3001,
		})
		if err == nil {
			t.Fatal("expected cleanup error")
		}
		if cleanup.called != 1 {
			t.Fatalf("expected cleanup called once, got=%d", cleanup.called)
		}
		if invalidator.CalledCount() != len(followerIDs) {
			t.Fatalf("feed invalidation should still be attempted, got=%d want=%d", invalidator.CalledCount(), len(followerIDs))
		}
	})

	t.Run("delete cleanup error should join invalidation error when both fail", func(t *testing.T) {
		t.Parallel()

		followerIDs := []int64{2001, 2002}
		followRepo := &fakeWorkerFollowRepo{followerIDs: followerIDs}
		invalidator := &fakeWorkerInvalidator{
			failByFollower: map[int64]error{
				2001: errors.New("invalidate failed"),
			},
		}
		cleanup := &fakeWorkerInboxCleanup{err: errors.New("cleanup failed")}
		worker := NewFeedInvalidationWorker(followRepo, invalidator).
			WithInboxCleanup(cleanup)

		err := worker.HandlePostDeletedEvent(ctx, PostLifecycleEvent{
			AuthorUserID: 1001,
			PostID:       3001,
		})
		if err == nil {
			t.Fatal("expected joined error")
		}
		if invalidator.CalledCount() != len(followerIDs) {
			t.Fatalf("all invalidations should still be attempted, got=%d want=%d", invalidator.CalledCount(), len(followerIDs))
		}
	})
}
