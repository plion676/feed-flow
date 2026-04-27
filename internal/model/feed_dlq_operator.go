package model

import "time"

const (
	FeedDLQOperatorRoleOperator = "operator"
	FeedDLQOperatorRoleAdmin    = "admin"
	FeedDLQOperatorStatusActive = 1
)

// FeedDLQOperator controls who can operate DLQ endpoints.
type FeedDLQOperator struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    int64     `gorm:"column:user_id;not null;uniqueIndex:uk_user_id"`
	Role      string    `gorm:"column:role;type:varchar(16);not null"`
	Status    int32     `gorm:"column:status;not null;default:1;index:idx_status"`
	CreatedBy int64     `gorm:"column:created_by;not null;default:0"`
	Remark    string    `gorm:"column:remark;type:varchar(255);default:''"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (FeedDLQOperator) TableName() string {
	return "feed_dlq_operators"
}
