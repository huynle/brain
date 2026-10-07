package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/mcpserver"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/sdk/brain"
)

func TestSDKStdioChild(t *testing.T) {
	if os.Getenv("BRAIN_SDK_STDIO_CHILD") != "1" {
		t.Skip("child process entry point")
	}
	if err := mcpserver.RunMCPServer(context.Background(), mcpserver.MCPOptions{APIURL: os.Getenv("BRAIN_API_URL")}, os.Stdin, os.Stdout); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func TestSDKStdioAuthenticatedProcessParity(t *testing.T) {
	root := t.TempDir()
	owner, err := storage.New(filepath.Join(root, "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, err := owner.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := owner.SingleModeTokens(tenant.ModeSingle)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"admin:*", "read:*"} {
		if err := tokens.CreateToken(context.Background(), scope, scope+"-stdio-fixture", scope); err != nil {
			t.Fatal(err)
		}
	}
	control, err := owner.Control()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BrainDir: root, EnableAuth: true}
	cfg.Tenancy.Mode = tenant.ModeSingle
	idx := indexer.NewIndexer(root, store)
	svc := service.NewBrainService(&cfg, store, idx, nil, nil)
	blobs, err := blobstore.NewFilesystemStore(filepath.Join(root, "blobs"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	attachments := service.NewAttachmentService(store, blobs, svc, 8<<20)
	srv := httptest.NewServer(api.NewRouter(cfg, api.WithHandler(api.NewHandler(svc, api.WithAttachmentService(attachments))), api.WithTokenValidator(control)))
	defer srv.Close()
	client, err := brain.New(brain.Config{BaseURL: srv.URL, Token: "admin:*-stdio-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	project := "stdio-fixture"
	call := startSDKStdio(t, srv.URL, "admin:*-stdio-fixture", root)
	saved := call("save", map[string]any{"type": "summary", "title": "Stdio SDK parity", "content": "exact child content", "project": project}, false)
	if !strings.Contains(saved, "Saved") {
		t.Fatalf("save result=%s", saved)
	}
	entries, err := client.Entries().List(context.Background(), &brain.EntriesListParams{Project: &project})
	if err != nil || entries.Entries == nil || len(*entries.Entries) != 1 {
		t.Fatalf("SDK list=%+v err=%v", entries, err)
	}
	id := (*entries.Entries)[0].Id
	text := call("recall", map[string]any{"path": id}, false)
	if !strings.Contains(text, "exact child content") {
		t.Fatalf("recall lost content: %s", text)
	}
	missing := call("recall", map[string]any{"path": "missing1"}, true)
	_, sdkErr := client.Entries().Get(context.Background(), "missing1")
	if sdkErr == nil || !strings.Contains(missing, sdkErr.(*brain.Error).Message) {
		t.Fatalf("legacy/SDK error differs: %s %v", missing, sdkErr)
	}
	content := []byte("stdio local bytes\x00\xff")
	call("attachment_upload", map[string]any{"project": project, "filename": "input.txt", "content": base64.StdEncoding.EncodeToString(content)}, false)
	list, err := client.Attachments().List(context.Background(), project)
	if err != nil || list.Attachments == nil || len(*list.Attachments) != 1 {
		t.Fatalf("SDK attachments=%+v err=%v", list, err)
	}
	attachmentID := (*list.Attachments)[0].Id
	inline := call("attachment_download", map[string]any{"project": project, "attachment_id": attachmentID}, false)
	if !strings.Contains(inline, base64.StdEncoding.EncodeToString(content)) {
		t.Fatal("inline bytes differ")
	}
	readOnly := startSDKStdio(t, srv.URL, "read:*-stdio-fixture", t.TempDir())
	readOnly("recall", map[string]any{"path": id}, false)
	readOnly("save", map[string]any{"type": "summary", "title": "forbidden", "content": "must not persist", "project": project}, true)
	unauth := startSDKStdio(t, srv.URL, "", t.TempDir())
	unauth("recall", map[string]any{"path": id}, true)
}

func startSDKStdio(t *testing.T, origin, token, home string) func(string, map[string]any, bool) string {
	t.Helper()
	// Match os.Getwd's canonical spelling (macOS /var -> /private/var).
	// Legacy discovery expects HOME and cwd to use the same path spelling.
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestSDKStdioChild$")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "BRAIN_") && !strings.HasPrefix(env, "HOME=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "BRAIN_SDK_STDIO_CHILD=1", "BRAIN_API_URL="+origin, "BRAIN_API_TOKEN="+token, "HOME="+home)
	cmd.Dir = home
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		err := cmd.Wait()
		cancel()
		if err != nil {
			t.Errorf("stdio child: %v stderr=%s", err, stderr.String())
		}
	})
	encoder, decoder := json.NewEncoder(stdin), json.NewDecoder(stdout)
	seq := 0
	return func(name string, args map[string]any, wantError bool) string {
		t.Helper()
		seq++
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": seq, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}); err != nil {
			t.Fatal(err)
		}
		var response struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result struct {
				IsError bool `json:"isError"`
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := decoder.Decode(&response); err != nil {
			if err == io.EOF {
				t.Fatalf("stdio EOF %s", stderr.String())
			}
			t.Fatal(err)
		}
		if response.ID != seq || len(response.Error) > 0 || response.Result.IsError != wantError {
			t.Fatalf("%s response=%+v", name, response)
		}
		var text strings.Builder
		for _, part := range response.Result.Content {
			text.WriteString(part.Text)
		}
		return text.String()
	}
}
