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
	Hybrid   FeedHybridConfig   `yaml:"hybrid"`
	Inbox    FeedInboxConfig    `yaml:"inbox"`
	Outbox   FeedOutboxConfig   `yaml:"outbox"`
	Exposure FeedExposureConfig `yaml:"exposure"`
	Worker   FeedWorkerConfig   `yaml:"worker"`
}

type FeedHybridConfig struct {
	// PushFollowerThreshold controls push/pull split:
	// follower_count <= threshold => push_and_pull; otherwise => pull_only.
	// 0 means pull_only for all authors.
	PushFollowerThreshold int           `yaml:"push_follower_threshold"`
	Mix                   FeedMixConfig `yaml:"mix"`
}

type FeedInboxConfig struct {
	Enabled   bool  `yaml:"enabled"`
	MaxItems  int64 `yaml:"max_items"`
	BatchSize int   `yaml:"batch_size"`
	Workers   int   `yaml:"workers"`
}

type FeedOutboxConfig struct {
	Enabled           bool  `yaml:"enabled"`
	MaxItems          int64 `yaml:"max_items"`
	ReadChunkSize     int   `yaml:"read_chunk_size"`
	MaxAuthorsPerRead int   `yaml:"max_authors_per_read"`
	DBFallbackEnabled bool  `yaml:"db_fallback_enabled"`
}

type FeedMixConfig struct {
	PushRatioNumerator   int  `yaml:"push_ratio_numerator"`
	PushRatioDenominator int  `yaml:"push_ratio_denominator"`
	MinPullItems         *int `yaml:"min_pull_items"`
	MaxConsecutiveAuthor int  `yaml:"max_consecutive_author"`
	AuthorCooldownWindow int  `yaml:"author_cooldown_window"`
	MaxConsecutiveSource *int `yaml:"max_consecutive_source"`
}

type FeedExposureConfig struct {
	Enabled bool `yaml:"enabled"`
	// WindowHours controls how long a returned post stays in the exposure window.
	WindowHours int `yaml:"window_hours"`
	// KeyTTLHours controls Redis key cleanup for inactive users.
	KeyTTLHours int `yaml:"key_ttl_hours"`
	// BatchMultiplier controls how many candidates to over-fetch for exposure backfill.
	BatchMultiplier int `yaml:"batch_multiplier"`
}

type FeedWorkerConfig struct {
	// ReclaimMinIdleSeconds controls the minimum idle time for XAUTOCLAIM.
	ReclaimMinIdleSeconds int `yaml:"reclaim_min_idle_seconds"`
	// IdleLogIntervalSeconds controls the interval for idle "waiting stream" logs.
	IdleLogIntervalSeconds int `yaml:"idle_log_interval_seconds"`
	// ReclaimBatchPerLoop controls max XAUTOCLAIM iterations per consume loop.
	ReclaimBatchPerLoop int `yaml:"reclaim_batch_per_loop"`

	// OutboxRelayBatchSize controls how many pending outbox events are claimed per relay loop.
	OutboxRelayBatchSize int `yaml:"outbox_relay_batch_size"`
	// OutboxRelayIdleSleepMS controls how long relay sleeps when no pending outbox events exist.
	OutboxRelayIdleSleepMS int `yaml:"outbox_relay_idle_sleep_ms"`
	// OutboxRelayInitialBackoffMS controls initial publish retry backoff for a failed outbox event.
	OutboxRelayInitialBackoffMS int `yaml:"outbox_relay_initial_backoff_ms"`
	// OutboxRelayMaxBackoffMS controls max publish retry backoff for a failed outbox event.
	OutboxRelayMaxBackoffMS int `yaml:"outbox_relay_max_backoff_ms"`

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
