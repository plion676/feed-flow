package model

import "time"

// Follow maps to the follows table and stores user follow relationships.
type Follow struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID       int64     `gorm:"column:user_id;not null;uniqueIndex:uk_user_target,priority:1;index:idx_target_user_user,priority:2"`
	TargetUserID int64     `gorm:"column:target_user_id;not null;uniqueIndex:uk_user_target,priority:2;index:idx_target_user_user,priority:1"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
}

func (Follow) TableName() string {
	return "follows"
}
