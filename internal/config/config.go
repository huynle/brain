// Package config loads Brain API configuration from config file and
// environment variables.
//
// Priority order (highest wins):
//  1. Environment variables (PORT, HOST, BRAIN_DIR, ENABLE_AUTH, CORS_ORIGIN, etc.)
//  2. Config file (~/.config/brain/config.yaml, server section)
//  3. Built-in defaults
package config

import (
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
	BrainDir        string
	Port            int
	Host            string
	EnableAuth      bool
	CORSOrigin      string
	LogLevel        string
	OAuthPIN        string // Optional PIN for consent page protection
	JWTSecret       string // Optional HMAC secret for HS256 JWT bearer tokens
	TaskDefaults    TaskDefaultsConfig
	FeatureCheckout FeatureCheckoutConfig
	Embedding       EmbeddingConfig
	Attachments     AttachmentConfig

	AttachmentExtraction AttachmentExtractionConfig
	Assistant            AssistantConfig

	// Rate limiting (0 = disabled)
	RateLimitPerMinute int // Per-IP requests per minute (default: 100)
	RateLimitBurst     int // Maximum burst size per IP (default: same as RateLimitPerMinute)
	SSEMaxPerIP        int // Maximum concurrent SSE connections per IP (default: 10)
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

	// Layer 1: Built-in defaults (aligned with unified.go defaults).
	// BRAIN_DIR env var is checked here so it applies even when no config file exists.
	brainDir := os.Getenv("BRAIN_DIR")
	if brainDir == "" {
		brainDir = filepath.Join(homeDir, ".brain")
	}

	cfg := Config{
		BrainDir:   brainDir,
		Port:       3333,
		Host:       "localhost",
		EnableAuth: false,
		CORSOrigin: "*",
		LogLevel:   "info",
		OAuthPIN:   "",
		JWTSecret:  "",
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
		if s.JWTSecret != "" {
			cfg.JWTSecret = s.JWTSecret
		}
		// Thread task defaults from unified config
		cfg.TaskDefaults = s.TaskDefaults
		cfg.FeatureCheckout = s.FeatureCheckout
		cfg.Embedding = s.Embedding
		cfg.Attachments = s.Attachments
		cfg.AttachmentExtraction = s.AttachmentExtraction
		cfg.Assistant = s.Assistant
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
	if v := os.Getenv("JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("BRAIN_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("BRAIN_FEATURE_CHECKOUT_ENABLED"); v != "" {
		lower := strings.ToLower(v)
		cfg.FeatureCheckout.Enabled = lower == "true" || lower == "1" || lower == "yes"
	}
	// Rate limiting env var overrides
	if v := os.Getenv("RATE_LIMIT_PER_MINUTE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimitPerMinute = n
		}
	}
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RateLimitBurst = n
		}
	}
	if v := os.Getenv("SSE_MAX_PER_IP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.SSEMaxPerIP = n
		}
	}

	// Task defaults env var overrides
	if v := os.Getenv("BRAIN_DEFAULT_AGENT"); v != "" {
		cfg.TaskDefaults.Agent = v
	}
	if v := os.Getenv("BRAIN_DEFAULT_MODEL"); v != "" {
		cfg.TaskDefaults.Model = v
	}

	return cfg
}

// Addr returns the listen address as "host:port".
func (c Config) Addr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}
