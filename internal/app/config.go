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
	Worker FeedWorkerConfig `yaml:"worker"`
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

type FeedWorkerConfig struct {
	// ReclaimMinIdleSeconds controls the minimum idle time for XAUTOCLAIM.
	ReclaimMinIdleSeconds int `yaml:"reclaim_min_idle_seconds"`
	// IdleLogIntervalSeconds controls the interval for idle "waiting stream" logs.
	IdleLogIntervalSeconds int `yaml:"idle_log_interval_seconds"`
	// ReclaimBatchPerLoop controls max XAUTOCLAIM iterations per consume loop.
	ReclaimBatchPerLoop int `yaml:"reclaim_batch_per_loop"`

	// RetryInitialBackoffMS is the initial worker consume retry backoff in ms.
	RetryInitialBackoffMS int `yaml:"retry_initial_backoff_ms"`
	// RetryMaxBackoffMS is the max worker consume retry backoff in ms.
	RetryMaxBackoffMS int `yaml:"retry_max_backoff_ms"`
	// RetryJitterPercent adds +/- jitter percent to the sleep backoff.
	RetryJitterPercent int `yaml:"retry_jitter_percent"`
	// RetryMaxAttempts controls how many handler failures before moving to DLQ.
	RetryMaxAttempts int `yaml:"retry_max_attempts"`
	// RetryCounterTTLSeconds controls TTL for per-stream retry counters.
	RetryCounterTTLSeconds int `yaml:"retry_counter_ttl_seconds"`
	// DLQStreamKey is the Redis stream key for poison/retry-exhausted events.
	DLQStreamKey string `yaml:"dlq_stream_key"`
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
