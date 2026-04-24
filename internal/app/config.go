package app

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config contains the application configuration loaded from yaml.
type Config struct {
	App   AppConfig   `yaml:"app"`
	MySQL MySQLConfig `yaml:"mysql"`
	Redis RedisConfig `yaml:"redis"`
	JWT   JWTConfig   `yaml:"jwt"`
	Feed  FeedConfig  `yaml:"feed"`
}

type AppConfig struct {
	Name string `yaml:"name"`
	Env  string `yaml:"env"`
	Port int    `yaml:"port"`
}

type MySQLConfig struct {
	DSN          string `yaml:"dsn"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type JWTConfig struct {
	Secret      string `yaml:"secret"`
	ExpireHours int    `yaml:"expire_hours"`
}

type FeedConfig struct {
	Hybrid FeedHybridConfig `yaml:"hybrid"`
	Inbox  FeedInboxConfig  `yaml:"inbox"`
}

type FeedHybridConfig struct {
	// PushFollowerThreshold controls push/pull split:
	// follower_count <= threshold => push_and_pull; otherwise => pull_only.
	// 0 means pull_only for all authors.
	PushFollowerThreshold int `yaml:"push_follower_threshold"`
}

type FeedInboxConfig struct {
	Enabled  bool  `yaml:"enabled"`
	MaxItems int64 `yaml:"max_items"`
}

// LoadConfig reads a yaml config file into memory.
func LoadConfig(path string) (*Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config %s: %w", path, err)
	}

	return &cfg, nil
}
