package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type postRepository interface {
	Create(ctx context.Context, post *model.Post) error
	GetByID(ctx context.Context, postID int64) (*model.Post, error)
}

// PostService handles post create/read workflows.
type PostService struct {
	postRepo postRepository
}

type CreatePostRequest struct {
	UserID  int64
	Content string
}

type PostResult struct {
	PostID    int64     `json:"post_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

const maxPostContentLen = 500

func NewPostService(postRepo postRepository) *PostService {
	return &PostService{postRepo: postRepo}
}

func (s *PostService) Create(ctx context.Context, req CreatePostRequest) (*PostResult, *xerror.Error) {
	trimmedContent := strings.TrimSpace(req.Content)
	if req.UserID <= 0 || trimmedContent == "" {
		return nil, xerror.ErrBadRequest
	}
	if utf8.RuneCountInString(trimmedContent) > maxPostContentLen {
		return nil, xerror.ErrBadRequest
	}

	post := &model.Post{
		UserID:  req.UserID,
		Content: trimmedContent,
		Status:  1,
	}

	err := s.postRepo.Create(ctx, post)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	return &PostResult{
		PostID:    post.ID,
		UserID:    post.UserID,
		Content:   post.Content,
		CreatedAt: post.CreatedAt,
	}, nil

}

func (s *PostService) GetByID(ctx context.Context, postID int64) (*PostResult, *xerror.Error) {
	if postID <= 0 {
		return nil, xerror.ErrBadRequest
	}

	post, err := s.postRepo.GetByID(ctx, postID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if post == nil || post.Status != 1 {
		return nil, xerror.ErrPostNotFound
	}

	return &PostResult{
		PostID:    post.ID,
		UserID:    post.UserID,
		Content:   post.Content,
		CreatedAt: post.CreatedAt,
	}, nil
}
