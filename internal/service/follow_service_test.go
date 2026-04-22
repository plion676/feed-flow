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

func (r *fakeFollowRepo) Delete(_ context.Context, userID int64, targetUserID int64) error {
	r.deleteCalled++
	r.lastDelete.userID = userID
	r.lastDelete.targetUserID = targetUserID
	return r.deleteErr
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

func (f *fakeFollowFeedInvalidator) InvalidateHomeFeed(_ context.Context, userID int64) error {
	f.called++
	f.gotUserID = userID
	return f.err
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
		wantErr          *xerror.Error
		wantDeleteCalled bool
		wantInvalidate   bool
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
			followRepo:       &fakeFollowRepo{},
			invalidator:      &fakeFollowFeedInvalidator{},
			wantDeleteCalled: true,
			wantInvalidate:   true,
		},
		{
			name:             "success even when invalidator fails",
			userID:           1001,
			targetUserID:     2001,
			followRepo:       &fakeFollowRepo{},
			invalidator:      &fakeFollowFeedInvalidator{err: errors.New("cache down")},
			wantDeleteCalled: true,
			wantInvalidate:   true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewFollowService(tc.followRepo, &fakeFollowUserRepo{}).WithFeedCacheInvalidator(tc.invalidator)
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
		})
	}
}
