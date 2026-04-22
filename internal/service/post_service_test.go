package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type fakePostRepo struct {
	createErr error
	getErr    error
	getPost   *model.Post

	createdPost *model.Post
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

type fakePostInvalidationEventPublisher struct {
	gotAuthorUserID int64
	called          int
	err             error
}

func (f *fakePostInvalidationEventPublisher) PublishPostCreated(_ context.Context, authorUserID int64) error {
	f.called++
	f.gotAuthorUserID = authorUserID
	return f.err
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

func (r *fakePostRepo) GetByID(_ context.Context, _ int64) (*model.Post, error) {
	if r.getErr != nil {
		return nil, r.getErr
	}
	return r.getPost, nil
}

func TestPostServiceCreate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name              string
		req               CreatePostRequest
		repo              *fakePostRepo
		invalidator       *fakePostFeedInvalidator
		eventPublisher    *fakePostInvalidationEventPublisher
		wantErr           *xerror.Error
		wantSavedContent  string
		wantResultContent string
		wantInvalidate    bool
		wantPublishEvent  bool
	}{
		{
			name:           "bad request when user id invalid",
			req:            CreatePostRequest{UserID: 0, Content: "hello"},
			repo:           &fakePostRepo{},
			invalidator:    &fakePostFeedInvalidator{},
			eventPublisher: &fakePostInvalidationEventPublisher{},
			wantErr:        xerror.ErrBadRequest,
		},
		{
			name:           "bad request when content empty after trim",
			req:            CreatePostRequest{UserID: 1001, Content: "   "},
			repo:           &fakePostRepo{},
			invalidator:    &fakePostFeedInvalidator{},
			eventPublisher: &fakePostInvalidationEventPublisher{},
			wantErr:        xerror.ErrBadRequest,
		},
		{
			name:           "bad request when content rune count too long",
			req:            CreatePostRequest{UserID: 1001, Content: strings.Repeat("你", 501)},
			repo:           &fakePostRepo{},
			invalidator:    &fakePostFeedInvalidator{},
			eventPublisher: &fakePostInvalidationEventPublisher{},
			wantErr:        xerror.ErrBadRequest,
		},
		{
			name:           "internal error when repository create fails",
			req:            CreatePostRequest{UserID: 1001, Content: "hello"},
			repo:           &fakePostRepo{createErr: errors.New("insert failed")},
			invalidator:    &fakePostFeedInvalidator{},
			eventPublisher: &fakePostInvalidationEventPublisher{},
			wantErr:        xerror.ErrInternal,
		},
		{
			name:              "success and save trimmed content",
			req:               CreatePostRequest{UserID: 1001, Content: "  hello post  "},
			repo:              &fakePostRepo{},
			invalidator:       &fakePostFeedInvalidator{},
			eventPublisher:    &fakePostInvalidationEventPublisher{},
			wantErr:           nil,
			wantSavedContent:  "hello post",
			wantResultContent: "hello post",
			wantInvalidate:    true,
			wantPublishEvent:  true,
		},
		{
			name:              "success even when event publish fails",
			req:               CreatePostRequest{UserID: 1001, Content: "  event down  "},
			repo:              &fakePostRepo{},
			invalidator:       &fakePostFeedInvalidator{},
			eventPublisher:    &fakePostInvalidationEventPublisher{err: errors.New("queue timeout")},
			wantErr:           nil,
			wantSavedContent:  "event down",
			wantResultContent: "event down",
			wantInvalidate:    true,
			wantPublishEvent:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewPostService(tc.repo).
				WithFeedCacheInvalidator(tc.invalidator).
				WithFeedInvalidationEventPublisher(tc.eventPublisher)
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
				if tc.eventPublisher.called != 0 {
					t.Fatalf("event publisher should not be called on failed create, called=%d", tc.eventPublisher.called)
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
			if tc.wantPublishEvent && tc.eventPublisher.called != 1 {
				t.Fatalf("expected event publisher called once, got=%d", tc.eventPublisher.called)
			}
			if tc.wantPublishEvent && tc.eventPublisher.gotAuthorUserID != tc.req.UserID {
				t.Fatalf("unexpected event publisher user id: got=%d want=%d", tc.eventPublisher.gotAuthorUserID, tc.req.UserID)
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
