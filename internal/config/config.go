// Package config loads Brain API configuration from environment variables.
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
	BrainDir   string
	Port       int
	Host       string
	EnableAuth bool
	CORSOrigin string
	LogLevel   string
	OAuthPIN   string // Optional PIN for consent page protection
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	homeDir, _ := os.UserHomeDir()
	defaultBrainDir := filepath.Join(homeDir, ".brain")

	return Config{
		BrainDir:   getEnv("BRAIN_DIR", defaultBrainDir),
		Port:       getEnvInt("PORT", 3000),
		Host:       getEnv("HOST", "0.0.0.0"),
		EnableAuth: getEnvBool("ENABLE_AUTH", false),
		CORSOrigin: getEnv("CORS_ORIGIN", "*"),
		LogLevel:   getEnv("LOG_LEVEL", "info"),
		OAuthPIN:   getEnv("OAUTH_PIN", ""),
	}
}

// Addr returns the listen address as "host:port".
func (c Config) Addr() string {
	return c.Host + ":" + strconv.Itoa(c.Port)
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultValue
	}
	return n
}

func getEnvBool(key string, defaultValue bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	lower := strings.ToLower(v)
	return lower == "true" || lower == "1"
}
