package service

import (
	"context"
	"time"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type userReadRepository interface {
	GetByID(ctx context.Context, userID int64) (*model.User, error)
}

type userPostReadRepository interface {
	ListByUserID(ctx context.Context, userID int64, lastPostID int64, limit int) ([]*model.Post, error)
}

// UserService handles user profile related read operations.
type UserService struct {
	userRepo userReadRepository
	postRepo userPostReadRepository
}

type MeResult struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
}

const (
	defaultUserPostsLimit = 20
	maxUserPostsLimit     = 50
)

type GetUserPostsRequest struct {
	UserID     int64
	LastPostID int64
	Limit      int
}

type UserPostItem struct {
	PostID    int64     `json:"post_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type UserPostsResult struct {
	Items      []UserPostItem `json:"items"`
	NextCursor int64          `json:"next_cursor"`
	HasMore    bool           `json:"has_more"`
}

func NewUserService(userRepo userReadRepository) *UserService {
	return &UserService{userRepo: userRepo}
}

func (s *UserService) WithPostRepository(postRepo userPostReadRepository) *UserService {
	s.postRepo = postRepo
	return s
}

func (s *UserService) GetMe(ctx context.Context, userID int64) (*MeResult, *xerror.Error) {
	if userID <= 0 {
		return nil, xerror.ErrUnauthorized
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if user == nil {
		return nil, xerror.ErrUnauthorized
	}

	return &MeResult{
		UserID:   user.ID,
		Username: user.Username,
		Nickname: user.Nickname,
		Avatar:   user.Avatar,
		Bio:      user.Bio,
	}, nil
}

func (s *UserService) GetUserPosts(ctx context.Context, req GetUserPostsRequest) (*UserPostsResult, *xerror.Error) {
	if req.UserID <= 0 || req.LastPostID < 0 {
		return nil, xerror.ErrBadRequest
	}
	if s.postRepo == nil {
		return nil, xerror.ErrInternal
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultUserPostsLimit
	}
	if limit > maxUserPostsLimit {
		limit = maxUserPostsLimit
	}

	user, err := s.userRepo.GetByID(ctx, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if user == nil {
		return nil, xerror.ErrNotFound
	}

	posts, err := s.postRepo.ListByUserID(ctx, req.UserID, req.LastPostID, limit+1)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	hasMore := len(posts) > limit
	if hasMore {
		posts = posts[:limit]
	}

	items := make([]UserPostItem, 0, len(posts))
	for _, post := range posts {
		if post == nil {
			continue
		}
		items = append(items, UserPostItem{
			PostID:    post.ID,
			UserID:    post.UserID,
			Content:   post.Content,
			CreatedAt: post.CreatedAt,
		})
	}

	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].PostID
	}

	return &UserPostsResult{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}
