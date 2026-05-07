// Package config loads Brain API configuration from config file and
// environment variables.
//
// Priority order (highest wins):
//  1. Environment variables (PORT, HOST, BRAIN_DIR, ENABLE_AUTH, CORS_ORIGIN, etc.)
//  2. Config file (~/.config/brain/config.yaml, server section)
//  3. Built-in defaults
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DataDir is the name of the internal data directory within the brain directory.
// This holds the SQLite database, config, and templates.
// Previously this was ".zk" (inherited from the zk tool); renamed to ".brain-data".
const DataDir = ".brain-data"

// LegacyDataDir is the old data directory name for migration purposes.
const LegacyDataDir = ".zk"

// Build-time variables set via -ldflags.
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// Config holds all Brain API configuration.
type Config struct {
	BrainDir   string
	Port       int
	Host       string
	EnableAuth bool
	CORSOrigin string
	LogLevel   string
	OAuthPIN   string // Optional PIN for consent page protection
	Embeddings EmbeddingConfig
}

// EmbeddingConfig holds optional semantic search embedding settings.
type EmbeddingConfig struct {
	Enabled   bool   `yaml:"enabled"`
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	BaseURL   string `yaml:"base_url"`
	TimeoutMS int    `yaml:"timeout_ms"`
	BatchSize int    `yaml:"batch_size"`
}

const (
	DefaultEmbeddingTimeoutMS = 30000
	DefaultEmbeddingBatchSize = 16
	DefaultOllamaBaseURL      = "http://localhost:11434"
)

// Normalize applies safe defaults without enabling embeddings.
func (e EmbeddingConfig) Normalize() EmbeddingConfig {
	e.Provider = strings.ToLower(strings.TrimSpace(e.Provider))
	e.Model = strings.TrimSpace(e.Model)
	e.BaseURL = strings.TrimRight(strings.TrimSpace(e.BaseURL), "/")
	if e.TimeoutMS == 0 {
		e.TimeoutMS = DefaultEmbeddingTimeoutMS
	}
	if e.BatchSize == 0 {
		e.BatchSize = DefaultEmbeddingBatchSize
	}
	if e.Enabled && e.Provider == "ollama" && e.BaseURL == "" {
		e.BaseURL = DefaultOllamaBaseURL
	}
	return e
}

// Validate returns clear configuration errors when embeddings are enabled.
func (e EmbeddingConfig) Validate() error {
	if !e.Enabled {
		return nil
	}
	if e.Provider == "" {
		return fmt.Errorf("embeddings enabled but provider is not configured")
	}
	if e.Model == "" {
		return fmt.Errorf("embeddings enabled but model is not configured")
	}
	if e.TimeoutMS <= 0 {
		return fmt.Errorf("embeddings timeout_ms must be positive")
	}
	if e.BatchSize <= 0 {
		return fmt.Errorf("embeddings batch_size must be positive")
	}
	return nil
}

// Load reads configuration with the following priority (highest wins):
//  1. Environment variables
//  2. Config file (~/.config/brain/config.yaml)
//  3. Built-in defaults
//
// This ensures all brain clients (brain-api standalone, brain server start,
// brain-mcp, OpenCode plugin) can share the same config file while still
// allowing per-deployment env var overrides (e.g., Docker).
func Load() Config {
	homeDir, _ := os.UserHomeDir()

	// Layer 1: Built-in defaults (aligned with unified.go defaults)
	cfg := Config{
		BrainDir:   filepath.Join(homeDir, ".brain"),
		Port:       3333,
		Host:       "localhost",
		EnableAuth: false,
		CORSOrigin: "*",
		LogLevel:   "info",
		OAuthPIN:   "",
		Embeddings: EmbeddingConfig{
			TimeoutMS: DefaultEmbeddingTimeoutMS,
			BatchSize: DefaultEmbeddingBatchSize,
		},
	}

	// Layer 2: Config file overrides
	ucfg, err := LoadConfig()
	if err == nil {
		s := ucfg.Server
		if s.BrainDir != "" {
			cfg.BrainDir = s.BrainDir
		}
		if s.Port != 0 {
			cfg.Port = s.Port
		}
		if s.Host != "" {
			cfg.Host = s.Host
		}
		if s.EnableAuth {
			cfg.EnableAuth = true
		}
		if s.CORSOrigin != "" {
			cfg.CORSOrigin = s.CORSOrigin
		}
		if s.LogLevel != "" {
			cfg.LogLevel = s.LogLevel
		}
		if s.OAuthPIN != "" {
			cfg.OAuthPIN = s.OAuthPIN
		}
		cfg.Embeddings = s.Embeddings.Normalize()
	}

	// Layer 3: Environment variable overrides (highest priority)
	if v := os.Getenv("BRAIN_DIR"); v != "" {
		cfg.BrainDir = v
	}
	if v := os.Getenv("PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Port = n
		}
	}
	if v := os.Getenv("HOST"); v != "" {
		cfg.Host = v
	}
	if v := os.Getenv("ENABLE_AUTH"); v != "" {
		lower := strings.ToLower(v)
		cfg.EnableAuth = lower == "true" || lower == "1"
	}
	if v := os.Getenv("CORS_ORIGIN"); v != "" {
		cfg.CORSOrigin = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("OAUTH_PIN"); v != "" {
		cfg.OAuthPIN = v
	}
	if v := os.Getenv("BRAIN_EMBEDDINGS_ENABLED"); v != "" {
		lower := strings.ToLower(v)
		cfg.Embeddings.Enabled = lower == "true" || lower == "1"
	}
	if v := os.Getenv("BRAIN_EMBEDDINGS_PROVIDER"); v != "" {
		cfg.Embeddings.Provider = v
	}
	if v := os.Getenv("BRAIN_EMBEDDINGS_MODEL"); v != "" {
		cfg.Embeddings.Model = v
	}
	if v := os.Getenv("BRAIN_EMBEDDINGS_BASE_URL"); v != "" {
		cfg.Embeddings.BaseURL = v
	}
	if v := os.Getenv("BRAIN_EMBEDDINGS_TIMEOUT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Embeddings.TimeoutMS = n
		}
	}
	if v := os.Getenv("BRAIN_EMBEDDINGS_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Embeddings.BatchSize = n
		}
	}
	cfg.Embeddings = cfg.Embeddings.Normalize()

	return cfg
}

// Addr returns the listen address as "host:port".
func (c Config) Addr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}
