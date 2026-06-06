// Package config defines the configuration structure for suwen.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// Config is the root configuration for suwen.
type Config struct {
	Server    ServerConfig    `toml:"server"`
	Vortex    VortexConfig    `toml:"vortex"`
	Proximia  ProximiaConfig  `toml:"proximia"`
	LLM       LLMConfig       `toml:"llm"`
	Retrieval RetrievalConfig `toml:"retrieval"`
	Ranking   RankingConfig   `toml:"ranking"`
	Query     QueryConfig     `toml:"query"`
	Cache     CacheConfig     `toml:"cache"`
	RateLimit RateLimitConfig `toml:"rate_limit"`
	Auth      AuthConfig      `toml:"auth"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Addr string `toml:"addr"`
}

// VortexConfig holds the connection settings for the Vortex search engine.
type VortexConfig struct {
	Addr string `toml:"addr"`
}

// ProximiaConfig holds the connection settings for the Proximia vector engine.
type ProximiaConfig struct {
	Addr string `toml:"addr"`
}

// LLMConfig holds the LLM gateway settings.
type LLMConfig struct {
	Provider   string `toml:"provider"`
	Model      string `toml:"model"`
	Timeout    string `toml:"timeout"`
	ConfigPath string `toml:"config_path"` // Optional: path to llmgate config file
}

// RetrievalConfig holds settings for the hybrid retrieval pipeline.
type RetrievalConfig struct {
	Timeout string `toml:"timeout"`
	RRFK    int    `toml:"rrf_k"`
}

// QueryConfig holds settings for query understanding.
type QueryConfig struct {
	Enabled    bool   `toml:"enabled"`     // Phase 2: enable LLM-powered query understanding
	Model      string `toml:"model"`       // LLM model for query parsing (should be fast/cheap)
	Timeout    string `toml:"timeout"`     // Timeout for query understanding (e.g. "500ms")
}

// RankingConfig holds settings for the ranking pipeline.
type RankingConfig struct {
	CrossEncoderEnabled bool   `toml:"cross_encoder_enabled"`
	CrossEncoderModel   string `toml:"cross_encoder_model"`
	CrossEncoderAddr    string `toml:"cross_encoder_addr"` // HTTP endpoint for Cross-Encoder service
}

// CacheConfig holds settings for query result caching.
type CacheConfig struct {
	Enabled   bool   `toml:"enabled"`    // Enable query cache
	MaxSize   int    `toml:"max_size"`   // Max number of cached entries
	TTL       string `toml:"ttl"`        // Default TTL (e.g. "5m")
}

// RateLimitConfig holds settings for rate limiting.
type RateLimitConfig struct {
	Enabled  bool   `toml:"enabled"`   // Enable rate limiting
	Rate     int    `toml:"rate"`      // Requests per interval
	Interval string `toml:"interval"`  // Interval (e.g. "1m")
	Burst    int    `toml:"burst"`     // Max burst size
}

// AuthConfig holds settings for API authentication.
type AuthConfig struct {
	Enabled bool     `toml:"enabled"` // Enable API key auth
	Keys    []string `toml:"keys"`    // Valid API keys
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Addr: ":8080",
		},
		Vortex: VortexConfig{
			Addr: "http://localhost:9527",
		},
		Proximia: ProximiaConfig{
			Addr: "http://localhost:9876",
		},
		LLM: LLMConfig{
			Provider: "openai",
			Model:    "gpt-4.1-mini",
			Timeout:  "30s",
		},
		Retrieval: RetrievalConfig{
			Timeout: "2s",
			RRFK:    60,
		},
		Ranking: RankingConfig{
			CrossEncoderEnabled: false,
			CrossEncoderModel:   "bge-reranker-v2-m3",
			CrossEncoderAddr:    "http://localhost:9988",
		},
		Query: QueryConfig{
			Enabled: false,
			Model:   "deepseek-v4-flash",
			Timeout: "500ms",
		},
		Cache: CacheConfig{
			Enabled: false,
			MaxSize: 1000,
			TTL:     "5m",
		},
		RateLimit: RateLimitConfig{
			Enabled:  false,
			Rate:     30,
			Interval: "1m",
			Burst:    5,
		},
		Auth: AuthConfig{
			Enabled: false,
			Keys:    nil,
		},
	}
}

// Load reads a TOML config file. Falls back to DefaultConfig if path is empty or file doesn't exist.
func Load(path string) (*Config, error) {
	if path == "" {
		return DefaultConfig(), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// TimeoutDuration parses the timeout string and returns a time.Duration.
func TimeoutDuration(s string) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 30 * time.Second
	}
	return d
}
