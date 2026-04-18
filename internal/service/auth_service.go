package service

import (
	"context"
	"errors"
	"strings"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/repository"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// RegisterRequest is the service-layer input for user registration.
type RegisterRequest struct {
	Username string
	Password string
	Nickname string
}

// RegisterResult is the service-layer output for user registration.
type RegisterResult struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// AuthService contains the registration and login workflows.
type AuthService struct {
	db            *gorm.DB
	userRepo      *repository.UserRepository
	userCountRepo *repository.UserCountRepository
}

func NewAuthService(db *gorm.DB, userRepo *repository.UserRepository, userCountRepo *repository.UserCountRepository) *AuthService {
	return &AuthService{
		db:            db,
		userRepo:      userRepo,
		userCountRepo: userCountRepo,
	}
}

func (s *AuthService) Register(ctx context.Context, req RegisterRequest) (*RegisterResult, *xerror.Error) {
	username := strings.TrimSpace(req.Username)
	nickname := strings.TrimSpace(req.Nickname)

	if username == "" || strings.TrimSpace(req.Password) == "" || nickname == "" {
		return nil, xerror.ErrBadRequest
	}

	existingUser, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if existingUser != nil {
		return nil, xerror.ErrUserAlreadyExists
	}

	passwordHash, hashErr := s.hashPassword(req.Password)
	if hashErr != nil {
		return nil, hashErr
	}

	user := &model.User{
		Username:     username,
		PasswordHash: passwordHash,
		Nickname:     nickname,
		Status:       1,
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.userRepo.CreateTx(ctx, tx, user); err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return xerror.ErrUserAlreadyExists
			}
			return err
		}

		userCount := &model.UserCount{
			UserID:         user.ID,
			FollowingCount: 0,
			FollowerCount:  0,
			PostCount:      0,
		}

		if err := s.userCountRepo.InitTx(ctx, tx, userCount); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		if bizErr, ok := err.(*xerror.Error); ok {
			return nil, bizErr
		}
		return nil, xerror.ErrInternal
	}

	return &RegisterResult{
		UserID:   user.ID,
		Username: user.Username,
		Nickname: user.Nickname,
	}, nil
}

func (s *AuthService) hashPassword(password string) (string, *xerror.Error) {
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", xerror.ErrInternal
	}

	return string(hashBytes), nil
}
