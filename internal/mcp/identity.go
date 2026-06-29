package mcp

import (
	"path/filepath"

	"github.com/huynle/brain-api/internal/identity"
)

// mcpClientIDFileName is the base name (under the brain config dir) of the
// per-install client id file used by the MCP stdio server. Older OpenCode
// installs may still have an opencode_client_id file from the deprecated
// brain.ts plugin; that file is no longer read or written here.
const mcpClientIDFileName = "mcp_client_id"

// LoadOrCreateMCPClientID returns the MCP-specific client id for this
// install, creating it on first call. The id is persisted under the brain
// config dir (e.g. ~/.config/brain/mcp_client_id) and is formatted as
// "mcp-<8 hex>". When the config dir cannot be resolved (no HOME,
// no XDG_CONFIG_HOME), a process-ephemeral id is returned so callers always
// see a non-empty value, matching the runner's degraded-mode behavior.
func LoadOrCreateMCPClientID() string {
	dir := identity.ConfigDir()
	if dir == "" {
		// LoadOrCreateID with an empty path falls back to an ephemeral id
		// without persisting. We forward that signal explicitly here.
		return identity.LoadOrCreateID("", "mcp-")
	}
	return identity.LoadOrCreateID(filepath.Join(dir, mcpClientIDFileName), "mcp-")
}
