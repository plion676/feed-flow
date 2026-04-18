package model

import "time"

// User maps to the users table and stores the core user identity fields.
type User struct {
	ID           int64     `gorm:"column:id;primaryKey;autoIncrement"`
	Username     string    `gorm:"column:username;type:varchar(32);not null;uniqueIndex"`
	PasswordHash string    `gorm:"column:password_hash;type:varchar(255);not null"`
	Nickname     string    `gorm:"column:nickname;type:varchar(32);not null"`
	Avatar       string    `gorm:"column:avatar;type:varchar(255);default:''"`
	Bio          string    `gorm:"column:bio;type:varchar(255);default:''"`
	Status       int32     `gorm:"column:status;not null;default:1"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (User) TableName() string {
	return "users"
}
