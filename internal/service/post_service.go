package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/repository"
	"gorm.io/gorm"
)

type postRepository interface {
	Create(ctx context.Context, post *model.Post) error
	CreateTx(ctx context.Context, tx *gorm.DB, post *model.Post) error
	GetByID(ctx context.Context, postID int64) (*model.Post, error)
	SoftDeleteByIDAndUserID(ctx context.Context, postID int64, userID int64) (bool, error)
	SoftDeleteByIDAndUserIDTx(ctx context.Context, tx *gorm.DB, postID int64, userID int64) (bool, error)
}

type postFeedCacheInvalidator interface {
	InvalidateHomeFeed(ctx context.Context, userID int64) error
}

type postUserCountRepository interface {
	AddPostCountTx(ctx context.Context, tx *gorm.DB, userID int64, delta int64) error
}

type postEventOutboxRepository interface {
	CreateTx(ctx context.Context, tx *gorm.DB, event *model.FeedEventOutbox) error
}

// PostService handles post create/read workflows.
type PostService struct {
	txRunner        transactionRunner
	postRepo        postRepository
	userCountRepo   postUserCountRepository
	eventOutboxRepo postEventOutboxRepository
	feedInvalidator postFeedCacheInvalidator
	outboxRepo      feedOutboxRepository
	outboxMaxItems  int64
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

func NewPostServiceWithTransaction(txRunner transactionRunner, postRepo postRepository, userCountRepo postUserCountRepository) *PostService {
	return &PostService{
		txRunner:      txRunner,
		postRepo:      postRepo,
		userCountRepo: userCountRepo,
	}
}

// WithFeedCacheInvalidator wires an optional cache invalidator for feed consistency.
func (s *PostService) WithFeedCacheInvalidator(invalidator postFeedCacheInvalidator) *PostService {
	s.feedInvalidator = invalidator
	return s
}

// WithEventOutbox wires an optional DB outbox writer for reliable async event relay.
func (s *PostService) WithEventOutbox(eventOutboxRepo postEventOutboxRepository) *PostService {
	s.eventOutboxRepo = eventOutboxRepo
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

	err := s.runCreateInTx(ctx, post)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, xerror.ErrInternal
		}
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

	deleted, err := s.runDeleteInTx(ctx, req.PostID, req.UserID)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	if !deleted {
		return nil, xerror.ErrPostNotFound
	}

	if s.feedInvalidator != nil {
		_ = s.feedInvalidator.InvalidateHomeFeed(ctx, req.UserID)
	}
	return &DeletePostResult{
		PostID:  req.PostID,
		UserID:  req.UserID,
		Deleted: true,
	}, nil
}

func (s *PostService) runCreateInTx(ctx context.Context, post *model.Post) error {
	if s.txRunner == nil || s.userCountRepo == nil {
		return s.postRepo.Create(ctx, post)
	}

	return s.txRunner.InTx(ctx, func(tx *gorm.DB) error {
		if err := s.postRepo.CreateTx(ctx, tx, post); err != nil {
			return err
		}
		if err := s.userCountRepo.AddPostCountTx(ctx, tx, post.UserID, 1); err != nil {
			return err
		}
		if err := s.enqueuePostLifecycleEventTx(ctx, tx, model.FeedEventOutboxTypePostCreated, post.UserID, post.ID); err != nil {
			return err
		}
		return nil
	})
}

func (s *PostService) runDeleteInTx(ctx context.Context, postID int64, userID int64) (bool, error) {
	if s.txRunner == nil || s.userCountRepo == nil {
		return s.postRepo.SoftDeleteByIDAndUserID(ctx, postID, userID)
	}

	deleted := false
	err := s.txRunner.InTx(ctx, func(tx *gorm.DB) error {
		var err error
		deleted, err = s.postRepo.SoftDeleteByIDAndUserIDTx(ctx, tx, postID, userID)
		if err != nil {
			return err
		}
		if !deleted {
			return nil
		}
		if err := s.userCountRepo.AddPostCountTx(ctx, tx, userID, -1); err != nil {
			return err
		}
		if err := s.enqueuePostLifecycleEventTx(ctx, tx, model.FeedEventOutboxTypePostDeleted, userID, postID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *PostService) enqueuePostLifecycleEventTx(
	ctx context.Context,
	tx *gorm.DB,
	eventType string,
	authorUserID int64,
	postID int64,
) error {
	if s.eventOutboxRepo == nil {
		return nil
	}

	event := repository.FeedInvalidationEvent{
		Type:       eventType,
		AuthorID:   authorUserID,
		PostID:     postID,
		OccurredAt: time.Now().Unix(),
	}
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return s.eventOutboxRepo.CreateTx(ctx, tx, &model.FeedEventOutbox{
		EventType:     eventType,
		AggregateType: "post",
		AggregateID:   postID,
		AuthorUserID:  authorUserID,
		PostID:        postID,
		Payload:       string(payloadBytes),
		Status:        model.FeedEventOutboxStatusPending,
		NextRetryAt:   time.Now(),
	})
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
