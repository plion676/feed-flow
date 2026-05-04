package service

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type fakePostInteractionPostRepo struct {
	post *model.Post
	err  error
}

func (r *fakePostInteractionPostRepo) GetByID(_ context.Context, _ int64) (*model.Post, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.post, nil
}

type fakePostInteractionRepo struct {
	likes    map[int64]map[int64]struct{}
	collects map[int64]map[int64]struct{}
	comments []*model.PostComment

	nextCommentID int64
	err           error
}

func (r *fakePostInteractionRepo) Like(_ context.Context, userID int64, postID int64) error {
	if r.err != nil {
		return r.err
	}
	if r.likes == nil {
		r.likes = make(map[int64]map[int64]struct{})
	}
	if r.likes[postID] == nil {
		r.likes[postID] = make(map[int64]struct{})
	}
	r.likes[postID][userID] = struct{}{}
	return nil
}

func (r *fakePostInteractionRepo) Unlike(_ context.Context, userID int64, postID int64) error {
	if r.err != nil {
		return r.err
	}
	delete(r.likes[postID], userID)
	return nil
}

func (r *fakePostInteractionRepo) Collect(_ context.Context, userID int64, postID int64) error {
	if r.err != nil {
		return r.err
	}
	if r.collects == nil {
		r.collects = make(map[int64]map[int64]struct{})
	}
	if r.collects[postID] == nil {
		r.collects[postID] = make(map[int64]struct{})
	}
	r.collects[postID][userID] = struct{}{}
	return nil
}

func (r *fakePostInteractionRepo) Uncollect(_ context.Context, userID int64, postID int64) error {
	if r.err != nil {
		return r.err
	}
	delete(r.collects[postID], userID)
	return nil
}

func (r *fakePostInteractionRepo) CreateComment(_ context.Context, comment *model.PostComment) error {
	if r.err != nil {
		return r.err
	}
	if r.nextCommentID <= 0 {
		r.nextCommentID = 1
	}
	comment.ID = r.nextCommentID
	comment.CreatedAt = time.Date(2026, 5, 4, 12, int(r.nextCommentID), 0, 0, time.UTC)
	r.nextCommentID++
	copied := *comment
	r.comments = append(r.comments, &copied)
	return nil
}

func (r *fakePostInteractionRepo) ListComments(_ context.Context, postID int64, lastCommentID int64, limit int) ([]*model.PostComment, error) {
	if r.err != nil {
		return nil, r.err
	}
	filtered := make([]*model.PostComment, 0, len(r.comments))
	for _, comment := range r.comments {
		if comment.PostID != postID || comment.Status != model.CommentStatusPublished {
			continue
		}
		if lastCommentID > 0 && comment.ID >= lastCommentID {
			continue
		}
		filtered = append(filtered, comment)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID > filtered[j].ID
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func (r *fakePostInteractionRepo) CountLikesByPostIDs(_ context.Context, postIDs []int64) (map[int64]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[int64]int64, len(postIDs))
	for _, postID := range postIDs {
		out[postID] = int64(len(r.likes[postID]))
	}
	return out, nil
}

func (r *fakePostInteractionRepo) CountCollectsByPostIDs(_ context.Context, postIDs []int64) (map[int64]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[int64]int64, len(postIDs))
	for _, postID := range postIDs {
		out[postID] = int64(len(r.collects[postID]))
	}
	return out, nil
}

func (r *fakePostInteractionRepo) CountCommentsByPostIDs(_ context.Context, postIDs []int64) (map[int64]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	allow := make(map[int64]struct{}, len(postIDs))
	for _, postID := range postIDs {
		allow[postID] = struct{}{}
	}
	out := make(map[int64]int64, len(postIDs))
	for _, comment := range r.comments {
		if _, ok := allow[comment.PostID]; ok && comment.Status == model.CommentStatusPublished {
			out[comment.PostID]++
		}
	}
	return out, nil
}

func (r *fakePostInteractionRepo) ListLikedPostIDs(_ context.Context, userID int64, postIDs []int64) ([]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	return filterUserInteractionPostIDs(r.likes, userID, postIDs), nil
}

func (r *fakePostInteractionRepo) ListCollectedPostIDs(_ context.Context, userID int64, postIDs []int64) ([]int64, error) {
	if r.err != nil {
		return nil, r.err
	}
	return filterUserInteractionPostIDs(r.collects, userID, postIDs), nil
}

func filterUserInteractionPostIDs(source map[int64]map[int64]struct{}, userID int64, postIDs []int64) []int64 {
	out := make([]int64, 0, len(postIDs))
	for _, postID := range postIDs {
		if _, ok := source[postID][userID]; ok {
			out = append(out, postID)
		}
	}
	return out
}

func TestPostInteractionServiceLikeAndCollect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	postRepo := &fakePostInteractionPostRepo{post: &model.Post{ID: 10, UserID: 1002, Status: model.PostStatusPublished}}
	interactionRepo := &fakePostInteractionRepo{}
	svc := NewPostInteractionService(postRepo, interactionRepo)

	liked, gotErr := svc.Like(ctx, 1001, 10)
	if gotErr != nil {
		t.Fatalf("unexpected like error: %v", gotErr)
	}
	if !liked.Liked || liked.LikeCount != 1 {
		t.Fatalf("unexpected liked result: %+v", liked)
	}

	likedAgain, gotErr := svc.Like(ctx, 1001, 10)
	if gotErr != nil {
		t.Fatalf("unexpected duplicate like error: %v", gotErr)
	}
	if likedAgain.LikeCount != 1 {
		t.Fatalf("duplicate like should be idempotent: %+v", likedAgain)
	}

	collected, gotErr := svc.Collect(ctx, 1001, 10)
	if gotErr != nil {
		t.Fatalf("unexpected collect error: %v", gotErr)
	}
	if !collected.Collected || collected.CollectCount != 1 {
		t.Fatalf("unexpected collect result: %+v", collected)
	}

	statuses, gotErr := svc.GetStatuses(ctx, 1001, []int64{10, 10, 0})
	if gotErr != nil {
		t.Fatalf("unexpected status error: %v", gotErr)
	}
	if len(statuses.Items) != 1 {
		t.Fatalf("expected one unique status item, got=%+v", statuses.Items)
	}
	if !statuses.Items[0].Liked || !statuses.Items[0].Collected {
		t.Fatalf("expected current user interaction state, got=%+v", statuses.Items[0])
	}

	unliked, gotErr := svc.Unlike(ctx, 1001, 10)
	if gotErr != nil {
		t.Fatalf("unexpected unlike error: %v", gotErr)
	}
	if unliked.Liked || unliked.LikeCount != 0 {
		t.Fatalf("unexpected unlike result: %+v", unliked)
	}
}

func TestPostInteractionServiceComments(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	postRepo := &fakePostInteractionPostRepo{post: &model.Post{ID: 10, UserID: 1002, Status: model.PostStatusPublished}}
	interactionRepo := &fakePostInteractionRepo{}
	svc := NewPostInteractionService(postRepo, interactionRepo)

	created, gotErr := svc.CreateComment(ctx, CreateCommentRequest{
		UserID:  1001,
		PostID:  10,
		Content: "  第一条评论  ",
	})
	if gotErr != nil {
		t.Fatalf("unexpected create comment error: %v", gotErr)
	}
	if created.Content != "第一条评论" || created.CommentID <= 0 {
		t.Fatalf("unexpected created comment: %+v", created)
	}

	_, _ = svc.CreateComment(ctx, CreateCommentRequest{UserID: 1002, PostID: 10, Content: "第二条评论"})
	_, _ = svc.CreateComment(ctx, CreateCommentRequest{UserID: 1003, PostID: 10, Content: "第三条评论"})

	firstPage, gotErr := svc.ListComments(ctx, 10, 0, 2)
	if gotErr != nil {
		t.Fatalf("unexpected list comments error: %v", gotErr)
	}
	if len(firstPage.Items) != 2 || !firstPage.HasMore || firstPage.NextCursor != 2 {
		t.Fatalf("unexpected first page: %+v", firstPage)
	}
	gotIDs := []int64{firstPage.Items[0].CommentID, firstPage.Items[1].CommentID}
	if !reflect.DeepEqual(gotIDs, []int64{3, 2}) {
		t.Fatalf("unexpected comment order: got=%v", gotIDs)
	}

	secondPage, gotErr := svc.ListComments(ctx, 10, firstPage.NextCursor, 2)
	if gotErr != nil {
		t.Fatalf("unexpected second page error: %v", gotErr)
	}
	if len(secondPage.Items) != 1 || secondPage.HasMore {
		t.Fatalf("unexpected second page: %+v", secondPage)
	}
}

func TestPostInteractionServiceValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("post not found", func(t *testing.T) {
		t.Parallel()
		svc := NewPostInteractionService(&fakePostInteractionPostRepo{}, &fakePostInteractionRepo{})
		got, gotErr := svc.Like(ctx, 1001, 10)
		if gotErr != xerror.ErrPostNotFound || got != nil {
			t.Fatalf("unexpected result: got=%+v err=%v", got, gotErr)
		}
	})

	t.Run("post repo internal error", func(t *testing.T) {
		t.Parallel()
		svc := NewPostInteractionService(&fakePostInteractionPostRepo{err: errors.New("db down")}, &fakePostInteractionRepo{})
		got, gotErr := svc.GetStatus(ctx, 1001, 10)
		if gotErr != xerror.ErrInternal || got != nil {
			t.Fatalf("unexpected result: got=%+v err=%v", got, gotErr)
		}
	})

	t.Run("comment too long", func(t *testing.T) {
		t.Parallel()
		svc := NewPostInteractionService(
			&fakePostInteractionPostRepo{post: &model.Post{ID: 10, Status: model.PostStatusPublished}},
			&fakePostInteractionRepo{},
		)
		got, gotErr := svc.CreateComment(ctx, CreateCommentRequest{
			UserID:  1001,
			PostID:  10,
			Content: strings.Repeat("你", maxCommentLen+1),
		})
		if gotErr != xerror.ErrBadRequest || got != nil {
			t.Fatalf("unexpected result: got=%+v err=%v", got, gotErr)
		}
	})
}
