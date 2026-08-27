package oauth

import (
	_ "embed"
	"html/template"
)

//go:embed templates/consent.html
var consentHTML string

// consentTmpl is the parsed consent page template.
var consentTmpl = template.Must(template.New("consent").Parse(consentHTML))

// ScopeInfo describes a scope for display on the consent page.
type ScopeInfo struct {
	Name        string
	Description string
}

// ConsentData holds the data for the consent page template.
type ConsentData struct {
	ClientName          string
	ClientID            string
	RedirectURI         string
	State               string
	RawScope            string
	CodeChallenge       string
	CodeChallengeMethod string
	Scopes              []ScopeInfo
	PINRequired         bool
	PasswordRequired    bool
	Username            string
	Error               string
}

// knownScopes maps scope names to human-readable descriptions.
var knownScopes = map[string]string{
	"mcp":       "Full access to the MCP server",
	"mcp:read":  "Read-only access to brain entries and tasks",
	"mcp:write": "Write access to brain entries and tasks",
	"control":   "Remote control of runners: attach to and spawn OpenCode instances (code execution on runner machines)",
}

// DescribeScopes returns ScopeInfo for each scope string.
func DescribeScopes(scopes []string) []ScopeInfo {
	infos := make([]ScopeInfo, 0, len(scopes))
	for _, s := range scopes {
		desc, ok := knownScopes[s]
		if !ok {
			desc = "Unknown scope"
		}
		infos = append(infos, ScopeInfo{Name: s, Description: desc})
	}
	return infos
}
