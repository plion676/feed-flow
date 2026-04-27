package service

import (
	"context"
	"errors"
	"testing"

	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/repository"
)

type fakeDLQRepo struct {
	listItems  []repository.FeedInvalidationDLQRecord
	listErr    error
	replayRes  *repository.ReplayDLQResult
	replayErr  error
	lastListN  int64
	lastReplay string
	lastDelete bool
}

func (f *fakeDLQRepo) ListDLQEvents(_ context.Context, count int64) ([]repository.FeedInvalidationDLQRecord, error) {
	f.lastListN = count
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]repository.FeedInvalidationDLQRecord, len(f.listItems))
	copy(out, f.listItems)
	return out, nil
}

func (f *fakeDLQRepo) ReplayDLQEvent(_ context.Context, dlqMessageID string, deleteAfterReplay bool) (*repository.ReplayDLQResult, error) {
	f.lastReplay = dlqMessageID
	f.lastDelete = deleteAfterReplay
	if f.replayErr != nil {
		return nil, f.replayErr
	}
	return f.replayRes, nil
}

func TestFeedDLQServiceListEvents(t *testing.T) {
	t.Parallel()

	t.Run("default limit", func(t *testing.T) {
		t.Parallel()
		repo := &fakeDLQRepo{}
		svc := NewFeedDLQService(repo)

		_, bizErr := svc.ListEvents(context.Background(), ListDLQEventsRequest{})
		if bizErr != nil {
			t.Fatalf("unexpected biz error: %v", bizErr)
		}
		if repo.lastListN != defaultDLQListLimit {
			t.Fatalf("unexpected default limit: got=%d want=%d", repo.lastListN, defaultDLQListLimit)
		}
	})

	t.Run("clamp max limit", func(t *testing.T) {
		t.Parallel()
		repo := &fakeDLQRepo{}
		svc := NewFeedDLQService(repo)

		_, bizErr := svc.ListEvents(context.Background(), ListDLQEventsRequest{Limit: 999})
		if bizErr != nil {
			t.Fatalf("unexpected biz error: %v", bizErr)
		}
		if repo.lastListN != maxDLQListLimit {
			t.Fatalf("unexpected clamp limit: got=%d want=%d", repo.lastListN, maxDLQListLimit)
		}
	})

	t.Run("repo failure returns internal", func(t *testing.T) {
		t.Parallel()
		repo := &fakeDLQRepo{listErr: errors.New("redis down")}
		svc := NewFeedDLQService(repo)

		_, bizErr := svc.ListEvents(context.Background(), ListDLQEventsRequest{Limit: 1})
		if bizErr == nil || bizErr.Code != xerror.CodeInternal {
			t.Fatalf("unexpected biz error: %+v", bizErr)
		}
	})
}

func TestFeedDLQServiceReplayEvent(t *testing.T) {
	t.Parallel()

	t.Run("empty message id", func(t *testing.T) {
		t.Parallel()
		svc := NewFeedDLQService(&fakeDLQRepo{})

		_, bizErr := svc.ReplayEvent(context.Background(), ReplayDLQEventRequest{})
		if bizErr == nil || bizErr.Code != xerror.CodeBadRequest {
			t.Fatalf("unexpected biz error: %+v", bizErr)
		}
	})

	t.Run("not found maps to not found", func(t *testing.T) {
		t.Parallel()
		repo := &fakeDLQRepo{
			replayErr: repository.ErrDLQEventNotFound,
		}
		svc := NewFeedDLQService(repo)

		_, bizErr := svc.ReplayEvent(context.Background(), ReplayDLQEventRequest{
			DLQMessageID: "1740000000000-0",
		})
		if bizErr == nil || bizErr.Code != xerror.CodeNotFound {
			t.Fatalf("unexpected biz error: %+v", bizErr)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		repo := &fakeDLQRepo{
			replayRes: &repository.ReplayDLQResult{
				ReplayedStreamID: "1740000001111-0",
			},
		}
		svc := NewFeedDLQService(repo)

		got, bizErr := svc.ReplayEvent(context.Background(), ReplayDLQEventRequest{
			DLQMessageID:      "1740000000000-0",
			DeleteAfterReplay: true,
		})
		if bizErr != nil {
			t.Fatalf("unexpected biz error: %v", bizErr)
		}
		if got == nil || got.ReplayedStreamID != "1740000001111-0" {
			t.Fatalf("unexpected replay result: %+v", got)
		}
		if repo.lastReplay != "1740000000000-0" || !repo.lastDelete {
			t.Fatalf("unexpected replay call args: message_id=%s delete=%v", repo.lastReplay, repo.lastDelete)
		}
	})
}
