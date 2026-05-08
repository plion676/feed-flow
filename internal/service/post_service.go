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
	SoftDeleteByIDAndUserID(ctx context.Context, postID int64, userID int64) (bool, error)
}

type postFeedCacheInvalidator interface {
	InvalidateHomeFeed(ctx context.Context, userID int64) error
}

type postFeedInvalidationEventPublisher interface {
	PublishPostCreatedEvent(ctx context.Context, authorUserID int64, postID int64) error
	PublishPostDeletedEvent(ctx context.Context, authorUserID int64, postID int64) error
}

// PostService handles post create/read workflows.
type PostService struct {
	postRepo             postRepository
	feedInvalidator      postFeedCacheInvalidator
	invalidationEventPub postFeedInvalidationEventPublisher
	outboxRepo           feedOutboxRepository
	outboxMaxItems       int64
}

type CreatePostRequest struct {
	UserID  int64
	Content string
}

type DeletePostRequest struct {
	UserID int64
	PostID int64
}

type PostResult struct {
	PostID    int64     `json:"post_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type DeletePostResult struct {
	PostID  int64 `json:"post_id"`
	UserID  int64 `json:"user_id"`
	Deleted bool  `json:"deleted"`
}

const maxPostContentLen = 500

func NewPostService(postRepo postRepository) *PostService {
	return &PostService{postRepo: postRepo}
}

// WithFeedCacheInvalidator wires an optional cache invalidator for feed consistency.
func (s *PostService) WithFeedCacheInvalidator(invalidator postFeedCacheInvalidator) *PostService {
	s.feedInvalidator = invalidator
	return s
}

// WithFeedInvalidationEventPublisher wires an optional async event publisher.
func (s *PostService) WithFeedInvalidationEventPublisher(publisher postFeedInvalidationEventPublisher) *PostService {
	s.invalidationEventPub = publisher
	return s
}

// WithFeedOutbox wires an optional author outbox writer for hybrid pull reads.
func (s *PostService) WithFeedOutbox(outboxRepo feedOutboxRepository, maxItems int64) *PostService {
	s.outboxRepo = outboxRepo
	s.outboxMaxItems = maxItems
	return s
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
		Status:  model.PostStatusPublished,
	}

	err := s.postRepo.Create(ctx, post)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	if s.feedInvalidator != nil {
		// Best-effort cache invalidation: publishing should not fail because cache cleanup fails.
		_ = s.feedInvalidator.InvalidateHomeFeed(ctx, req.UserID)
	}
	if s.outboxRepo != nil {
		// Best-effort outbox write: Redis repair is also covered by async worker replay.
		_ = s.outboxRepo.AddPostToOutbox(ctx, req.UserID, post.ID)
		if s.outboxMaxItems > 0 {
			_ = s.outboxRepo.TrimOutbox(ctx, req.UserID, s.outboxMaxItems)
		}
	}
	if s.invalidationEventPub != nil {
		// Best-effort async signal: queue publish failure should not fail post creation.
		_ = s.invalidationEventPub.PublishPostCreatedEvent(ctx, req.UserID, post.ID)
	}

	return &PostResult{
		PostID:    post.ID,
		UserID:    post.UserID,
		Content:   post.Content,
		CreatedAt: post.CreatedAt,
	}, nil

}

func (s *PostService) Delete(ctx context.Context, req DeletePostRequest) (*DeletePostResult, *xerror.Error) {
	if req.UserID <= 0 || req.PostID <= 0 {
		return nil, xerror.ErrBadRequest
	}

	post, err := s.postRepo.GetByID(ctx, req.PostID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if post == nil || post.Status != model.PostStatusPublished {
		return nil, xerror.ErrPostNotFound
	}
	if post.UserID != req.UserID {
		return nil, xerror.ErrForbidden
	}

	deleted, err := s.postRepo.SoftDeleteByIDAndUserID(ctx, req.PostID, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if !deleted {
		return nil, xerror.ErrPostNotFound
	}

	if s.feedInvalidator != nil {
		_ = s.feedInvalidator.InvalidateHomeFeed(ctx, req.UserID)
	}
	if s.invalidationEventPub != nil {
		_ = s.invalidationEventPub.PublishPostDeletedEvent(ctx, req.UserID, req.PostID)
	}

	return &DeletePostResult{
		PostID:  req.PostID,
		UserID:  req.UserID,
		Deleted: true,
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
	if post == nil || post.Status != model.PostStatusPublished {
		return nil, xerror.ErrPostNotFound
	}

	return &PostResult{
		PostID:    post.ID,
		UserID:    post.UserID,
		Content:   post.Content,
		CreatedAt: post.CreatedAt,
	}, nil
}
