package service

import (
	"context"
	"errors"
	"strings"

	"github.com/plion676/feed-flow/internal/pkg/xerror"
	"github.com/plion676/feed-flow/internal/repository"
)

const (
	defaultDLQListLimit = 20
	maxDLQListLimit     = 100
)

type feedDLQRepository interface {
	ListDLQEvents(ctx context.Context, count int64) ([]repository.FeedInvalidationDLQRecord, error)
	ReplayDLQEvent(ctx context.Context, dlqMessageID string, deleteAfterReplay bool) (*repository.ReplayDLQResult, error)
}

type FeedDLQService struct {
	repo feedDLQRepository
}

type ListDLQEventsRequest struct {
	Limit int `json:"limit"`
}

type ListDLQEventsResult struct {
	Items []repository.FeedInvalidationDLQRecord `json:"items"`
}

type ReplayDLQEventRequest struct {
	DLQMessageID      string `json:"dlq_message_id"`
	DeleteAfterReplay bool   `json:"delete_after_replay"`
}

func NewFeedDLQService(repo feedDLQRepository) *FeedDLQService {
	return &FeedDLQService{repo: repo}
}

func (s *FeedDLQService) ListEvents(ctx context.Context, req ListDLQEventsRequest) (*ListDLQEventsResult, *xerror.Error) {
	if s == nil || s.repo == nil {
		return nil, xerror.ErrInternal
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultDLQListLimit
	}
	if limit > maxDLQListLimit {
		limit = maxDLQListLimit
	}

	items, err := s.repo.ListDLQEvents(ctx, int64(limit))
	if err != nil {
		return nil, xerror.ErrInternal
	}

	return &ListDLQEventsResult{
		Items: items,
	}, nil
}

func (s *FeedDLQService) ReplayEvent(ctx context.Context, req ReplayDLQEventRequest) (*repository.ReplayDLQResult, *xerror.Error) {
	if s == nil || s.repo == nil {
		return nil, xerror.ErrInternal
	}

	if strings.TrimSpace(req.DLQMessageID) == "" {
		return nil, xerror.ErrBadRequest
	}

	result, err := s.repo.ReplayDLQEvent(ctx, req.DLQMessageID, req.DeleteAfterReplay)
	if err != nil {
		if errors.Is(err, repository.ErrDLQEventNotFound) {
			return nil, xerror.ErrNotFound
		}
		return nil, xerror.ErrInternal
	}

	return result, nil
}
