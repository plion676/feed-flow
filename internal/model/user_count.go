package model

import "time"

// UserCount stores the counters that will be frequently updated later.
type UserCount struct {
	UserID         int64     `gorm:"column:user_id;primaryKey"`
	FollowingCount int64     `gorm:"column:following_count;not null;default:0"`
	FollowerCount  int64     `gorm:"column:follower_count;not null;default:0"`
	PostCount      int64     `gorm:"column:post_count;not null;default:0"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UserCount) TableName() string {
	return "user_counts"
}
