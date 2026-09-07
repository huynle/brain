package runner

import (
	"log/slog"
	"strings"

	"github.com/huynle/brain-api/internal/gitremote"
)

// advertisedCapabilities never trusts generic configuration for reserved host
// advertisements. Recompute on every registration/heartbeat, including [] to
// revoke old support when a credential disappears or configuration is invalid.
func (tr *TaskRunner) advertisedCapabilities() []string {
	caps := make([]string, 0, len(tr.config.Capabilities))
	for _, capability := range tr.config.Capabilities {
		if !strings.HasPrefix(capability, gitremote.CredentialHostCapabilityPrefix) {
			caps = append(caps, capability)
		}
	}
	hosts, err := tr.config.CredentialedGitHosts()
	if err != nil {
		slog.Warn("git host advertisement withheld: invalid credential configuration", "error", err)
		return caps
	}
	for _, host := range hosts {
		caps = append(caps, gitremote.CredentialHostCapabilityPrefix+host)
	}
	return caps
}
