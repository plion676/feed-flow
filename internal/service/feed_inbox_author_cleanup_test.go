package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
)

type fakeInboxAuthorCleanupInboxRepo struct {
	postIDsByUser map[int64][]int64
	listErr       error
	removeErr     error

	lastListUserID int64
	lastListLimit  int
	removedUserID  int64
	removedPostIDs []int64
}

func (f *fakeInboxAuthorCleanupInboxRepo) ListPostIDsByCursor(_ context.Context, userID int64, _ int64, limit int) ([]int64, error) {
	f.lastListUserID = userID
	f.lastListLimit = limit
	if f.listErr != nil {
		return nil, f.listErr
	}
	source := f.postIDsByUser[userID]
	if len(source) > limit {
		source = source[:limit]
	}
	return append([]int64(nil), source...), nil
}

func (f *fakeInboxAuthorCleanupInboxRepo) RemovePostsFromInbox(_ context.Context, userID int64, postIDs []int64) error {
	f.removedUserID = userID
	f.removedPostIDs = append([]int64(nil), postIDs...)
	return f.removeErr
}

type fakeInboxAuthorCleanupPostRepo struct {
	posts      []*model.Post
	err        error
	listByIDsN int
}

func (f *fakeInboxAuthorCleanupPostRepo) ListByIDs(_ context.Context, postIDs []int64) ([]*model.Post, error) {
	f.listByIDsN++
	if f.err != nil {
		return nil, f.err
	}
	byID := make(map[int64]*model.Post, len(f.posts))
	for _, post := range f.posts {
		if post == nil {
			continue
		}
		byID[post.ID] = post
	}
	ordered := make([]*model.Post, 0, len(postIDs))
	for _, postID := range postIDs {
		if post, ok := byID[postID]; ok {
			ordered = append(ordered, post)
		}
	}
	return ordered, nil
}

func TestFeedInboxAuthorCleanupRemoveAuthorPostsFromInbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 5, 5, 10, 0, 0, 0, time.UTC)

	t.Run("should remove target author posts from inbox", func(t *testing.T) {
		t.Parallel()

		inboxRepo := &fakeInboxAuthorCleanupInboxRepo{
			postIDsByUser: map[int64][]int64{
				1001: {12, 11, 10, 9},
			},
		}
		postRepo := &fakeInboxAuthorCleanupPostRepo{
			posts: []*model.Post{
				{ID: 12, UserID: 2002, Content: "a12", Status: 1, CreatedAt: now},
				{ID: 11, UserID: 2001, Content: "b11", Status: 1, CreatedAt: now},
				{ID: 10, UserID: 2002, Content: "a10", Status: 1, CreatedAt: now},
				{ID: 9, UserID: 1001, Content: "self9", Status: 1, CreatedAt: now},
			},
		}

		cleanup := NewFeedInboxAuthorCleanup(inboxRepo, postRepo).WithScanOptions(10, 2)
		if err := cleanup.RemoveAuthorPostsFromInbox(ctx, 1001, 2002); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inboxRepo.removedUserID != 1001 {
			t.Fatalf("unexpected removed user id: got=%d want=%d", inboxRepo.removedUserID, 1001)
		}
		want := []int64{12, 10}
		if len(inboxRepo.removedPostIDs) != len(want) {
			t.Fatalf("unexpected removed post count: got=%d want=%d", len(inboxRepo.removedPostIDs), len(want))
		}
		for i, wantID := range want {
			if inboxRepo.removedPostIDs[i] != wantID {
				t.Fatalf("unexpected removed post id at %d: got=%d want=%d", i, inboxRepo.removedPostIDs[i], wantID)
			}
		}
		if postRepo.listByIDsN != 2 {
			t.Fatalf("expected post repo scanned by batch twice, got=%d", postRepo.listByIDsN)
		}
	})

	t.Run("should no-op when target author has no posts in inbox", func(t *testing.T) {
		t.Parallel()

		inboxRepo := &fakeInboxAuthorCleanupInboxRepo{
			postIDsByUser: map[int64][]int64{
				1001: {12, 11},
			},
		}
		postRepo := &fakeInboxAuthorCleanupPostRepo{
			posts: []*model.Post{
				{ID: 12, UserID: 2003, Status: 1},
				{ID: 11, UserID: 1001, Status: 1},
			},
		}

		cleanup := NewFeedInboxAuthorCleanup(inboxRepo, postRepo)
		if err := cleanup.RemoveAuthorPostsFromInbox(ctx, 1001, 2002); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(inboxRepo.removedPostIDs) != 0 {
			t.Fatalf("expected no removed posts, got=%v", inboxRepo.removedPostIDs)
		}
	})

	t.Run("should return list error", func(t *testing.T) {
		t.Parallel()

		inboxRepo := &fakeInboxAuthorCleanupInboxRepo{listErr: errors.New("redis down")}
		cleanup := NewFeedInboxAuthorCleanup(inboxRepo, &fakeInboxAuthorCleanupPostRepo{})
		if err := cleanup.RemoveAuthorPostsFromInbox(ctx, 1001, 2002); err == nil {
			t.Fatal("expected list error")
		}
	})

	t.Run("should return remove error", func(t *testing.T) {
		t.Parallel()

		inboxRepo := &fakeInboxAuthorCleanupInboxRepo{
			postIDsByUser: map[int64][]int64{
				1001: {12},
			},
			removeErr: errors.New("zrem failed"),
		}
		postRepo := &fakeInboxAuthorCleanupPostRepo{
			posts: []*model.Post{
				{ID: 12, UserID: 2002, Status: 1},
			},
		}
		cleanup := NewFeedInboxAuthorCleanup(inboxRepo, postRepo)
		if err := cleanup.RemoveAuthorPostsFromInbox(ctx, 1001, 2002); err == nil {
			t.Fatal("expected remove error")
		}
	})
}
