package repository

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestFeedOutboxRepositoryAddListTrimAndRemove(t *testing.T) {
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
		DB:   15,
	})
	const authorUserID int64 = 992001
	t.Cleanup(func() {
		_ = client.Del(ctx, buildFeedOutboxKey(authorUserID)).Err()
		_ = client.Close()
	})

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis is not available for repository integration test: %v", err)
	}
	if err := client.Del(ctx, buildFeedOutboxKey(authorUserID)).Err(); err != nil {
		t.Fatalf("cleanup outbox key: %v", err)
	}

	repo := NewFeedOutboxRepository(client)

	t.Run("add and list should keep latest-first order by post id", func(t *testing.T) {
		if err := client.Del(ctx, buildFeedOutboxKey(authorUserID)).Err(); err != nil {
			t.Fatalf("clear outbox key failed: %v", err)
		}

		for _, postID := range []int64{3001, 3003, 3002} {
			if err := repo.AddPostToOutbox(ctx, authorUserID, postID); err != nil {
				t.Fatalf("add post_id=%d failed: %v", postID, err)
			}
		}

		postIDs, err := repo.ListPostIDsByCursor(ctx, authorUserID, 0, 10)
		if err != nil {
			t.Fatalf("list post ids failed: %v", err)
		}
		want := []int64{3003, 3002, 3001}
		if len(postIDs) != len(want) {
			t.Fatalf("unexpected outbox size: got=%d want=%d", len(postIDs), len(want))
		}
		for i, wantID := range want {
			if postIDs[i] != wantID {
				t.Fatalf("unexpected post id at %d: got=%d want=%d", i, postIDs[i], wantID)
			}
		}
	})

	t.Run("cursor read should only return post ids smaller than maxPostID", func(t *testing.T) {
		postIDs, err := repo.ListPostIDsByCursor(ctx, authorUserID, 3003, 10)
		if err != nil {
			t.Fatalf("list by cursor failed: %v", err)
		}
		want := []int64{3002, 3001}
		if len(postIDs) != len(want) {
			t.Fatalf("unexpected cursor result size: got=%d want=%d", len(postIDs), len(want))
		}
		for i, wantID := range want {
			if postIDs[i] != wantID {
				t.Fatalf("unexpected cursor result at %d: got=%d want=%d", i, postIDs[i], wantID)
			}
		}
	})

	t.Run("trim should keep latest maxItems by post id", func(t *testing.T) {
		if err := repo.TrimOutbox(ctx, authorUserID, 2); err != nil {
			t.Fatalf("trim outbox failed: %v", err)
		}

		postIDs, err := repo.ListPostIDsByCursor(ctx, authorUserID, 0, 10)
		if err != nil {
			t.Fatalf("list after trim failed: %v", err)
		}
		want := []int64{3003, 3002}
		if len(postIDs) != len(want) {
			t.Fatalf("unexpected outbox size after trim: got=%d want=%d", len(postIDs), len(want))
		}
		for i, wantID := range want {
			if postIDs[i] != wantID {
				t.Fatalf("unexpected trimmed post id at %d: got=%d want=%d", i, postIDs[i], wantID)
			}
		}
	})

	t.Run("remove should delete target post id from outbox", func(t *testing.T) {
		if err := repo.RemovePostFromOutbox(ctx, authorUserID, 3003); err != nil {
			t.Fatalf("remove post from outbox failed: %v", err)
		}

		postIDs, err := repo.ListPostIDsByCursor(ctx, authorUserID, 0, 10)
		if err != nil {
			t.Fatalf("list after remove failed: %v", err)
		}
		want := []int64{3002}
		if len(postIDs) != len(want) {
			t.Fatalf("unexpected outbox size after remove: got=%d want=%d", len(postIDs), len(want))
		}
		for i, wantID := range want {
			if postIDs[i] != wantID {
				t.Fatalf("unexpected remaining post id at %d: got=%d want=%d", i, postIDs[i], wantID)
			}
		}
	})

	t.Run("repeated add of same post id should not create duplicates", func(t *testing.T) {
		if err := repo.AddPostToOutbox(ctx, authorUserID, 3002); err != nil {
			t.Fatalf("repeat add failed: %v", err)
		}

		postIDs, err := repo.ListPostIDsByCursor(ctx, authorUserID, 0, 10)
		if err != nil {
			t.Fatalf("list after repeated add failed: %v", err)
		}
		if len(postIDs) != 1 {
			t.Fatalf("unexpected outbox size after repeated add: got=%d want=1", len(postIDs))
		}
		if postIDs[0] != 3002 {
			t.Fatalf("unexpected remaining post id: got=%d want=3002", postIDs[0])
		}
	})
}
