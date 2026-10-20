package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/oauth"
	"github.com/huynle/brain-api/internal/tenant"
)

// Temporary write:* bridge (decision nezg1ru5). write:* admits content reads
// (every route read:* reaches) plus exactly the content-write route class
// that P6.6 (commit e48ffe22, oauthContentWriteRoutes) defined for OAuth
// mcp:write. Fold into the P6.6 route classes when the multi-tenant line
// merges.

// writeBridgeContentWriteRoutes: reached by write:*, refused to read:*.
// Identical to e48ffe22's oauthContentWriteRoutes (40 routes).
var writeBridgeContentWriteRoutes = []string{
	"DELETE /api/v1/attachments/{attachmentID}",
	"DELETE /api/v1/entries/*",
	"DELETE /api/v1/entries/{id}/attachments/{attachmentID}",
	"DELETE /api/v1/reminders/{reminderId}",
	"DELETE /api/v1/tasks/{projectId}/features/{featureId}/run",
	"GET /api/v1/bulk-jobs",
	"GET /api/v1/bulk-jobs/{jobID}",
	"GET /api/v1/bulk-jobs/{jobID}/items",
	"GET /api/v1/sync/devices",
	"GET /api/v1/sync/devices/{deviceID}/operations/{operationID}/diff",
	"PATCH /api/v1/entries/*",
	"PATCH /api/v1/reminders/{reminderId}",
	"POST /api/v1/attachments",
	"POST /api/v1/attachments/{attachmentID}/extract",
	"POST /api/v1/bulk-jobs",
	"POST /api/v1/bulk-jobs/{jobID}/control",
	"POST /api/v1/entries",
	"POST /api/v1/entries/*",
	"POST /api/v1/entries/bulk-delete",
	"POST /api/v1/entries/bulk-update",
	"POST /api/v1/entries/{id}/attachments",
	"POST /api/v1/link",
	"POST /api/v1/reminders",
	"POST /api/v1/reminders/{reminderId}/ack",
	"POST /api/v1/reminders/{reminderId}/fire",
	"POST /api/v1/reminders/{reminderId}/snooze",
	"POST /api/v1/sync/devices/{deviceID}/operations/{operationID}/reconcile",
	"POST /api/v1/sync/devices/{deviceID}/report",
	"POST /api/v1/sync/entries",
	"POST /api/v1/tasks/{projectId}/features/{featureId}/resume",
	"POST /api/v1/tasks/{projectId}/features/{featureId}/resume-with-context",
	"POST /api/v1/tasks/{projectId}/features/{featureId}/run",
	"POST /api/v1/tasks/{projectId}/run",
	"POST /api/v1/tasks/{projectId}/{taskId}/delivery",
	"POST /api/v1/tasks/{projectId}/{taskId}/dispatch",
	"POST /api/v1/tasks/{projectId}/{taskId}/resume",
	"POST /api/v1/tasks/{projectId}/{taskId}/resume-with-context",
	"POST /api/v1/tasks/{projectId}/{taskId}/run",
	"POST /api/v1/tasks/{projectId}/{taskId}/trigger",
}

// writeBridgeRefusedReadRoutes: reached by read:* on main only because they
// carry no RequireScope at all, yet e48ffe22 classifies them as
// administration (webhooks configure outbound effects; event ingest is a
// runner act). write:* is refused there; read:* is deliberately unchanged.
var writeBridgeRefusedReadRoutes = []string{
	"DELETE /api/v1/webhooks/{id}",
	"GET /api/v1/webhooks",
	"GET /api/v1/webhooks/{id}",
	"GET /api/v1/webhooks/{id}/deliveries",
	"PATCH /api/v1/webhooks/{id}",
	"POST /api/v1/events",
	"POST /api/v1/webhooks",
	"POST /api/v1/webhooks/{id}/test",
}

// writeBridgePublicRoutes need no credential and are not classified.
var writeBridgePublicRoutes = map[string]bool{
	"GET /api/v1/health":            true,
	"POST /api/v1/tokens/bootstrap": true,
	"POST /api/v1/auth/login":       true,
	"POST /api/v1/auth/refresh":     true,
	"POST /api/v1/auth/logout":      true,
}

type writeBridgeServer struct {
	h                    http.Handler
	url                  string
	admin, read, write   string
	runner, controlToken string
}

// newWriteBridgeServer runs the REAL buildHTTPHandler composition with auth
// on, behind a real loopback listener (the hosted MCP handler calls the REST
// API back on opts.Port with the caller's credential).
func newWriteBridgeServer(t *testing.T) *writeBridgeServer {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithCancel(context.Background())
	h, _, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: "127.0.0.1", Port: port, BrainDir: filepath.Join(t.TempDir(), "brain"), EnableAuth: true})
	if err != nil {
		cancel()
		_ = ln.Close()
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	t.Cleanup(func() {
		_ = srv.Close()
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("serve: %v", err)
		}
		cancel()
		cleanup()
	})
	s := &writeBridgeServer{h: h, url: fmt.Sprintf("http://127.0.0.1:%d", port)}
	w := s.do(http.MethodPost, "/api/v1/tokens/bootstrap", "", `{"name":"owner"}`)
	var boot struct{ Token string }
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &boot) != nil || boot.Token == "" {
		t.Fatalf("bootstrap: %d %s", w.Code, w.Body.String())
	}
	s.admin = boot.Token
	s.read = s.token(t, "reader", "read:*")
	s.write = s.token(t, "task-agent", api.ScopeWriteBridge)
	s.runner = s.token(t, "runner", "runner:*")
	s.controlToken = s.token(t, "control", api.ScopeControl)
	return s
}

func (s *writeBridgeServer) token(t *testing.T, name, scope string) string {
	t.Helper()
	w := s.do(http.MethodPost, "/api/v1/tokens", s.admin, fmt.Sprintf(`{"name":%q,"scope":%q}`, name, scope))
	var tok struct{ Token, Scope string }
	if w.Code != http.StatusCreated || json.Unmarshal(w.Body.Bytes(), &tok) != nil || tok.Scope != scope {
		t.Fatalf("create %s token: %d %s", scope, w.Code, w.Body.String())
	}
	return tok.Token
}

func (s *writeBridgeServer) doCtx(ctx context.Context, method, path, bearer, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	s.h.ServeHTTP(w, r)
	return w
}

func (s *writeBridgeServer) do(method, path, bearer, body string) *httptest.ResponseRecorder {
	return s.doCtx(context.Background(), method, path, bearer, body)
}

// scopeRefused reports whether RequireScope (or the write:* refusal) turned
// the credential away; any other outcome means the route was reached.
func (s *writeBridgeServer) scopeRefused(t *testing.T, route, bearer string) bool {
	t.Helper()
	method, pattern, _ := strings.Cut(route, " ")
	// A reached stream handler holds the request open: bound it.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	w := s.doCtx(ctx, method, writeBridgePath(pattern), bearer, "{}")
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("%s: credential rejected (%s)", route, w.Body.String())
	}
	return w.Code == http.StatusForbidden && strings.Contains(w.Body.String(), "Token scope")
}

func writeBridgePath(pattern string) string {
	parts := strings.Split(pattern, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, "{") || p == "*" {
			parts[i] = "inv"
		}
	}
	return strings.Join(parts, "/")
}

// writeBridgeRoutePatterns walks api.NewRouter wired with a REAL tenant graph
// handler, so handler-gated routes (bulk jobs, sync, supervision, ...) exist.
func writeBridgeRoutePatterns(t *testing.T) []string {
	t.Helper()
	root := t.TempDir()
	views, err := openSingleModeStorage(context.Background(), tenant.ModeSingle, filepath.Join(root, "brain.db"), root, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(views.close)
	if _, err := views.roots.ProvisionLocal(context.Background(), root, filepath.Join(root, "attachments")); err != nil {
		t.Fatal(err)
	}
	g, err := newTenantGraph(context.Background(), views.tenant, views.roots, config.Config{BrainDir: root}, graphIdentity{tokens: views.tokens})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.Close)
	router := api.NewRouter(config.Config{}, api.WithHandler(g.handler), api.WithConfigHandler(api.NewConfigHandler("", nil)))
	oauth.RegisterRoutes(router, oauth.NewHandler(nil))
	var out []string
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.ReplaceAll(route, "/*/", "/"), "/")
		if strings.HasPrefix(route, "/api/v1/") && method != http.MethodOptions {
			out = append(out, method+" "+route)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// TestWriteBridgeRouteInventory classifies EVERY authenticated /api/v1 route
// for read:* and write:* tokens on the real composition, and pins the
// difference. A future administration route that becomes write-reachable,
// or a content write that silently drops out, fails here.
func TestWriteBridgeRouteInventory(t *testing.T) {
	s := newWriteBridgeServer(t)
	var writeOnly, readOnly []string
	reached := 0
	for _, route := range writeBridgeRoutePatterns(t) {
		if writeBridgePublicRoutes[route] {
			continue
		}
		readOK, writeOK := !s.scopeRefused(t, route, s.read), !s.scopeRefused(t, route, s.write)
		switch {
		case writeOK && !readOK:
			writeOnly = append(writeOnly, route)
		case readOK && !writeOK:
			readOnly = append(readOnly, route)
		}
		if writeOK {
			reached++
		}
	}
	pin := func(name string, got, want []string) {
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			var b strings.Builder
			for _, r := range got {
				fmt.Fprintf(&b, "\t%q,\n", r)
			}
			t.Errorf("%s changed (review the write:* route class; no auto-rebaseline):\ngot:\n%s", name, b.String())
		}
	}
	pin("writeBridgeContentWriteRoutes", writeOnly, writeBridgeContentWriteRoutes)
	pin("writeBridgeRefusedReadRoutes", readOnly, writeBridgeRefusedReadRoutes)
	t.Logf("write:* reaches %d routes: %d content writes beyond read:*", reached, len(writeOnly))
}

// Administration and control stay refused to write:* (representative set,
// including the routes the bridge must never reach).
func TestWriteBridgeRefusesAdministrationAndControl(t *testing.T) {
	s := newWriteBridgeServer(t)
	for _, route := range []string{
		"POST /api/v1/tokens", "GET /api/v1/tokens", "DELETE /api/v1/tokens/{name}",
		"PUT /api/v1/config",
		"POST /api/v1/embeddings/backfill", "POST /api/v1/attachments/backfill/extraction",
		"POST /api/v1/calendars", "PUT /api/v1/calendars/{name}", "DELETE /api/v1/calendars/{name}",
		"POST /api/v1/webhooks", "POST /api/v1/events",
		"POST /api/v1/supervision/budgets", "POST /api/v1/supervision/operations",
		"POST /api/v1/goals", "POST /api/v1/monitors", "POST /api/v1/automations/run", "POST /api/v1/attention",
		"POST /api/v1/tasks/runner/pause", "POST /api/v1/tasks/runner/automations/pause",
		"PUT /api/v1/projects/{projectId}/placement",
		"DELETE /api/v1/tasks/{projectId}", // stricter than P6.6: project erasure
		"POST /api/v1/tasks/{projectId}/features/{featureId}/checkout",
		"PUT /api/v1/tasks/{projectId}/{taskId}/assignment",
		"POST /api/v1/tasks/{projectId}/{taskId}/claim", "POST /api/v1/tasks/{projectId}/{taskId}/logs",
		"POST /api/v1/runners/register", "PUT /api/v1/runners/{runnerId}/shutdown",
		"POST /api/v1/assistant/chat", "GET /api/v1/push",
		"POST /api/v1/control/runners/{runnerId}/exec",
		"POST /api/v1/control/runners/{runnerId}/instances",
	} {
		if !s.scopeRefused(t, route, s.write) {
			t.Errorf("write:* reached administration/control route %s", route)
		}
	}
	// Positive controls: existing scopes keep their reach.
	if w := s.do(http.MethodGet, "/api/v1/tokens", s.admin, ""); w.Code != http.StatusOK {
		t.Fatalf("admin token list: %d", w.Code)
	}
	if s.scopeRefused(t, "POST /api/v1/tasks/{projectId}/{taskId}/claim", s.runner) {
		t.Fatal("runner:* lost claim")
	}
	if !s.scopeRefused(t, "POST /api/v1/entries", s.runner) || !s.scopeRefused(t, "POST /api/v1/entries", s.read) {
		t.Fatal("runner:*/read:* gained entry writes")
	}
	if !s.scopeRefused(t, "POST /api/v1/entries", s.controlToken) || s.scopeRefused(t, "GET /api/v1/control/runners/{runnerId}/sessions/{sessionId}/tail", s.controlToken) {
		t.Fatal("control:* reach changed")
	}
}

// mcpCall is one hosted MCP tools/call with the caller's credential.
func (s *writeBridgeServer) mcpCall(t *testing.T, bearer, tool string, args map[string]any) (bool, string) {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": tool, "arguments": args})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := s.doCtx(ctx, http.MethodPost, "/mcp", bearer, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":`+string(params)+`}`)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: HTTP %d %s", tool, w.Code, w.Body.String())
	}
	body := w.Body.String()
	return !strings.Contains(body, `"isError":true`), body
}

// The hosted MCP server forwards the caller's credential, so REST scopes
// decide tool reach: save/update work with write:*, administration does not.
func TestWriteBridgeMCPToolReach(t *testing.T) {
	s := newWriteBridgeServer(t)
	ok, body := s.mcpCall(t, s.write, "save", map[string]any{"type": "idea", "title": "Bridge note", "content": "first", "project": "bridge"})
	if !ok {
		t.Fatalf("save with write:*: %.600s", body)
	}
	w := s.do(http.MethodGet, "/api/v1/entries?project=bridge&type=idea", s.write, "")
	var list struct {
		Entries []struct{ Path string } `json:"entries"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Entries) != 1 {
		t.Fatalf("list after save: %d %s", w.Code, w.Body.String())
	}
	path := list.Entries[0].Path
	if ok, body := s.mcpCall(t, s.write, "update", map[string]any{"path": path, "append": "appended-by-bridge"}); !ok {
		t.Fatalf("update with write:*: %.600s", body)
	}
	if w := s.do(http.MethodGet, "/api/v1/entries/"+path, s.write, ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "appended-by-bridge") {
		t.Fatalf("entry after update: %d %.400s", w.Code, w.Body.String())
	}
	// read:* still cannot write through MCP.
	if ok, body := s.mcpCall(t, s.read, "save", map[string]any{"type": "idea", "title": "Nope", "content": "x", "project": "bridge"}); ok || !strings.Contains(body, "insufficient") {
		t.Fatalf("save with read:* not refused by scope: %.600s", body)
	}
	// Administration tools are refused to write:*.
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"runner_pause_all", map[string]any{"confirm": true}},
		{"webhook_create", map[string]any{"name": "w", "url": "https://example.com/hook", "events": []any{"task.created"}}},
		{"project_placement_put", map[string]any{"project": "bridge", "placement": map[string]any{}}},
	} {
		ok, body := s.mcpCall(t, s.write, c.tool, c.args)
		if ok || !strings.Contains(body, `Token scope \"write:*\" insufficient`) {
			t.Errorf("%s with write:* not refused by scope: %.600s", c.tool, body)
		}
	}
}
