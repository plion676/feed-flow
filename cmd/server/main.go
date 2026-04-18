package main

import (
	"log"
	"os"

	"github.com/plion676/feed-flow/internal/app"
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

	application := app.New(cfg)
	if err := application.Run(); err != nil {
		log.Fatalf("run app failed: %v", err)
	}
}
