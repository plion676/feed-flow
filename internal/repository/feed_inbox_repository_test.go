package repository

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestFeedInboxRepositoryBatchAddPostToInboxes(t *testing.T) {
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
		DB:   15,
	})
	const idempotentUserID int64 = 991001
	const trimUserID int64 = 991002
	t.Cleanup(func() {
		_ = client.Del(ctx, buildFeedInboxKey(idempotentUserID), buildFeedInboxKey(trimUserID)).Err()
		_ = client.Close()
	})

	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis is not available for repository integration test: %v", err)
	}
	if err := client.Del(ctx, buildFeedInboxKey(idempotentUserID), buildFeedInboxKey(trimUserID)).Err(); err != nil {
		t.Fatalf("cleanup inbox keys: %v", err)
	}

	repo := NewFeedInboxRepository(client)

	t.Run("repeated batch fanout should be idempotent for same post id", func(t *testing.T) {
		if err := repo.BatchAddPostToInboxes(ctx, []int64{idempotentUserID}, 3001, 1713950400, 10); err != nil {
			t.Fatalf("first batch add failed: %v", err)
		}
		if err := repo.BatchAddPostToInboxes(ctx, []int64{idempotentUserID}, 3001, 1713950500, 10); err != nil {
			t.Fatalf("second batch add failed: %v", err)
		}

		members, err := client.ZRevRange(ctx, buildFeedInboxKey(idempotentUserID), 0, -1).Result()
		if err != nil {
			t.Fatalf("read inbox members failed: %v", err)
		}
		if len(members) != 1 {
			t.Fatalf("unexpected member count after repeated fanout: got=%d want=1", len(members))
		}
		if members[0] != "3001" {
			t.Fatalf("unexpected inbox member: got=%s want=3001", members[0])
		}
	})

	t.Run("batch fanout should trim oldest overflow and keep latest max items", func(t *testing.T) {
		if err := client.Del(ctx, buildFeedInboxKey(trimUserID)).Err(); err != nil {
			t.Fatalf("clear inbox key failed: %v", err)
		}

		for i, postID := range []int64{3001, 3002, 3003, 3004} {
			if err := repo.BatchAddPostToInboxes(ctx, []int64{trimUserID}, postID, 1713950400+int64(i), 3); err != nil {
				t.Fatalf("batch add post_id=%d failed: %v", postID, err)
			}
		}

		postIDs, err := repo.ListPostIDsByCursor(ctx, trimUserID, 0, 10)
		if err != nil {
			t.Fatalf("list post ids failed: %v", err)
		}
		want := []int64{3004, 3003, 3002}
		if len(postIDs) != len(want) {
			t.Fatalf("unexpected inbox size after trim: got=%d want=%d", len(postIDs), len(want))
		}
		for i, wantID := range want {
			if postIDs[i] != wantID {
				t.Fatalf("unexpected post id at %d: got=%d want=%d", i, postIDs[i], wantID)
			}
		}
	})
}
