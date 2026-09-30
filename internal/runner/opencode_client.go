package runner

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// opencode_client.go — v2 local OpenCode API client helper.
//
// OpenCode v2 (>=2.0) made two breaking changes the runner must speak:
//   1. Every server route is under "/api" and returns JSON (the old bare
//      routes like "/session" now serve the web UI or 405).
//   2. HTTP Basic auth is required: Authorization: Basic base64("opencode:<pw>").
//
// The password is set deterministically by the runner: it generates a strong
// per-serve secret, exports it as OPENCODE_PASSWORD when spawning `opencode
// serve` and `opencode run`, and reuses the same value to authenticate every
// API call. No log-parsing. localOpenCodeClient is the single place that holds
// {port, password} and builds "/api"-prefixed, Basic-authed requests.

// opencodeAPIUser is the fixed username half of the Basic credential; OpenCode
// only checks the password half against OPENCODE_PASSWORD.
const opencodeAPIUser = "opencode"

// generateOpenCodePassword returns a strong random hex secret for one serve.
// Hex keeps it safe to place in an env var and a Basic auth credential.
func generateOpenCodePassword() string {
	b := make([]byte, 24) // 192 bits -> 48 hex chars
	// crypto/rand.Read never returns an error as of Go 1.24 — it panics if the
	// system entropy source fails.
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// localOpenCodeClient targets a local `opencode serve` instance on 127.0.0.1,
// prefixing "/api" and attaching Basic auth built from password.
type localOpenCodeClient struct {
	port     int
	password string
}

// newLocalOpenCodeClient builds a client for the given bound port and the
// per-serve password.
func newLocalOpenCodeClient(port int, password string) localOpenCodeClient {
	return localOpenCodeClient{port: port, password: password}
}

// url turns a bare v2 path (e.g. "/session" or "session") into the full
// "/api"-prefixed URL on this instance.
func (c localOpenCodeClient) url(path string) string {
	path = strings.TrimPrefix(path, "/")
	return fmt.Sprintf("http://127.0.0.1:%d/api/%s", c.port, path)
}

// authHeader returns the Basic auth header value, or "" when no password is
// configured (the caller then sends no Authorization header rather than a
// bogus credential).
func (c localOpenCodeClient) authHeader() string {
	if c.password == "" {
		return ""
	}
	cred := opencodeAPIUser + ":" + c.password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))
}

// newRequest builds an *http.Request to a bare v2 path with the "/api" prefix
// and Basic auth header applied (when a password is set).
func (c localOpenCodeClient) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url(path), body)
	if err != nil {
		return nil, err
	}
	if h := c.authHeader(); h != "" {
		req.Header.Set("Authorization", h)
	}
	return req, nil
}

// injectOpenCodePassword threads the per-serve password into a child env map
// under OPENCODE_PASSWORD so `opencode serve` and `opencode run` authenticate
// with the same value the runner uses for API calls. An empty password sets
// nothing (no bogus credential).
func injectOpenCodePassword(env map[string]string, password string) {
	if password == "" {
		return
	}
	env["OPENCODE_PASSWORD"] = password
}
