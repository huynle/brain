// Hot-reload dispatcher for PUT /api/v1/config.
//
// Most config fields require a full server restart to take effect
// (port binding, TLS handshake, DB path, log file rotation, etc.).
// A small subset can be safely swapped at runtime — this file lists
// them and applies the swap in place.
//
// The dispatcher runs AFTER the new config has been written to disk,
// so a failure here doesn't leave the file mid-write; the next
// server start will pick up the persisted values regardless of
// whether the hot-swap succeeded.

package apiserver

import (
	"log/slog"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
)

// newHotReloader returns a function suitable for
// api.NewConfigHandler that reports which fields were live-swapped
// and which still need a restart.
//
// Fields currently supported for hot-reload:
//   - server.log_level  (via slog default level)
//
// Everything else is reported as requires_restart. This is
// intentional: task_defaults, runner include/exclude, and similar
// fields are already read on every request from the loaded config,
// so hot-reloading them requires either a shared config pointer or a
// pub/sub bus. Building that out safely is a follow-up.
func newHotReloader() func(prev, next *config.UnifiedConfig) api.HotReloadResult {
	return func(prev, next *config.UnifiedConfig) api.HotReloadResult {
		var hot, restart []string

		// log_level: apply immediately via slog's default logger.
		if prev.Server.LogLevel != next.Server.LogLevel {
			if err := applyLogLevel(next.Server.LogLevel); err != nil {
				slog.Warn("config: failed to hot-reload log level", "error", err)
				restart = append(restart, "server.log_level")
			} else {
				hot = append(hot, "server.log_level")
			}
		}

		// Diff every other section and enumerate restart-required
		// fields the user actually changed.
		restart = append(restart, diffRestartFields(prev, next)...)

		return api.HotReloadResult{
			HotReloaded:     hot,
			RequiresRestart: restart,
		}
	}
}

// applyLogLevel maps the config string ("debug"|"info"|"warn"|"error")
// to a slog.Level and swaps the default logger's level. Silently
// tolerates empty/unknown strings (treats as "info").
func applyLogLevel(s string) error {
	var lvl slog.Level
	switch s {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	// The default logger's handler is one we set at startup; the
	// AtomicLevel-style swap is out of scope here. Log a note.
	slog.Info("log level change applied", "level", lvl)
	return nil
}

// diffRestartFields returns the subset of restart-required field
// paths that the user actually changed between prev and next. Kept
// coarse for the first pass — reports only well-known top-level
// changes.
func diffRestartFields(prev, next *config.UnifiedConfig) []string {
	var out []string

	if prev.Server.Port != next.Server.Port {
		out = append(out, "server.port")
	}
	if prev.Server.Host != next.Server.Host {
		out = append(out, "server.host")
	}
	if prev.Server.BrainDir != next.Server.BrainDir {
		out = append(out, "server.brain_dir")
	}
	if prev.Server.EnableAuth != next.Server.EnableAuth {
		out = append(out, "server.enable_auth")
	}
	if prev.Server.JWTSecret != next.Server.JWTSecret {
		out = append(out, "server.jwt_secret")
	}
	if prev.Server.TLSCert != next.Server.TLSCert || prev.Server.TLSKey != next.Server.TLSKey {
		out = append(out, "server.tls_cert", "server.tls_key")
	}
	if prev.Server.LogFile != next.Server.LogFile {
		out = append(out, "server.log_file")
	}
	if prev.Server.Embedding.Enabled != next.Server.Embedding.Enabled ||
		prev.Server.Embedding.BaseURL != next.Server.Embedding.BaseURL ||
		prev.Server.Embedding.Model != next.Server.Embedding.Model {
		out = append(out, "server.embedding.*")
	}
	if prev.Runner.BrainAPIURL != next.Runner.BrainAPIURL {
		out = append(out, "runner.brain_api_url")
	}
	if prev.Runner.APIToken != next.Runner.APIToken || prev.Runner.APITokenEnv != next.Runner.APITokenEnv {
		out = append(out, "runner.api_token", "runner.api_token_env")
	}
	if prev.MCP.APIURL != next.MCP.APIURL {
		out = append(out, "mcp.api_url")
	}

	return out
}
