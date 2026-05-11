package service

import (
	"context"
	"errors"
	"testing"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"gorm.io/gorm"
)

type fakeFollowRepo struct {
	createErr error
	deleteErr error
	deleted   bool

	createCalled int
	deleteCalled int

	lastCreate *model.Follow
	lastDelete struct {
		userID       int64
		targetUserID int64
	}
}

func (r *fakeFollowRepo) Create(_ context.Context, follow *model.Follow) error {
	r.createCalled++
	r.lastCreate = &model.Follow{
		UserID:       follow.UserID,
		TargetUserID: follow.TargetUserID,
	}
	return r.createErr
}

func (r *fakeFollowRepo) CreateTx(ctx context.Context, _ *gorm.DB, follow *model.Follow) error {
	return r.Create(ctx, follow)
}

func (r *fakeFollowRepo) Delete(_ context.Context, userID int64, targetUserID int64) (bool, error) {
	r.deleteCalled++
	r.lastDelete.userID = userID
	r.lastDelete.targetUserID = targetUserID
	if r.deleteErr != nil {
		return false, r.deleteErr
	}
	return r.deleted, nil
}

func (r *fakeFollowRepo) DeleteTx(ctx context.Context, _ *gorm.DB, userID int64, targetUserID int64) (bool, error) {
	return r.Delete(ctx, userID, targetUserID)
}

type fakeFollowUserRepo struct {
	user *model.User
	err  error
}

func (r *fakeFollowUserRepo) GetByID(_ context.Context, _ int64) (*model.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.user, nil
}

type fakeFollowFeedInvalidator struct {
	called    int
	gotUserID int64
	err       error
}

type fakeFollowInboxCleanup struct {
	called          int
	gotUserID       int64
	gotAuthorUserID int64
	err             error
}

type fakeFollowUserCountRepo struct {
	followingCalls []fakeFollowCountCall
	followerCalls  []fakeFollowCountCall
	followingErr   error
	followerErr    error
}

type fakeFollowCountCall struct {
	userID int64
	delta  int64
}

func (f *fakeFollowFeedInvalidator) InvalidateHomeFeed(_ context.Context, userID int64) error {
	f.called++
	f.gotUserID = userID
	return f.err
}

func (f *fakeFollowInboxCleanup) RemoveAuthorPostsFromInbox(_ context.Context, userID int64, authorUserID int64) error {
	f.called++
	f.gotUserID = userID
	f.gotAuthorUserID = authorUserID
	return f.err
}

func (r *fakeFollowUserCountRepo) AddFollowingCountTx(_ context.Context, _ *gorm.DB, userID int64, delta int64) error {
	r.followingCalls = append(r.followingCalls, fakeFollowCountCall{userID: userID, delta: delta})
	return r.followingErr
}

func (r *fakeFollowUserCountRepo) AddFollowerCountTx(_ context.Context, _ *gorm.DB, userID int64, delta int64) error {
	r.followerCalls = append(r.followerCalls, fakeFollowCountCall{userID: userID, delta: delta})
	return r.followerErr
}

func TestFollowServiceFollow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name             string
		userID           int64
		targetUserID     int64
		followRepo       *fakeFollowRepo
		userRepo         *fakeFollowUserRepo
		invalidator      *fakeFollowFeedInvalidator
		wantErr          *xerror.Error
		wantCreateCalled bool
		wantInvalidate   bool
	}{
		{
			name:         "bad request on self follow",
			userID:       1001,
			targetUserID: 1001,
			followRepo:   &fakeFollowRepo{},
			userRepo:     &fakeFollowUserRepo{user: &model.User{ID: 1001}},
			invalidator:  &fakeFollowFeedInvalidator{},
			wantErr:      xerror.ErrBadRequest,
		},
		{
			name:         "not found when target user missing",
			userID:       1001,
			targetUserID: 2001,
			followRepo:   &fakeFollowRepo{},
			userRepo:     &fakeFollowUserRepo{user: nil},
			invalidator:  &fakeFollowFeedInvalidator{},
			wantErr:      xerror.ErrNotFound,
		},
		{
			name:             "already followed on duplicated key",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{createErr: gorm.ErrDuplicatedKey},
			userRepo:         &fakeFollowUserRepo{user: &model.User{ID: 2001}},
			invalidator:      &fakeFollowFeedInvalidator{},
			wantErr:          xerror.ErrFollowAlreadyExists,
			wantCreateCalled: true,
		},
		{
			name:             "success should invalidate feed cache",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{},
			userRepo:         &fakeFollowUserRepo{user: &model.User{ID: 2001}},
			invalidator:      &fakeFollowFeedInvalidator{},
			wantCreateCalled: true,
			wantInvalidate:   true,
		},
		{
			name:             "success even when invalidator fails",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{},
			userRepo:         &fakeFollowUserRepo{user: &model.User{ID: 2001}},
			invalidator:      &fakeFollowFeedInvalidator{err: errors.New("cache down")},
			wantCreateCalled: true,
			wantInvalidate:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewFollowService(tc.followRepo, tc.userRepo).WithFeedCacheInvalidator(tc.invalidator)
			gotErr := svc.Follow(ctx, tc.userID, tc.targetUserID)

			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}
			if tc.wantCreateCalled && tc.followRepo.createCalled != 1 {
				t.Fatalf("expected create called once, got=%d", tc.followRepo.createCalled)
			}
			if !tc.wantCreateCalled && tc.followRepo.createCalled != 0 {
				t.Fatalf("expected create not called, got=%d", tc.followRepo.createCalled)
			}
			if tc.wantInvalidate && tc.invalidator.called != 1 {
				t.Fatalf("expected invalidator called once, got=%d", tc.invalidator.called)
			}
			if !tc.wantInvalidate && tc.invalidator.called != 0 {
				t.Fatalf("expected invalidator not called, got=%d", tc.invalidator.called)
			}
			if tc.wantInvalidate && tc.invalidator.gotUserID != tc.userID {
				t.Fatalf("unexpected invalidator user id: got=%d want=%d", tc.invalidator.gotUserID, tc.userID)
			}
		})
	}
}

func TestFollowServiceUnfollow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name             string
		userID           int64
		targetUserID     int64
		followRepo       *fakeFollowRepo
		invalidator      *fakeFollowFeedInvalidator
		inboxCleanup     *fakeFollowInboxCleanup
		wantErr          *xerror.Error
		wantDeleteCalled bool
		wantInvalidate   bool
		wantCleanup      bool
	}{
		{
			name:         "bad request on self unfollow",
			userID:       1001,
			targetUserID: 1001,
			followRepo:   &fakeFollowRepo{},
			invalidator:  &fakeFollowFeedInvalidator{},
			wantErr:      xerror.ErrBadRequest,
		},
		{
			name:             "internal when repository delete fails",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{deleteErr: errors.New("delete failed")},
			invalidator:      &fakeFollowFeedInvalidator{},
			wantErr:          xerror.ErrInternal,
			wantDeleteCalled: true,
		},
		{
			name:             "success should invalidate feed cache",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{deleted: true},
			invalidator:      &fakeFollowFeedInvalidator{},
			inboxCleanup:     &fakeFollowInboxCleanup{},
			wantDeleteCalled: true,
			wantInvalidate:   true,
			wantCleanup:      true,
		},
		{
			name:             "success even when invalidator fails",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{deleted: true},
			invalidator:      &fakeFollowFeedInvalidator{err: errors.New("cache down")},
			inboxCleanup:     &fakeFollowInboxCleanup{},
			wantDeleteCalled: true,
			wantInvalidate:   true,
			wantCleanup:      true,
		},
		{
			name:             "success even when inbox cleanup fails",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{deleted: true},
			invalidator:      &fakeFollowFeedInvalidator{},
			inboxCleanup:     &fakeFollowInboxCleanup{err: errors.New("cleanup down")},
			wantDeleteCalled: true,
			wantInvalidate:   true,
			wantCleanup:      true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewFollowService(tc.followRepo, &fakeFollowUserRepo{}).
				WithFeedCacheInvalidator(tc.invalidator).
				WithInboxAuthorCleanup(tc.inboxCleanup)
			gotErr := svc.Unfollow(ctx, tc.userID, tc.targetUserID)

			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}
			if tc.wantDeleteCalled && tc.followRepo.deleteCalled != 1 {
				t.Fatalf("expected delete called once, got=%d", tc.followRepo.deleteCalled)
			}
			if !tc.wantDeleteCalled && tc.followRepo.deleteCalled != 0 {
				t.Fatalf("expected delete not called, got=%d", tc.followRepo.deleteCalled)
			}
			if tc.wantInvalidate && tc.invalidator.called != 1 {
				t.Fatalf("expected invalidator called once, got=%d", tc.invalidator.called)
			}
			if !tc.wantInvalidate && tc.invalidator.called != 0 {
				t.Fatalf("expected invalidator not called, got=%d", tc.invalidator.called)
			}
			if tc.wantInvalidate && tc.invalidator.gotUserID != tc.userID {
				t.Fatalf("unexpected invalidator user id: got=%d want=%d", tc.invalidator.gotUserID, tc.userID)
			}
			if tc.wantCleanup && tc.inboxCleanup.called != 1 {
				t.Fatalf("expected inbox cleanup called once, got=%d", tc.inboxCleanup.called)
			}
			if !tc.wantCleanup && tc.inboxCleanup != nil && tc.inboxCleanup.called != 0 {
				t.Fatalf("expected inbox cleanup not called, got=%d", tc.inboxCleanup.called)
			}
			if tc.wantCleanup && tc.inboxCleanup.gotUserID != tc.userID {
				t.Fatalf("unexpected inbox cleanup user id: got=%d want=%d", tc.inboxCleanup.gotUserID, tc.userID)
			}
			if tc.wantCleanup && tc.inboxCleanup.gotAuthorUserID != tc.targetUserID {
				t.Fatalf("unexpected inbox cleanup author id: got=%d want=%d", tc.inboxCleanup.gotAuthorUserID, tc.targetUserID)
			}
		})
	}
}

func TestFollowServiceFollowMaintainsUserCountsInTransaction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	followRepo := &fakeFollowRepo{}
	userRepo := &fakeFollowUserRepo{user: &model.User{ID: 2001}}
	userCountRepo := &fakeFollowUserCountRepo{}
	txRunner := &fakeTransactionRunner{}
	invalidator := &fakeFollowFeedInvalidator{}

	svc := NewFollowServiceWithTransaction(txRunner, followRepo, userRepo, userCountRepo).
		WithFeedCacheInvalidator(invalidator)

	gotErr := svc.Follow(ctx, 1001, 2001)

	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if txRunner.called != 1 {
		t.Fatalf("expected transaction runner called once, got=%d", txRunner.called)
	}
	if len(userCountRepo.followingCalls) != 1 {
		t.Fatalf("expected one following count update, got=%d", len(userCountRepo.followingCalls))
	}
	if len(userCountRepo.followerCalls) != 1 {
		t.Fatalf("expected one follower count update, got=%d", len(userCountRepo.followerCalls))
	}
	if userCountRepo.followingCalls[0].userID != 1001 || userCountRepo.followingCalls[0].delta != 1 {
		t.Fatalf("unexpected following count update: %+v", userCountRepo.followingCalls[0])
	}
	if userCountRepo.followerCalls[0].userID != 2001 || userCountRepo.followerCalls[0].delta != 1 {
		t.Fatalf("unexpected follower count update: %+v", userCountRepo.followerCalls[0])
	}
	if invalidator.called != 1 {
		t.Fatalf("expected invalidator called once, got=%d", invalidator.called)
	}
}

func TestFollowServiceUnfollowMaintainsUserCountsOnlyWhenDeleted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	followRepo := &fakeFollowRepo{deleted: true}
	userCountRepo := &fakeFollowUserCountRepo{}
	txRunner := &fakeTransactionRunner{}
	invalidator := &fakeFollowFeedInvalidator{}
	cleanup := &fakeFollowInboxCleanup{}

	svc := NewFollowServiceWithTransaction(txRunner, followRepo, &fakeFollowUserRepo{}, userCountRepo).
		WithFeedCacheInvalidator(invalidator).
		WithInboxAuthorCleanup(cleanup)

	gotErr := svc.Unfollow(ctx, 1001, 2001)

	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if txRunner.called != 1 {
		t.Fatalf("expected transaction runner called once, got=%d", txRunner.called)
	}
	if len(userCountRepo.followingCalls) != 1 || len(userCountRepo.followerCalls) != 1 {
		t.Fatalf("expected one decrement for both counters, got following=%d follower=%d", len(userCountRepo.followingCalls), len(userCountRepo.followerCalls))
	}
	if userCountRepo.followingCalls[0].userID != 1001 || userCountRepo.followingCalls[0].delta != -1 {
		t.Fatalf("unexpected following count update: %+v", userCountRepo.followingCalls[0])
	}
	if userCountRepo.followerCalls[0].userID != 2001 || userCountRepo.followerCalls[0].delta != -1 {
		t.Fatalf("unexpected follower count update: %+v", userCountRepo.followerCalls[0])
	}
	if invalidator.called != 1 {
		t.Fatalf("expected invalidator called once, got=%d", invalidator.called)
	}
	if cleanup.called != 1 {
		t.Fatalf("expected cleanup called once, got=%d", cleanup.called)
	}
}

func TestFollowServiceUnfollowNoopDoesNotTouchCounts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	followRepo := &fakeFollowRepo{deleted: false}
	userCountRepo := &fakeFollowUserCountRepo{}
	txRunner := &fakeTransactionRunner{}
	invalidator := &fakeFollowFeedInvalidator{}
	cleanup := &fakeFollowInboxCleanup{}

	svc := NewFollowServiceWithTransaction(txRunner, followRepo, &fakeFollowUserRepo{}, userCountRepo).
		WithFeedCacheInvalidator(invalidator).
		WithInboxAuthorCleanup(cleanup)

	gotErr := svc.Unfollow(ctx, 1001, 2001)

	if gotErr != nil {
		t.Fatalf("unexpected error: %v", gotErr)
	}
	if txRunner.called != 1 {
		t.Fatalf("expected transaction runner called once, got=%d", txRunner.called)
	}
	if len(userCountRepo.followingCalls) != 0 || len(userCountRepo.followerCalls) != 0 {
		t.Fatalf("expected no count updates, got following=%d follower=%d", len(userCountRepo.followingCalls), len(userCountRepo.followerCalls))
	}
	if invalidator.called != 0 {
		t.Fatalf("expected invalidator not called on noop unfollow, got=%d", invalidator.called)
	}
	if cleanup.called != 0 {
		t.Fatalf("expected cleanup not called on noop unfollow, got=%d", cleanup.called)
	}
}
