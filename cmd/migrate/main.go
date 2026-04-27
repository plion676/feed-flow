package main

import (
	"log"
	"os"

	"github.com/plion676/feed-flow/internal/app"
	"github.com/plion676/feed-flow/internal/model"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "configs/config.yaml"
	}

	cfg, err := app.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	db, err := app.NewMySQLDB(cfg)
	if err != nil {
		log.Fatalf("connect mysql failed: %v", err)
	}

	if err := db.AutoMigrate(
		&model.User{},
		&model.UserCount{},
		&model.Post{},
		&model.Follow{},
		&model.FeedDLQOperator{},
	); err != nil {
		log.Fatalf("auto migrate failed: %v", err)
	}

	log.Println("migration completed: users, user_counts, posts, follows, feed_dlq_operators")
}
