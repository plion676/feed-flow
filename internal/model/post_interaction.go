package model

import "time"

const (
	CommentStatusDeleted   int32 = 0
	CommentStatusPublished int32 = 1
)

// PostLike stores a user's like relation to a post.
type PostLike struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	PostID    int64     `gorm:"column:post_id;not null;uniqueIndex:uk_post_like,priority:1;index:idx_post_like_post_id"`
	UserID    int64     `gorm:"column:user_id;not null;uniqueIndex:uk_post_like,priority:2;index:idx_post_like_user_id"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (PostLike) TableName() string {
	return "post_likes"
}

// PostCollect stores a user's collect relation to a post.
type PostCollect struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	PostID    int64     `gorm:"column:post_id;not null;uniqueIndex:uk_post_collect,priority:1;index:idx_post_collect_post_id"`
	UserID    int64     `gorm:"column:user_id;not null;uniqueIndex:uk_post_collect,priority:2;index:idx_post_collect_user_id"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (PostCollect) TableName() string {
	return "post_collects"
}

// PostComment stores comments under posts.
type PostComment struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	PostID    int64     `gorm:"column:post_id;not null;index:idx_post_comment_post_id"`
	UserID    int64     `gorm:"column:user_id;not null;index:idx_post_comment_user_id"`
	Content   string    `gorm:"column:content;type:varchar(300);not null"`
	Status    int32     `gorm:"column:status;not null;default:1"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index:idx_post_comment_created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (PostComment) TableName() string {
	return "post_comments"
}
