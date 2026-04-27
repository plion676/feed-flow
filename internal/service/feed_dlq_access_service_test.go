package service

import (
	"context"
	"errors"
	"testing"

	"github.com/plion676/feed-flow/internal/model"
	"github.com/plion676/feed-flow/internal/pkg/xerror"
)

type fakeDLQOperatorRepo struct {
	operator *model.FeedDLQOperator
	err      error
}

func (f *fakeDLQOperatorRepo) GetByUserID(_ context.Context, _ int64) (*model.FeedDLQOperator, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.operator, nil
}

func TestFeedDLQAccessServiceAuthorizeList(t *testing.T) {
	t.Parallel()

	t.Run("allow operator", func(t *testing.T) {
		t.Parallel()
		svc := NewFeedDLQAccessService(&fakeDLQOperatorRepo{
			operator: &model.FeedDLQOperator{
				UserID: 1,
				Role:   model.FeedDLQOperatorRoleOperator,
				Status: model.FeedDLQOperatorStatusActive,
				Remark: "ops",
			},
		})
		if bizErr := svc.AuthorizeList(context.Background(), 1); bizErr != nil {
			t.Fatalf("unexpected biz error: %v", bizErr)
		}
	})

	t.Run("deny unknown user", func(t *testing.T) {
		t.Parallel()
		svc := NewFeedDLQAccessService(&fakeDLQOperatorRepo{})
		bizErr := svc.AuthorizeList(context.Background(), 2)
		if bizErr == nil || bizErr.Code != xerror.CodeForbidden {
			t.Fatalf("unexpected biz error: %+v", bizErr)
		}
	})

	t.Run("deny disabled operator", func(t *testing.T) {
		t.Parallel()
		svc := NewFeedDLQAccessService(&fakeDLQOperatorRepo{
			operator: &model.FeedDLQOperator{
				UserID: 2,
				Role:   model.FeedDLQOperatorRoleAdmin,
				Status: 0,
			},
		})
		bizErr := svc.AuthorizeList(context.Background(), 2)
		if bizErr == nil || bizErr.Code != xerror.CodeForbidden {
			t.Fatalf("unexpected biz error: %+v", bizErr)
		}
	})
}

func TestFeedDLQAccessServiceAuthorizeReplay(t *testing.T) {
	t.Parallel()

	t.Run("allow admin", func(t *testing.T) {
		t.Parallel()
		svc := NewFeedDLQAccessService(&fakeDLQOperatorRepo{
			operator: &model.FeedDLQOperator{
				UserID: 10,
				Role:   model.FeedDLQOperatorRoleAdmin,
				Status: model.FeedDLQOperatorStatusActive,
				Remark: "admin",
			},
		})
		if bizErr := svc.AuthorizeReplay(context.Background(), 10); bizErr != nil {
			t.Fatalf("unexpected biz error: %v", bizErr)
		}
	})

	t.Run("deny operator for replay", func(t *testing.T) {
		t.Parallel()
		svc := NewFeedDLQAccessService(&fakeDLQOperatorRepo{
			operator: &model.FeedDLQOperator{
				UserID: 11,
				Role:   model.FeedDLQOperatorRoleOperator,
				Status: model.FeedDLQOperatorStatusActive,
			},
		})
		bizErr := svc.AuthorizeReplay(context.Background(), 11)
		if bizErr == nil || bizErr.Code != xerror.CodeForbidden {
			t.Fatalf("unexpected biz error: %+v", bizErr)
		}
	})

	t.Run("repo error", func(t *testing.T) {
		t.Parallel()
		svc := NewFeedDLQAccessService(&fakeDLQOperatorRepo{
			err: errors.New("db error"),
		})
		bizErr := svc.AuthorizeReplay(context.Background(), 11)
		if bizErr == nil || bizErr.Code != xerror.CodeInternal {
			t.Fatalf("unexpected biz error: %+v", bizErr)
		}
	})
}
