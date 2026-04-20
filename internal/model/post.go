package model

import "time"

// Post maps to the posts table and stores user generated content.
type Post struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64     `gorm:"column:user_id;not null;index:idx_user_created_at,priority:1"`
	Content   string    `gorm:"column:content;type:varchar(500);not null"`
	Status    int32     `gorm:"column:status;not null;default:1"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index:idx_user_created_at,priority:2"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (Post) TableName() string {
	return "posts"
}
