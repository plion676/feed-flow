package service

import (
	"context"
	"errors"
	"testing"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type fakeUserRepo struct {
	user *model.User
	err  error
}

func (r *fakeUserRepo) GetByUsername(_ context.Context, _ string) (*model.User, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.user, nil
}

func (r *fakeUserRepo) CreateTx(_ context.Context, _ *gorm.DB, _ *model.User) error {
	return nil
}

type fakeUserCountRepo struct{}

func (r *fakeUserCountRepo) InitTx(_ context.Context, _ *gorm.DB, _ *model.UserCount) error {
	return nil
}

type fakeTokenManager struct {
	token string
	err   error
}

func (m *fakeTokenManager) GenerateToken(_ int64) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.token, nil
}

func hashForTest(t *testing.T, plain string) string {
	t.Helper()

	hashBytes, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash test password failed: %v", err)
	}
	return string(hashBytes)
}

func TestAuthServiceLogin(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	passwordHash := hashForTest(t, "correct-password")

	testCases := []struct {
		name          string
		req           LoginRequest
		repo          *fakeUserRepo
		tokenManager  *fakeTokenManager
		wantErr       *xerror.Error
		wantToken     string
		wantUsername  string
		wantNickname  string
		wantUserID    int64
	}{
		{
			name:         "bad request when username empty",
			req:          LoginRequest{Username: " ", Password: "123456"},
			repo:         &fakeUserRepo{},
			tokenManager: &fakeTokenManager{token: "unused"},
			wantErr:      xerror.ErrBadRequest,
		},
		{
			name:         "invalid credentials when user not found",
			req:          LoginRequest{Username: "alice", Password: "123456"},
			repo:         &fakeUserRepo{user: nil},
			tokenManager: &fakeTokenManager{token: "unused"},
			wantErr:      xerror.ErrInvalidCredentials,
		},
		{
			name: "invalid credentials when password mismatch",
			req:  LoginRequest{Username: "alice", Password: "wrong-password"},
			repo: &fakeUserRepo{
				user: &model.User{
					ID:           1001,
					Username:     "alice",
					Nickname:     "Alice",
					PasswordHash: passwordHash,
				},
			},
			tokenManager: &fakeTokenManager{token: "unused"},
			wantErr:      xerror.ErrInvalidCredentials,
		},
		{
			name: "internal error when token generation fails",
			req:  LoginRequest{Username: "alice", Password: "correct-password"},
			repo: &fakeUserRepo{
				user: &model.User{
					ID:           1001,
					Username:     "alice",
					Nickname:     "Alice",
					PasswordHash: passwordHash,
				},
			},
			tokenManager: &fakeTokenManager{err: errors.New("token sign failed")},
			wantErr:      xerror.ErrInternal,
		},
		{
			name: "success",
			req:  LoginRequest{Username: "alice", Password: "correct-password"},
			repo: &fakeUserRepo{
				user: &model.User{
					ID:           1001,
					Username:     "alice",
					Nickname:     "Alice",
					PasswordHash: passwordHash,
				},
			},
			tokenManager: &fakeTokenManager{token: "mock-token"},
			wantToken:    "mock-token",
			wantUsername: "alice",
			wantNickname: "Alice",
			wantUserID:   1001,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := NewAuthService(nil, tc.repo, &fakeUserCountRepo{}, tc.tokenManager)
			got, gotErr := svc.Login(ctx, tc.req)

			if gotErr != tc.wantErr {
				t.Fatalf("unexpected error: got=%v want=%v", gotErr, tc.wantErr)
			}

			if tc.wantErr != nil {
				if got != nil {
					t.Fatalf("expected nil result when error happens, got=%+v", got)
				}
				return
			}

			if got == nil {
				t.Fatal("expected non-nil result on success")
			}
			if got.Token != tc.wantToken {
				t.Fatalf("unexpected token: got=%s want=%s", got.Token, tc.wantToken)
			}
			if got.Username != tc.wantUsername {
				t.Fatalf("unexpected username: got=%s want=%s", got.Username, tc.wantUsername)
			}
			if got.Nickname != tc.wantNickname {
				t.Fatalf("unexpected nickname: got=%s want=%s", got.Nickname, tc.wantNickname)
			}
			if got.UserID != tc.wantUserID {
				t.Fatalf("unexpected user id: got=%d want=%d", got.UserID, tc.wantUserID)
			}
		})
	}
}
