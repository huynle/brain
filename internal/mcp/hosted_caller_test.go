package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// stubBrainAPI records every request the hosted MCP server makes against the
// Brain API and answers with a generic entry body.
type stubBrainAPI struct {
	mu       sync.Mutex
	requests []string
	saveBody map[string]any
}

func newStubBrainAPI(t *testing.T) (*stubBrainAPI, *httptest.Server) {
	t.Helper()
	stub := &stubBrainAPI{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.requests = append(stub.requests, r.Method+" "+r.URL.RequestURI())
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/entries") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			stub.saveBody = body
		}
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "abc12345", "path": "projects/demo/task/abc12345.md", "title": "Task", "type": "task", "status": "draft",
		})
	}))
	t.Cleanup(srv.Close)
	return stub, srv
}

func (s *stubBrainAPI) lastSave() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveBody
}

func (s *stubBrainAPI) allRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// hostedMCP serves the real HTTP handler on a real listener.
func hostedMCP(t *testing.T, apiURL string) string {
	t.Helper()
	srv := httptest.NewServer(NewHTTPHandler(NewAPIClient(apiURL)))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp"
}

type toolResult struct {
	Text    string
	IsError bool
}

func callHostedTool(t *testing.T, mcpURL string, headers map[string]string, name string, args map[string]any) toolResult {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	req, err := http.NewRequest(http.MethodPost, mcpURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /mcp status = %d, body = %s", resp.StatusCode, raw)
	}
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *JSONRPCError `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpc); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, raw)
	}
	if rpc.Error != nil {
		t.Fatalf("JSON-RPC error: %+v", rpc.Error)
	}
	var text string
	if len(rpc.Result.Content) > 0 {
		text = rpc.Result.Content[0].Text
	}
	return toolResult{Text: text, IsError: rpc.Result.IsError}
}

func validCallerHeaders() map[string]string {
	return map[string]string{
		HeaderBrainHostID:   "machine_cafebabe",
		HeaderBrainClientID: "opencode-laptop",
		HeaderBrainWorkdir:  "/Users/huy/projects/demo/.worktrees/feat-x",
		HeaderBrainHome:     "/Users/huy",
	}
}

func TestHostedMCP_CallerHeadersStampTaskOrigin(t *testing.T) {
	stub, api := newStubBrainAPI(t)
	mcpURL := hostedMCP(t, api.URL)

	res := callHostedTool(t, mcpURL, validCallerHeaders(), "save", map[string]any{
		"type": "task", "title": "T", "content": "x",
	})
	if res.IsError {
		t.Fatalf("save failed: %s", res.Text)
	}
	body := stub.lastSave()
	want := map[string]string{
		"origin_machine_id": "machine_cafebabe",
		"origin_client_id":  "opencode-laptop",
		"origin_path":       "/Users/huy/projects/demo/.worktrees/feat-x",
		"project":           "demo",
		"workdir":           "projects/demo",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %q", k, body[k], v)
		}
	}
}

func TestHostedMCP_CallerHeadersAllowLocalAffinity(t *testing.T) {
	stub, api := newStubBrainAPI(t)
	mcpURL := hostedMCP(t, api.URL)

	for _, v := range []string{types.MachineAffinityLocal, types.MachineAffinityPreferred} {
		res := callHostedTool(t, mcpURL, validCallerHeaders(), "save", map[string]any{
			"type": "task", "title": "Pinned", "content": "x", "machine_affinity": v,
		})
		if res.IsError {
			t.Fatalf("machine_affinity=%s rejected with caller headers: %s", v, res.Text)
		}
		if got := stub.lastSave()["machine_affinity"]; got != v {
			t.Errorf("machine_affinity = %v, want %s", got, v)
		}
	}
}

func TestHostedMCP_MissingHeadersKeepCurrentBehavior(t *testing.T) {
	stub, api := newStubBrainAPI(t)
	mcpURL := hostedMCP(t, api.URL)

	res := callHostedTool(t, mcpURL, nil, "save", map[string]any{
		"type": "task", "title": "T", "content": "x", "project": "explicit",
	})
	if res.IsError {
		t.Fatalf("save failed: %s", res.Text)
	}
	body := stub.lastSave()
	for _, key := range []string{"origin_machine_id", "origin_client_id", "origin_path"} {
		if v, ok := body[key]; ok && v != nil && v != "" {
			t.Errorf("%s = %v without caller headers, want unset", key, v)
		}
	}
	if body["project"] != "explicit" {
		t.Errorf("project = %v, want explicit", body["project"])
	}

	res = callHostedTool(t, mcpURL, nil, "save", map[string]any{
		"type": "task", "title": "T", "content": "x", "project": "explicit",
		"machine_affinity": types.MachineAffinityLocal,
	})
	if !res.IsError {
		t.Fatal("machine_affinity=local accepted without a host-id header; the task could never run")
	}
	if !strings.Contains(res.Text, HeaderBrainHostID) {
		t.Errorf("error should name the %s header so the user can fix their config: %s", HeaderBrainHostID, res.Text)
	}
}

func TestHostedMCP_MalformedOrOversizedHeadersAreIgnored(t *testing.T) {
	cases := map[string]map[string]string{
		"oversized host id":   {HeaderBrainHostID: "machine_" + strings.Repeat("a", 300)},
		"host id with spaces": {HeaderBrainHostID: "machine cafe"},
		"unsubstituted var":   {HeaderBrainHostID: "{env:BRAIN_HOST_ID}"},
		"client id slash":     {HeaderBrainClientID: "../../etc"},
		"relative workdir":    {HeaderBrainWorkdir: "projects/demo"},
		"dotdot workdir":      {HeaderBrainWorkdir: "/Users/huy/../root/demo"},
		"oversized workdir":   {HeaderBrainWorkdir: "/" + strings.Repeat("d", 5000)},
		"relative home":       {HeaderBrainHome: "~"},
	}
	for name, headers := range cases {
		t.Run(name, func(t *testing.T) {
			stub, api := newStubBrainAPI(t)
			mcpURL := hostedMCP(t, api.URL)
			res := callHostedTool(t, mcpURL, headers, "save", map[string]any{
				"type": "task", "title": "T", "content": "x", "project": "explicit",
			})
			if res.IsError {
				t.Fatalf("a bad routing hint must not break the tool: %s", res.Text)
			}
			body := stub.lastSave()
			for _, key := range []string{"origin_machine_id", "origin_client_id", "origin_path"} {
				if v, ok := body[key]; ok && v != nil && v != "" {
					t.Errorf("%s = %v from a malformed header, want unset", key, v)
				}
			}

			ctxRes := callHostedTool(t, mcpURL, headers, "context_get", map[string]any{})
			for h := range headers {
				if !strings.Contains(ctxRes.Text, h) || !strings.Contains(ctxRes.Text, "ignored") {
					t.Errorf("context_get should report that %s was ignored:\n%s", h, ctxRes.Text)
				}
			}
		})
	}
}

func TestParseCallerHeaders_RejectsControlCharacters(t *testing.T) {
	h := http.Header{}
	h.Set(HeaderBrainWorkdir, "/Users/huy/demo\x00evil")
	h.Set(HeaderBrainHostID, "machine_ok")
	c := ParseCallerHeaders(h)
	if c == nil {
		t.Fatal("expected caller context for the valid host id")
	}
	if c.Workdir != "" {
		t.Errorf("Workdir = %q, want rejected", c.Workdir)
	}
	if c.HostID != "machine_ok" {
		t.Errorf("HostID = %q, want machine_ok", c.HostID)
	}
	if len(c.Rejected) != 1 || c.Rejected[0] != HeaderBrainWorkdir {
		t.Errorf("Rejected = %v, want [%s]", c.Rejected, HeaderBrainWorkdir)
	}
	if ParseCallerHeaders(http.Header{}) != nil {
		t.Error("no headers must yield nil caller context")
	}
}

func TestCallerProjectDetection(t *testing.T) {
	cases := []struct {
		workdir, home, project, workdirRel string
	}{
		{"/Users/huy/projects/brain-api", "/Users/huy", "brain-api", "projects/brain-api"},
		{"/Users/huy/projects/brain-api/.worktrees/hosted-mcp-only", "/Users/huy", "brain-api", "projects/brain-api"},
		{"/Users/huy/projects/brain-api/.claude/worktrees/x-1", "/Users/huy", "brain-api", "projects/brain-api"},
		{"/srv/repos/demo", "", "demo", "/srv/repos/demo"},
		{"/Users/huy", "/Users/huy", "", ""},
		{"/", "", "", "/"},
		{"/Users/huy2/projects/x", "/Users/huy", "x", "/Users/huy2/projects/x"},
		{"/Users/huy/My Project!", "/Users/huy", "", "My Project!"},
	}
	for _, tc := range cases {
		c := &CallerContext{Workdir: tc.workdir, Home: tc.home}
		ec := c.ExecutionContext()
		if ec.ProjectID != tc.project {
			t.Errorf("workdir %q: project = %q, want %q", tc.workdir, ec.ProjectID, tc.project)
		}
		if ec.Workdir != tc.workdirRel {
			t.Errorf("workdir %q: Workdir = %q, want %q", tc.workdir, ec.Workdir, tc.workdirRel)
		}
		if ec.AbsPath != tc.workdir {
			t.Errorf("workdir %q: AbsPath = %q", tc.workdir, ec.AbsPath)
		}
	}
}

func TestHostedMCP_ProjectDetectedFromWorkdirHeader(t *testing.T) {
	stub, api := newStubBrainAPI(t)
	mcpURL := hostedMCP(t, api.URL)

	callHostedTool(t, mcpURL, validCallerHeaders(), "tasks", map[string]any{})
	if !containsRequest(stub.allRequests(), "/tasks/demo") {
		t.Errorf("tasks did not target the header-detected project: %v", stub.allRequests())
	}

	// An explicit project always wins over the header.
	callHostedTool(t, mcpURL, validCallerHeaders(), "tasks", map[string]any{"project": "other"})
	if !containsRequest(stub.allRequests(), "/tasks/other") {
		t.Errorf("explicit project was overridden by the header: %v", stub.allRequests())
	}
}

func TestHostedMCP_ToolOutputUnchangedByHeaders(t *testing.T) {
	_, api := newStubBrainAPI(t)
	mcpURL := hostedMCP(t, api.URL)

	explicit := callHostedTool(t, mcpURL, nil, "tasks", map[string]any{"project": "demo"})
	detected := callHostedTool(t, mcpURL, validCallerHeaders(), "tasks", map[string]any{})
	if explicit != detected {
		t.Errorf("tool output differs with caller headers:\nexplicit: %+v\ndetected: %+v", explicit, detected)
	}
}

func TestHostedMCP_LocalPathArgsRejected(t *testing.T) {
	_, api := newStubBrainAPI(t)
	mcpURL := hostedMCP(t, api.URL)

	cases := []struct {
		tool string
		args map[string]any
		arg  string
	}{
		{"attachment_upload", map[string]any{"project": "demo", "file_path": "/tmp/x.png"}, "file_path"},
		{"attachment_download", map[string]any{"project": "demo", "attachment_id": "a1", "output_path": "/tmp/x.png"}, "output_path"},
		{"plan_discover_docs", map[string]any{"additional_dirs": []any{"docs"}}, "additional_dirs"},
	}
	for _, tc := range cases {
		res := callHostedTool(t, mcpURL, validCallerHeaders(), tc.tool, tc.args)
		if !res.IsError {
			t.Errorf("%s accepted %q; local paths are not readable by the hosted server", tc.tool, tc.arg)
			continue
		}
		if !strings.Contains(res.Text, tc.arg) {
			t.Errorf("%s error should name %q: %s", tc.tool, tc.arg, res.Text)
		}
	}
}

func TestHostedMCP_ToolSchemasHaveNoLocalPathArgs(t *testing.T) {
	s := NewServer()
	RegisterBrainTools(s, NewAPIClient("http://127.0.0.1"))
	RegisterPlanningTools(s, NewAPIClient("http://127.0.0.1"))
	for tool, arg := range map[string]string{
		"attachment_upload":   "file_path",
		"attachment_download": "output_path",
		"plan_discover_docs":  "additional_dirs",
	} {
		if _, ok := s.tools[tool].tool.InputSchema.Properties[arg]; ok {
			t.Errorf("%s still advertises %q", tool, arg)
		}
	}
	if _, ok := s.tools["plan_discover_docs"].tool.InputSchema.Properties["doc_paths"]; !ok {
		t.Error("plan_discover_docs should accept agent-supplied doc_paths")
	}
}

func TestHostedMCP_PlanDiscoverDocsUsesAgentSuppliedPaths(t *testing.T) {
	_, api := newStubBrainAPI(t)
	mcpURL := hostedMCP(t, api.URL)

	res := callHostedTool(t, mcpURL, validCallerHeaders(), "plan_discover_docs", map[string]any{
		"doc_paths": []any{"docs/prd/checkout.md", "docs/architecture.md", "README.md"},
	})
	if res.IsError {
		t.Fatalf("plan_discover_docs failed: %s", res.Text)
	}
	prd := strings.Index(res.Text, "### PRD Documents")
	arch := strings.Index(res.Text, "### Architecture Documents")
	if prd < 0 || arch < 0 {
		t.Fatalf("unexpected output:\n%s", res.Text)
	}
	if !strings.Contains(res.Text[prd:arch], "docs/prd/checkout.md") {
		t.Errorf("PRD not categorized:\n%s", res.Text)
	}
	if !strings.Contains(res.Text[arch:], "docs/architecture.md") {
		t.Errorf("architecture doc not categorized:\n%s", res.Text)
	}
	if strings.Contains(res.Text, "README.md") {
		t.Errorf("uncategorized doc listed:\n%s", res.Text)
	}
}

func containsRequest(reqs []string, fragment string) bool {
	for _, r := range reqs {
		if strings.Contains(r, fragment) {
			return true
		}
	}
	return false
}
