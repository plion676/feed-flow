package model

import "time"

const (
	FeedEventOutboxStatusPending = 0
	FeedEventOutboxStatusSent    = 1
)

const (
	FeedEventOutboxTypePostCreated = "post_created"
	FeedEventOutboxTypePostDeleted = "post_deleted"
)

// FeedEventOutbox stores pending domain events before they are relayed to Redis Stream.
type FeedEventOutbox struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement"`
	EventType     string     `gorm:"column:event_type;type:varchar(64);not null;index:idx_status_next_retry_id,priority:4"`
	AggregateType string     `gorm:"column:aggregate_type;type:varchar(64);not null"`
	AggregateID   int64      `gorm:"column:aggregate_id;not null"`
	AuthorUserID  int64      `gorm:"column:author_user_id;not null"`
	PostID        int64      `gorm:"column:post_id;not null;default:0"`
	Payload       string     `gorm:"column:payload;type:json;not null"`
	Status        int32      `gorm:"column:status;not null;default:0;index:idx_status_next_retry_id,priority:1"`
	RetryCount    int32      `gorm:"column:retry_count;not null;default:0"`
	NextRetryAt   time.Time  `gorm:"column:next_retry_at;not null;index:idx_status_next_retry_id,priority:2"`
	SentAt        *time.Time `gorm:"column:sent_at"`
	LastError     string     `gorm:"column:last_error;type:varchar(255);default:''"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime;index:idx_status_next_retry_id,priority:3"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime"`
}

func (FeedEventOutbox) TableName() string {
	return "feed_event_outbox"
}
