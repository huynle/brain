package gitremote

import "strings"

// CredentialHostCapabilityPrefix is reserved for runner-derived advertisements,
// not operator-supplied generic capabilities. Only exact authorities are valid.
const CredentialHostCapabilityPrefix = "git-credential-host:"

// CredentialHosts extracts nonsecret configured support from runner capabilities.
// Malformed advertisements never authorize a host.
func CredentialHosts(capabilities []string) []string {
	hosts := make([]string, 0)
	for _, capability := range capabilities {
		if raw, ok := strings.CutPrefix(capability, CredentialHostCapabilityPrefix); ok {
			if host, err := Authority(raw); err == nil {
				hosts = append(hosts, host)
			}
		}
	}
	return hosts
}
