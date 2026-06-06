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

// RankingConfig holds settings for the ranking pipeline.
type RankingConfig struct {
	CrossEncoderEnabled bool   `toml:"cross_encoder_enabled"`
	CrossEncoderModel   string `toml:"cross_encoder_model"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Addr: ":8080",
		},
		Vortex: VortexConfig{
			Addr: "http://localhost:8080",
		},
		Proximia: ProximiaConfig{
			Addr: "http://localhost:8080",
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
