package service

import (
	"context"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type feedDLQOperatorRepository interface {
	GetByUserID(ctx context.Context, userID int64) (*model.FeedDLQOperator, error)
}

type FeedDLQAccessService struct {
	repo feedDLQOperatorRepository
}

func NewFeedDLQAccessService(repo feedDLQOperatorRepository) *FeedDLQAccessService {
	return &FeedDLQAccessService{repo: repo}
}

func (s *FeedDLQAccessService) AuthorizeList(ctx context.Context, userID int64) *xerror.Error {
	return s.authorize(ctx, userID, false)
}

func (s *FeedDLQAccessService) AuthorizeReplay(ctx context.Context, userID int64) *xerror.Error {
	return s.authorize(ctx, userID, true)
}

func (s *FeedDLQAccessService) authorize(ctx context.Context, userID int64, adminOnly bool) *xerror.Error {
	if s == nil || s.repo == nil {
		return xerror.ErrInternal
	}
	if userID <= 0 {
		return xerror.ErrUnauthorized
	}

	operator, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		return xerror.ErrInternal
	}
	if operator == nil || operator.Status != model.FeedDLQOperatorStatusActive {
		return xerror.ErrForbidden
	}
	if adminOnly {
		if operator.Role != model.FeedDLQOperatorRoleAdmin {
			return xerror.ErrForbidden
		}
		return nil
	}

	if operator.Role != model.FeedDLQOperatorRoleAdmin && operator.Role != model.FeedDLQOperatorRoleOperator {
		return xerror.ErrForbidden
	}
	return nil
}
