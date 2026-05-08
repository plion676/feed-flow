package service

import "context"

type feedOutboxRepository interface {
	AddPostToOutbox(ctx context.Context, authorUserID int64, postID int64) error
	RemovePostFromOutbox(ctx context.Context, authorUserID int64, postID int64) error
	TrimOutbox(ctx context.Context, authorUserID int64, maxItems int64) error
}
