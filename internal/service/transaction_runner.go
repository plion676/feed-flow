package service

import (
	"context"

	"gorm.io/gorm"
)

type transactionRunner interface {
	InTx(ctx context.Context, fn func(tx *gorm.DB) error) error
}

type gormTransactionRunner struct {
	db *gorm.DB
}

func NewGormTransactionRunner(db *gorm.DB) transactionRunner {
	if db == nil {
		return nil
	}
	return &gormTransactionRunner{db: db}
}

func (r *gormTransactionRunner) InTx(ctx context.Context, fn func(tx *gorm.DB) error) error {
	if r == nil || r.db == nil {
		return gorm.ErrInvalidDB
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(tx)
	})
}
