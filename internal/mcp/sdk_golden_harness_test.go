package mcp_test

// Golden harness for the hosted-MCP-on-SDK migration.
//
// Every migrated tool file has a golden test that drives its tools over real
// HTTP POST /mcp, served by the real in-process Brain API (apiserver.RunServer),
// whose MCP handler calls back into that same server's REST API. The captured
// tool text (success and error) is what agents read, so it must be identical
// before and after a tool is moved from hand-built requests to the public SDK.
//
// Regenerate deliberately with: go test ./internal/mcp -run Golden -update-golden

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/apiserver"
	"github.com/huynle/brain-api/internal/mcp"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite internal/mcp/testdata/sdk_golden/*.golden")

// TestMain makes the whole internal/mcp test binary hermetic: a throwaway
// HOME/XDG tree and an unreachable BRAIN_API_URL, so no test can read the
// developer's config or reach a real Brain (never brain.huynle.com).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "brain-mcp-test-home-")
	if err != nil {
		panic(err)
	}
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(k, "BRAIN_") {
			_ = os.Unsetenv(k)
		}
	}
	for k, v := range map[string]string{
		"HOME":            dir,
		"XDG_CONFIG_HOME": filepath.Join(dir, ".config"),
		"XDG_STATE_HOME":  filepath.Join(dir, ".local", "state"),
		"XDG_DATA_HOME":   filepath.Join(dir, ".local", "share"),
		"BRAIN_API_URL":   "http://127.0.0.1:1",
		"BRAIN_API_TOKEN": "",
	} {
		_ = os.Setenv(k, v)
	}
	code := m.Run()
	stopRealAPI()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

var (
	realAPIOnce sync.Once
	realAPIURL  string
	realAPIErr  error
	realAPIStop func()
)

// realAPI starts (once per test binary) the actual Brain API server on a free
// loopback port with a throwaway brain dir. Its /mcp endpoint is the hosted MCP
// under test; tools call back into the same process over loopback HTTP.
func realAPI(t *testing.T) string {
	t.Helper()
	realAPIOnce.Do(func() {
		realAPIURL, realAPIStop, realAPIErr = launchAPI()
	})
	if realAPIErr != nil {
		t.Fatalf("real api: %v", realAPIErr)
	}
	return realAPIURL
}

// dedicatedAPI starts a private in-process Brain API for one test. Tools with
// server-wide effects (pause-all, runner registry, scheduler state) use it so
// their transcripts cannot see, or leak into, any other test's state.
func dedicatedAPI(t *testing.T) string {
	t.Helper()
	url, stop, err := launchAPI()
	if err != nil {
		t.Fatalf("dedicated api: %v", err)
	}
	t.Cleanup(stop)
	return url
}

// launchAPI runs the actual Brain API server on a free loopback port with a
// throwaway brain dir and waits for it to report healthy.
func launchAPI() (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	brainDir, err := os.MkdirTemp("", "brain-mcp-golden-")
	if err != nil {
		return "", nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- apiserver.RunServer(ctx, apiserver.ServerOptions{
			Host:      "127.0.0.1",
			Port:      port,
			BrainDir:  filepath.Join(brainDir, "brain"),
			LogLevel:  "error",
			LogWriter: io.Discard,
		})
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
		}
		_ = os.RemoveAll(brainDir)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get(url + "/api/v1/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return url, stop, nil
			}
		}
		select {
		case err := <-done:
			stop()
			return "", nil, fmt.Errorf("api server exited during startup: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			stop()
			return "", nil, fmt.Errorf("api server not healthy at %s: %v", url, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func stopRealAPI() {
	if realAPIStop != nil {
		realAPIStop()
	}
}

// deadAPIMCP serves the real hosted MCP handler pointed at an API base that
// refuses connections, capturing transport-failure error text.
func deadAPIMCP(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(mcp.NewHTTPHandler(mcp.NewAPIClient("http://127.0.0.1:1")))
	t.Cleanup(srv.Close)
	return srv.URL + "/mcp"
}

// mcpCall invokes one tool over real HTTP and returns "OK"/"ERROR" plus text.
func mcpCall(t *testing.T, mcpURL, name string, args map[string]any) (bool, string) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	req, err := http.NewRequest(http.MethodPost, mcpURL, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
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
		Error *mcp.JSONRPCError `json:"error"`
	}
	if err := json.Unmarshal(raw, &rpc); err != nil {
		t.Fatalf("decode: %v; body=%s", err, raw)
	}
	if rpc.Error != nil {
		t.Fatalf("JSON-RPC error: %+v", rpc.Error)
	}
	text := ""
	if len(rpc.Result.Content) > 0 {
		text = rpc.Result.Content[0].Text
	}
	return !rpc.Result.IsError, text
}

// golden records an ordered transcript of tool calls with dynamic values
// (generated ids, timestamps, ports) replaced by stable placeholders.
type golden struct {
	t      *testing.T
	name   string
	mcpURL string
	vars   map[string]string // value -> placeholder
	scrubs []scrub
	post   []func(string) string
	out    strings.Builder
}

func newGolden(t *testing.T, name string) *golden {
	t.Helper()
	return newGoldenAt(t, name, realAPI(t))
}

// newGoldenAt records a transcript against a specific API (e.g. dedicatedAPI).
func newGoldenAt(t *testing.T, name, apiURL string) *golden {
	t.Helper()
	return &golden{t: t, name: name, mcpURL: apiURL + "/mcp", vars: map[string]string{}}
}

// note appends a free-form line to the transcript (e.g. what a fake runner
// received), normalized like tool output.
func (g *golden) note(format string, args ...any) {
	fmt.Fprintf(&g.out, format+"\n", args...)
}

var projectSeq struct {
	sync.Mutex
	n int
}

// project returns a project id unique to this run (the real API is shared
// across -count iterations) that renders as <base> in the golden.
func (g *golden) project(base string) string {
	projectSeq.Lock()
	projectSeq.n++
	id := fmt.Sprintf("%s-%d-%d", base, time.Now().UnixNano()%1000000, projectSeq.n)
	projectSeq.Unlock()
	g.bind(base, id)
	return id
}

type scrub struct {
	re   *regexp.Regexp
	repl string
}

// scrubRe replaces volatile text (latencies, delivery ids) after binding.
func (g *golden) scrubRe(re, repl string) {
	g.scrubs = append(g.scrubs, scrub{regexp.MustCompile(re), repl})
}

// postProcess applies a whole-transcript normalisation after scrubs.
func (g *golden) postProcess(fn func(string) string) { g.post = append(g.post, fn) }

// bind registers a dynamic value so it renders as <placeholder>.
func (g *golden) bind(placeholder, value string) {
	if value != "" {
		g.vars[value] = "<" + placeholder + ">"
	}
}

// call runs a tool, records the result (normalized at check time, so ids
// bound after the call that produced them are still replaced), and returns the raw text.
func (g *golden) call(label, tool string, args map[string]any) string {
	g.t.Helper()
	return g.callAt(g.mcpURL, label, tool, args)
}

func (g *golden) callAt(mcpURL, label, tool string, args map[string]any) string {
	g.t.Helper()
	ok, text := mcpCall(g.t, mcpURL, tool, args)
	status := "OK"
	if !ok {
		status = "ERROR"
	}
	fmt.Fprintf(&g.out, "=== %s (%s)\n%s\n%s\n", label, tool, status, text)
	return text
}

var (
	rfc3339Re  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?`)
	loopbackRe = regexp.MustCompile(`(127\.0\.0\.1|localhost):\d+`)
)

func (g *golden) normalize(s string) string {
	// Longest values first so overlapping ids cannot partially replace.
	keys := make([]string, 0, len(g.vars))
	for k := range g.vars {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		s = strings.ReplaceAll(s, k, g.vars[k])
	}
	s = rfc3339Re.ReplaceAllString(s, "<TIME>")
	s = loopbackRe.ReplaceAllString(s, "<LOOPBACK>")
	for _, sc := range g.scrubs {
		s = sc.re.ReplaceAllString(s, sc.repl)
	}
	for _, fn := range g.post {
		s = fn(s)
	}
	return s
}

// check compares (or with -update-golden rewrites) testdata/sdk_golden/<name>.golden.
func (g *golden) check() {
	g.t.Helper()
	path := filepath.Join("testdata", "sdk_golden", g.name+".golden")
	got := g.normalize(g.out.String())
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			g.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			g.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		g.t.Fatalf("missing golden %s; run with -update-golden", path)
	}
	if err != nil {
		g.t.Fatal(err)
	}
	if got != string(want) {
		g.t.Errorf("tool output drifted from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}

// field extracts the first capture of re from s or fails.
func field(t *testing.T, s, re string) string {
	t.Helper()
	m := regexp.MustCompile(re).FindStringSubmatch(s)
	if len(m) < 2 {
		t.Fatalf("pattern %q not found in:\n%s", re, s)
	}
	return m[1]
}

func fieldOptional(s, re string) string {
	if m := regexp.MustCompile(re).FindStringSubmatch(s); len(m) >= 2 {
		return m[1]
	}
	return ""
}
