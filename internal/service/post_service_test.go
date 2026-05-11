package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/repository"
	"gorm.io/gorm"
)

type fakePostRepo struct {
	createErr error
	getErr    error
	deleteErr error
	getPost   *model.Post
	deleted   bool

	createdPost *model.Post
}

type fakePostUserCountRepo struct {
	addCalls []fakePostCountCall
	addErr   error
}

type fakePostEventOutboxRepo struct {
	createCalled int
	lastEvent    *model.FeedEventOutbox
	createErr    error
}

type fakePostCountCall struct {
	userID int64
	delta  int64
}

type fakeTransactionRunner struct {
	called int
	err    error
}

func (r *fakeTransactionRunner) InTx(_ context.Context, fn func(tx *gorm.DB) error) error {
	r.called++
	if r.err != nil {
		return r.err
	}
	return fn(&gorm.DB{})
}

type fakePostFeedInvalidator struct {
	gotUserID int64
	called    int
	err       error
}

func (f *fakePostFeedInvalidator) InvalidateHomeFeed(_ context.Context, userID int64) error {
	f.called++
	f.gotUserID = userID
	return f.err
}

type fakePostOutboxRepo struct {
	addCalled    int
	trimCalled   int
	removeCalled int
	gotAuthorID  int64
	gotPostID    int64
	gotMaxItems  int64
	addErr       error
	trimErr      error
	removeErr    error
}

func (f *fakePostOutboxRepo) AddPostToOutbox(_ context.Context, authorUserID int64, postID int64) error {
	f.addCalled++
	f.gotAuthorID = authorUserID
	f.gotPostID = postID
	return f.addErr
}

func (f *fakePostOutboxRepo) RemovePostFromOutbox(_ context.Context, authorUserID int64, postID int64) error {
	f.removeCalled++
	f.gotAuthorID = authorUserID
	f.gotPostID = postID
	return f.removeErr
}

func (f *fakePostOutboxRepo) TrimOutbox(_ context.Context, authorUserID int64, maxItems int64) error {
	f.trimCalled++
	f.gotAuthorID = authorUserID
	f.gotMaxItems = maxItems
	return f.trimErr
}

func (r *fakePostUserCountRepo) AddPostCountTx(_ context.Context, _ *gorm.DB, userID int64, delta int64) error {
	r.addCalls = append(r.addCalls, fakePostCountCall{userID: userID, delta: delta})
	return r.addErr
}

func (r *fakePostEventOutboxRepo) CreateTx(_ context.Context, _ *gorm.DB, event *model.FeedEventOutbox) error {
	r.createCalled++
	if r.createErr != nil {
		return r.createErr
	}
	copied := *event
	r.lastEvent = &copied
	return nil
}

func (r *fakePostRepo) Create(_ context.Context, post *model.Post) error {
	if r.createErr != nil {
		return r.createErr
	}

	r.createdPost = &model.Post{
		UserID:  post.UserID,
		Content: post.Content,
		Status:  post.Status,
	}

	post.ID = 2001
	post.CreatedAt = time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	return nil
}

func (r *fakePostRepo) CreateTx(ctx context.Context, _ *gorm.DB, post *model.Post) error {
	return r.Create(ctx, post)
}

func (r *fakePostRepo) GetByID(_ context.Context, _ int64) (*model.Post, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.getPost, nil
}

func (r *fakePostRepo) SoftDeleteByIDAndUserID(_ context.Context, postID int64, userID int64) (bool, error) {
	if r.deleteErr != nil {
		return false, r.deleteErr
	}
	if r.getPost == nil || r.getPost.ID != postID || r.getPost.UserID != userID || r.getPost.Status != model.PostStatusPublished {
		return false, nil
	}
	r.deleted = true
	r.getPost.Status = model.PostStatusDeleted
	return true, nil
}

func (r *fakePostRepo) SoftDeleteByIDAndUserIDTx(ctx context.Context, _ *gorm.DB, postID int64, userID int64) (bool, error) {
	return r.SoftDeleteByIDAndUserID(ctx, postID, userID)
}

func TestPostServiceCreate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name              string
		req               CreatePostRequest
		repo              *fakePostRepo
		invalidator       *fakePostFeedInvalidator
		outboxRepo        *fakePostOutboxRepo
		outboxMaxItems    int64
		wantErr           *xerror.Error
		wantSavedContent  string
		wantResultContent string
		wantInvalidate    bool
		wantOutboxAdd     bool
		wantOutboxTrim    bool
	}{
		{
			name:        "bad request when user id invalid",
			req:         CreatePostRequest{UserID: 0, Content: "hello"},
			repo:        &fakePostRepo{},
			invalidator: &fakePostFeedInvalidator{},
			outboxRepo:  &fakePostOutboxRepo{},
			wantErr:     xerror.ErrBadRequest,
		},
		{
			name:        "bad request when content empty after trim",
			req:         CreatePostRequest{UserID: 1001, Content: "   "},
			repo:        &fakePostRepo{},
			invalidator: &fakePostFeedInvalidator{},
			outboxRepo:  &fakePostOutboxRepo{},
			wantErr:     xerror.ErrBadRequest,
		},
		{
			name:        "bad request when content rune count too long",
			req:         CreatePostRequest{UserID: 1001, Content: strings.Repeat("你", 501)},
			repo:        &fakePostRepo{},
			invalidator: &fakePostFeedInvalidator{},
			outboxRepo:  &fakePostOutboxRepo{},
			wantErr:     xerror.ErrBadRequest,
		},
		{
			name:        "internal error when repository create fails",
			req:         CreatePostRequest{UserID: 1001, Content: "hello"},
			repo:        &fakePostRepo{createErr: errors.New("insert failed")},
			invalidator: &fakePostFeedInvalidator{},
			outboxRepo:  &fakePostOutboxRepo{},
			wantErr:     xerror.ErrInternal,
		},
		{
			name:              "success and save trimmed content",
			req:               CreatePostRequest{UserID: 1001, Content: "  hello post  "},
			repo:              &fakePostRepo{},
			invalidator:       &fakePostFeedInvalidator{},
			outboxRepo:        &fakePostOutboxRepo{},
			outboxMaxItems:    1000,
			wantErr:           nil,
			wantSavedContent:  "hello post",
			wantResultContent: "hello post",
			wantInvalidate:    true,
			wantOutboxAdd:     true,
			wantOutboxTrim:    true,
		},
		{
			name:              "success even when outbox write fails",
			req:               CreatePostRequest{UserID: 1001, Content: "  outbox down  "},
			repo:              &fakePostRepo{},
			invalidator:       &fakePostFeedInvalidator{},
			outboxRepo:        &fakePostOutboxRepo{addErr: errors.New("redis down")},
			outboxMaxItems:    1000,
			wantErr:           nil,
			wantSavedContent:  "outbox down",
			wantResultContent: "outbox down",
			wantInvalidate:    true,
			wantOutboxAdd:     true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewPostService(tc.repo).
				WithFeedCacheInvalidator(tc.invalidator).
				WithFeedOutbox(tc.outboxRepo, tc.outboxMaxItems)
			got, gotErr := svc.Create(ctx, tc.req)

			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}

			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("expected nil result when error happens, got=%+v", got)
				}
				if tc.invalidator.called != 0 {
					t.Fatalf("invalidator should not be called on failed create, called=%d", tc.invalidator.called)
				}
				if tc.outboxRepo.addCalled != 0 {
					t.Fatalf("outbox should not be called on failed create, called=%d", tc.outboxRepo.addCalled)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil result on success")
			}
			if tc.repo.createdPost == nil {
				t.Fatal("expected repository create to be called")
			}
			if tc.repo.createdPost.Content != tc.wantSavedContent {
				t.Fatalf("unexpected saved content: got=%q want=%q", tc.repo.createdPost.Content, tc.wantSavedContent)
			}
			if got.Content != tc.wantResultContent {
				t.Fatalf("unexpected result content: got=%q want=%q", got.Content, tc.wantResultContent)
			}
			if got.UserID != tc.req.UserID {
				t.Fatalf("unexpected user id: got=%d want=%d", got.UserID, tc.req.UserID)
			}
			if got.PostID <= 0 {
				t.Fatalf("unexpected post id: got=%d", got.PostID)
			}
			if got.CreatedAt.IsZero() {
				t.Fatal("created_at should not be zero")
			}
			if tc.wantInvalidate && tc.invalidator.called != 1 {
				t.Fatalf("expected invalidator called once, got=%d", tc.invalidator.called)
			}
			if tc.wantInvalidate && tc.invalidator.gotUserID != tc.req.UserID {
				t.Fatalf("unexpected invalidator user id: got=%d want=%d", tc.invalidator.gotUserID, tc.req.UserID)
			}
			if tc.wantOutboxAdd && tc.outboxRepo.addCalled != 1 {
				t.Fatalf("expected outbox add called once, got=%d", tc.outboxRepo.addCalled)
			}
			if tc.wantOutboxAdd && tc.outboxRepo.gotAuthorID != tc.req.UserID {
				t.Fatalf("unexpected outbox author id: got=%d want=%d", tc.outboxRepo.gotAuthorID, tc.req.UserID)
			}
			if tc.wantOutboxAdd && tc.outboxRepo.gotPostID <= 0 {
				t.Fatalf("unexpected outbox post id: got=%d", tc.outboxRepo.gotPostID)
			}
			if tc.wantOutboxTrim && tc.outboxRepo.trimCalled != 1 {
				t.Fatalf("expected outbox trim called once, got=%d", tc.outboxRepo.trimCalled)
			}
			if tc.wantOutboxTrim && tc.outboxRepo.gotMaxItems != tc.outboxMaxItems {
				t.Fatalf("unexpected outbox max items: got=%d want=%d", tc.outboxRepo.gotMaxItems, tc.outboxMaxItems)
			}
		})
	}
}

func TestPostServiceGetByID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 20, 12, 30, 0, 0, time.UTC)

	tests := []struct {
		name      string
		postID    int64
		repo      *fakePostRepo
		wantErr   *xerror.Error
		wantFound bool
	}{
		{
			name:    "bad request when post id invalid",
			postID:  0,
			repo:    &fakePostRepo{},
			wantErr: xerror.ErrBadRequest,
		},
		{
			name:    "internal error when repository fails",
			postID:  1,
			repo:    &fakePostRepo{getErr: errors.New("query failed")},
			wantErr: xerror.ErrInternal,
		},
		{
			name:    "not found when post missing",
			postID:  1,
			repo:    &fakePostRepo{getPost: nil},
			wantErr: xerror.ErrPostNotFound,
		},
		{
			name:   "not found when post is not active",
			postID: 1,
			repo: &fakePostRepo{getPost: &model.Post{
				ID:        1,
				UserID:    1001,
				Content:   "hidden",
				Status:    0,
				CreatedAt: now,
			}},
			wantErr: xerror.ErrPostNotFound,
		},
		{
			name:   "success",
			postID: 1,
			repo: &fakePostRepo{getPost: &model.Post{
				ID:        1,
				UserID:    1001,
				Content:   "hello",
				Status:    1,
				CreatedAt: now,
			}},
			wantErr:   nil,
			wantFound: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewPostService(tc.repo)
			got, gotErr := svc.GetByID(ctx, tc.postID)

			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}

			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("expected nil result when error happens, got=%+v", got)
				}
				return
			}

			if !tc.wantFound {
				return
			}
			if got == nil {
				t.Fatal("expected non-nil result on success")
			}
			if got.PostID != tc.repo.getPost.ID || got.UserID != tc.repo.getPost.UserID {
				t.Fatalf("unexpected result identity: got=%+v source=%+v", got, tc.repo.getPost)
			}
			if got.Content != tc.repo.getPost.Content {
				t.Fatalf("unexpected content: got=%q want=%q", got.Content, tc.repo.getPost.Content)
			}
		})
	}
}

func TestPostServiceDelete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		req            DeletePostRequest
		repo           *fakePostRepo
		invalidator    *fakePostFeedInvalidator
		wantErr        *xerror.Error
		wantDeleted    bool
		wantInvalidate bool
	}{
		{
			name:        "bad request when user id invalid",
			req:         DeletePostRequest{UserID: 0, PostID: 1},
			repo:        &fakePostRepo{},
			invalidator: &fakePostFeedInvalidator{},
			wantErr:     xerror.ErrBadRequest,
		},
		{
			name:        "bad request when post id invalid",
			req:         DeletePostRequest{UserID: 1001, PostID: 0},
			repo:        &fakePostRepo{},
			invalidator: &fakePostFeedInvalidator{},
			wantErr:     xerror.ErrBadRequest,
		},
		{
			name:        "internal error when get fails",
			req:         DeletePostRequest{UserID: 1001, PostID: 1},
			repo:        &fakePostRepo{getErr: errors.New("query failed")},
			invalidator: &fakePostFeedInvalidator{},
			wantErr:     xerror.ErrInternal,
		},
		{
			name:        "not found when post missing",
			req:         DeletePostRequest{UserID: 1001, PostID: 1},
			repo:        &fakePostRepo{},
			invalidator: &fakePostFeedInvalidator{},
			wantErr:     xerror.ErrPostNotFound,
		},
		{
			name: "not found when post already deleted",
			req:  DeletePostRequest{UserID: 1001, PostID: 1},
			repo: &fakePostRepo{getPost: &model.Post{
				ID:        1,
				UserID:    1001,
				Content:   "deleted",
				Status:    model.PostStatusDeleted,
				CreatedAt: now,
			}},
			invalidator: &fakePostFeedInvalidator{},
			wantErr:     xerror.ErrPostNotFound,
		},
		{
			name: "forbidden when current user is not author",
			req:  DeletePostRequest{UserID: 2002, PostID: 1},
			repo: &fakePostRepo{getPost: &model.Post{
				ID:        1,
				UserID:    1001,
				Content:   "other user post",
				Status:    model.PostStatusPublished,
				CreatedAt: now,
			}},
			invalidator: &fakePostFeedInvalidator{},
			wantErr:     xerror.ErrForbidden,
		},
		{
			name: "internal error when delete fails",
			req:  DeletePostRequest{UserID: 1001, PostID: 1},
			repo: &fakePostRepo{
				getPost: &model.Post{
					ID:        1,
					UserID:    1001,
					Content:   "delete failed",
					Status:    model.PostStatusPublished,
					CreatedAt: now,
				},
				deleteErr: errors.New("update failed"),
			},
			invalidator: &fakePostFeedInvalidator{},
			wantErr:     xerror.ErrInternal,
		},
		{
			name: "success soft deletes author post",
			req:  DeletePostRequest{UserID: 1001, PostID: 1},
			repo: &fakePostRepo{getPost: &model.Post{
				ID:        1,
				UserID:    1001,
				Content:   "hello",
				Status:    model.PostStatusPublished,
				CreatedAt: now,
			}},
			invalidator:    &fakePostFeedInvalidator{},
			wantDeleted:    true,
			wantInvalidate: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewPostService(tc.repo).
				WithFeedCacheInvalidator(tc.invalidator)
			got, gotErr := svc.Delete(ctx, tc.req)

			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("expected nil result when error happens, got=%+v", got)
				}
				if tc.repo.deleted {
					t.Fatal("post should not be deleted when request fails")
				}
				if tc.invalidator.called != 0 {
					t.Fatalf("invalidator should not be called on failed delete, called=%d", tc.invalidator.called)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil result on success")
			}
			if got.PostID != tc.req.PostID || got.UserID != tc.req.UserID || got.Deleted != tc.wantDeleted {
				t.Fatalf("unexpected delete result: got=%+v req=%+v", got, tc.req)
			}
			if !tc.repo.deleted {
				t.Fatal("expected repository soft delete to be called")
			}
			if tc.wantInvalidate && tc.invalidator.called != 1 {
				t.Fatalf("expected invalidator called once, got=%d", tc.invalidator.called)
			}
			if tc.wantInvalidate && tc.invalidator.gotUserID != tc.req.UserID {
				t.Fatalf("unexpected invalidator user id: got=%d want=%d", tc.invalidator.gotUserID, tc.req.UserID)
			}
		})
	}
}

func TestPostServiceCreateMaintainsUserCountInTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &fakePostRepo{}
	userCountRepo := &fakePostUserCountRepo{}
	eventOutboxRepo := &fakePostEventOutboxRepo{}
	txRunner := &fakeTransactionRunner{}
	invalidator := &fakePostFeedInvalidator{}

	svc := NewPostServiceWithTransaction(txRunner, repo, userCountRepo).
		WithFeedCacheInvalidator(invalidator).
		WithEventOutbox(eventOutboxRepo)

	got, gotErr := svc.Create(ctx, CreatePostRequest{
		UserID:  1001,
		Content: "  tx post  ",
	})

	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if got == nil {
		t.Fatal("expected result")
	}
	if txRunner.called != 1 {
		t.Fatalf("expected transaction runner called once, got=%d", txRunner.called)
	}
	if len(userCountRepo.addCalls) != 1 {
		t.Fatalf("expected one count update, got=%d", len(userCountRepo.addCalls))
	}
	if userCountRepo.addCalls[0].userID != 1001 || userCountRepo.addCalls[0].delta != 1 {
		t.Fatalf("unexpected count update: %+v", userCountRepo.addCalls[0])
	}
	if invalidator.called != 1 {
		t.Fatalf("expected invalidator called once, got=%d", invalidator.called)
	}
	if eventOutboxRepo.createCalled != 1 {
		t.Fatalf("expected outbox event created once, got=%d", eventOutboxRepo.createCalled)
	}
	if eventOutboxRepo.lastEvent == nil {
		t.Fatal("expected outbox event payload")
	}
	if eventOutboxRepo.lastEvent.EventType != model.FeedEventOutboxTypePostCreated {
		t.Fatalf("unexpected event type: %s", eventOutboxRepo.lastEvent.EventType)
	}
	var payload repository.FeedInvalidationEvent
	if err := json.Unmarshal([]byte(eventOutboxRepo.lastEvent.Payload), &payload); err != nil {
		t.Fatalf("unmarshal payload failed: %v", err)
	}
	if payload.Type != repository.FeedInvalidationEventTypePostCreated || payload.AuthorID != 1001 || payload.PostID != 2001 {
		t.Fatalf("unexpected event payload: %+v", payload)
	}
}

func TestPostServiceCreateDoesNotPublishWhenCountUpdateFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &fakePostRepo{}
	userCountRepo := &fakePostUserCountRepo{addErr: errors.New("count update failed")}
	eventOutboxRepo := &fakePostEventOutboxRepo{}
	txRunner := &fakeTransactionRunner{}
	invalidator := &fakePostFeedInvalidator{}

	svc := NewPostServiceWithTransaction(txRunner, repo, userCountRepo).
		WithFeedCacheInvalidator(invalidator).
		WithEventOutbox(eventOutboxRepo)

	got, gotErr := svc.Create(ctx, CreatePostRequest{
		UserID:  1001,
		Content: "hello",
	})

	if gotErr != xerror.ErrInternal {
		t.Fatalf("unexpected error: got=%v want=%v", gotErr, xerror.ErrInternal)
	}
	if got != nil {
		t.Fatalf("expected nil result, got=%+v", got)
	}
	if txRunner.called != 1 {
		t.Fatalf("expected transaction runner called once, got=%d", txRunner.called)
	}
	if invalidator.called != 0 {
		t.Fatalf("invalidator should not run, called=%d", invalidator.called)
	}
	if eventOutboxRepo.createCalled != 0 {
		t.Fatalf("outbox event should not be created after count failure, called=%d", eventOutboxRepo.createCalled)
	}
}

func TestPostServiceDeleteMaintainsUserCountInTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	now := time.Date(2026, 4, 20, 14, 0, 0, 0, time.UTC)
	repo := &fakePostRepo{getPost: &model.Post{
		ID:        1,
		UserID:    1001,
		Content:   "hello",
		Status:    model.PostStatusPublished,
		CreatedAt: now,
	}}
	userCountRepo := &fakePostUserCountRepo{}
	eventOutboxRepo := &fakePostEventOutboxRepo{}
	txRunner := &fakeTransactionRunner{}
	invalidator := &fakePostFeedInvalidator{}

	svc := NewPostServiceWithTransaction(txRunner, repo, userCountRepo).
		WithFeedCacheInvalidator(invalidator).
		WithEventOutbox(eventOutboxRepo)

	got, gotErr := svc.Delete(ctx, DeletePostRequest{
		UserID: 1001,
		PostID: 1,
	})

	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if got == nil || !got.Deleted {
		t.Fatalf("expected deleted result, got=%+v", got)
	}
	if txRunner.called != 1 {
		t.Fatalf("expected transaction runner called once, got=%d", txRunner.called)
	}
	if len(userCountRepo.addCalls) != 1 {
		t.Fatalf("expected one count update, got=%d", len(userCountRepo.addCalls))
	}
	if userCountRepo.addCalls[0].userID != 1001 || userCountRepo.addCalls[0].delta != -1 {
		t.Fatalf("unexpected count update: %+v", userCountRepo.addCalls[0])
	}
	if invalidator.called != 1 {
		t.Fatalf("expected invalidator called once, got=%d", invalidator.called)
	}
	if eventOutboxRepo.createCalled != 1 {
		t.Fatalf("expected delete outbox event created once, got=%d", eventOutboxRepo.createCalled)
	}
	if eventOutboxRepo.lastEvent == nil || eventOutboxRepo.lastEvent.EventType != model.FeedEventOutboxTypePostDeleted {
		t.Fatalf("unexpected delete event row: %+v", eventOutboxRepo.lastEvent)
	}
	var payload repository.FeedInvalidationEvent
	if err := json.Unmarshal([]byte(eventOutboxRepo.lastEvent.Payload), &payload); err != nil {
		t.Fatalf("unmarshal delete payload failed: %v", err)
	}
	if payload.Type != repository.FeedInvalidationEventTypePostDeleted || payload.AuthorID != 1001 || payload.PostID != 1 {
		t.Fatalf("unexpected delete event payload: %+v", payload)
	}
}
