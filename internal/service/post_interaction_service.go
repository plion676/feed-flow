package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

const (
	defaultCommentLimit = 20
	maxCommentLimit     = 50
	maxCommentLen       = 300
	maxStatusPostIDs    = 100
)

type postInteractionPostRepository interface {
	GetByID(ctx context.Context, postID int64) (*model.Post, error)
}

type postInteractionRepository interface {
	Like(ctx context.Context, userID int64, postID int64) error
	Unlike(ctx context.Context, userID int64, postID int64) error
	Collect(ctx context.Context, userID int64, postID int64) error
	Uncollect(ctx context.Context, userID int64, postID int64) error
	CreateComment(ctx context.Context, comment *model.PostComment) error
	ListComments(ctx context.Context, postID int64, lastCommentID int64, limit int) ([]*model.PostComment, error)
	CountLikesByPostIDs(ctx context.Context, postIDs []int64) (map[int64]int64, error)
	CountCollectsByPostIDs(ctx context.Context, postIDs []int64) (map[int64]int64, error)
	CountCommentsByPostIDs(ctx context.Context, postIDs []int64) (map[int64]int64, error)
	ListLikedPostIDs(ctx context.Context, userID int64, postIDs []int64) ([]int64, error)
	ListCollectedPostIDs(ctx context.Context, userID int64, postIDs []int64) ([]int64, error)
}

// PostInteractionService handles likes, collects, and comments.
type PostInteractionService struct {
	postRepo        postInteractionPostRepository
	interactionRepo postInteractionRepository
}

type PostInteractionResult struct {
	PostID       int64 `json:"post_id"`
	Liked        bool  `json:"liked"`
	Collected    bool  `json:"collected"`
	LikeCount    int64 `json:"like_count"`
	CollectCount int64 `json:"collect_count"`
	CommentCount int64 `json:"comment_count"`
}

type PostInteractionStatusResult struct {
	Items []PostInteractionResult `json:"items"`
}

type CreateCommentRequest struct {
	UserID  int64
	PostID  int64
	Content string
}

type CommentResult struct {
	CommentID int64     `json:"comment_id"`
	PostID    int64     `json:"post_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type CommentListResult struct {
	Items      []CommentResult `json:"items"`
	NextCursor int64           `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

func NewPostInteractionService(postRepo postInteractionPostRepository, interactionRepo postInteractionRepository) *PostInteractionService {
	return &PostInteractionService{
		postRepo:        postRepo,
		interactionRepo: interactionRepo,
	}
}

func (s *PostInteractionService) Like(ctx context.Context, userID int64, postID int64) (*PostInteractionResult, *xerror.Error) {
	if err := s.ensurePublishedPost(ctx, postID); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, xerror.ErrUnauthorized
	}
	if err := s.interactionRepo.Like(ctx, userID, postID); err != nil {
		return nil, xerror.ErrInternal
	}
	return s.GetStatus(ctx, userID, postID)
}

func (s *PostInteractionService) Unlike(ctx context.Context, userID int64, postID int64) (*PostInteractionResult, *xerror.Error) {
	if err := s.ensurePublishedPost(ctx, postID); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, xerror.ErrUnauthorized
	}
	if err := s.interactionRepo.Unlike(ctx, userID, postID); err != nil {
		return nil, xerror.ErrInternal
	}
	return s.GetStatus(ctx, userID, postID)
}

func (s *PostInteractionService) Collect(ctx context.Context, userID int64, postID int64) (*PostInteractionResult, *xerror.Error) {
	if err := s.ensurePublishedPost(ctx, postID); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, xerror.ErrUnauthorized
	}
	if err := s.interactionRepo.Collect(ctx, userID, postID); err != nil {
		return nil, xerror.ErrInternal
	}
	return s.GetStatus(ctx, userID, postID)
}

func (s *PostInteractionService) Uncollect(ctx context.Context, userID int64, postID int64) (*PostInteractionResult, *xerror.Error) {
	if err := s.ensurePublishedPost(ctx, postID); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, xerror.ErrUnauthorized
	}
	if err := s.interactionRepo.Uncollect(ctx, userID, postID); err != nil {
		return nil, xerror.ErrInternal
	}
	return s.GetStatus(ctx, userID, postID)
}

func (s *PostInteractionService) CreateComment(ctx context.Context, req CreateCommentRequest) (*CommentResult, *xerror.Error) {
	if req.UserID <= 0 {
		return nil, xerror.ErrUnauthorized
	}
	if err := s.ensurePublishedPost(ctx, req.PostID); err != nil {
		return nil, err
	}

	content := strings.TrimSpace(req.Content)
	if content == "" || utf8.RuneCountInString(content) > maxCommentLen {
		return nil, xerror.ErrBadRequest
	}

	comment := &model.PostComment{
		PostID:  req.PostID,
		UserID:  req.UserID,
		Content: content,
		Status:  model.CommentStatusPublished,
	}
	if err := s.interactionRepo.CreateComment(ctx, comment); err != nil {
		return nil, xerror.ErrInternal
	}
	return mapCommentResult(comment), nil
}

func (s *PostInteractionService) ListComments(ctx context.Context, postID int64, lastCommentID int64, limit int) (*CommentListResult, *xerror.Error) {
	if err := s.ensurePublishedPost(ctx, postID); err != nil {
		return nil, err
	}
	if lastCommentID < 0 {
		return nil, xerror.ErrBadRequest
	}
	if limit <= 0 {
		limit = defaultCommentLimit
	}
	if limit > maxCommentLimit {
		limit = maxCommentLimit
	}

	comments, err := s.interactionRepo.ListComments(ctx, postID, lastCommentID, limit+1)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	hasMore := len(comments) > limit
	if hasMore {
		comments = comments[:limit]
	}

	items := make([]CommentResult, 0, len(comments))
	for _, comment := range comments {
		items = append(items, *mapCommentResult(comment))
	}

	var nextCursor int64
	if len(items) > 0 {
		nextCursor = items[len(items)-1].CommentID
	}

	return &CommentListResult{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	}, nil
}

func (s *PostInteractionService) GetStatus(ctx context.Context, userID int64, postID int64) (*PostInteractionResult, *xerror.Error) {
	if err := s.ensurePublishedPost(ctx, postID); err != nil {
		return nil, err
	}
	result, err := s.GetStatuses(ctx, userID, []int64{postID})
	if err != nil {
		return nil, err
	}
	if len(result.Items) == 0 {
		return &PostInteractionResult{PostID: postID}, nil
	}
	return &result.Items[0], nil
}

func (s *PostInteractionService) GetStatuses(ctx context.Context, userID int64, postIDs []int64) (*PostInteractionStatusResult, *xerror.Error) {
	postIDs = uniquePositiveInt64s(postIDs, maxStatusPostIDs)
	if len(postIDs) == 0 {
		return &PostInteractionStatusResult{Items: []PostInteractionResult{}}, nil
	}

	likeCounts, err := s.interactionRepo.CountLikesByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	collectCounts, err := s.interactionRepo.CountCollectsByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, xerror.ErrInternal
	}
	commentCounts, err := s.interactionRepo.CountCommentsByPostIDs(ctx, postIDs)
	if err != nil {
		return nil, xerror.ErrInternal
	}

	liked := map[int64]struct{}{}
	collected := map[int64]struct{}{}
	if userID > 0 {
		likedIDs, err := s.interactionRepo.ListLikedPostIDs(ctx, userID, postIDs)
		if err != nil {
			return nil, xerror.ErrInternal
		}
		for _, id := range likedIDs {
			liked[id] = struct{}{}
		}
		collectedIDs, err := s.interactionRepo.ListCollectedPostIDs(ctx, userID, postIDs)
		if err != nil {
			return nil, xerror.ErrInternal
		}
		for _, id := range collectedIDs {
			collected[id] = struct{}{}
		}
	}

	items := make([]PostInteractionResult, 0, len(postIDs))
	for _, postID := range postIDs {
		_, isLiked := liked[postID]
		_, isCollected := collected[postID]
		items = append(items, PostInteractionResult{
			PostID:       postID,
			Liked:        isLiked,
			Collected:    isCollected,
			LikeCount:    likeCounts[postID],
			CollectCount: collectCounts[postID],
			CommentCount: commentCounts[postID],
		})
	}
	return &PostInteractionStatusResult{Items: items}, nil
}

func (s *PostInteractionService) ensurePublishedPost(ctx context.Context, postID int64) *xerror.Error {
	if postID <= 0 {
		return xerror.ErrBadRequest
	}
	post, err := s.postRepo.GetByID(ctx, postID)
	if err != nil {
		return xerror.ErrInternal
	}
	if post == nil || post.Status != model.PostStatusPublished {
		return xerror.ErrPostNotFound
	}
	return nil
}

func mapCommentResult(comment *model.PostComment) *CommentResult {
	if comment == nil {
		return nil
	}
	return &CommentResult{
		CommentID: comment.ID,
		PostID:    comment.PostID,
		UserID:    comment.UserID,
		Content:   comment.Content,
		CreatedAt: comment.CreatedAt,
	}
}

func uniquePositiveInt64s(values []int64, limit int) []int64 {
	if limit <= 0 {
		limit = len(values)
	}
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if len(out) >= limit {
			break
		}
	}
	return out
}
