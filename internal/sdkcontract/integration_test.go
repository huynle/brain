package sdkcontract_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/attentionstore"
	"github.com/huynle/brain-api/internal/blobstore"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/sdk/brain"
)

func TestExternalClientsAgainstAuthenticatedRealHandler(t *testing.T) {
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
	const token = "sdk-integration-secret"
	if err := tokens.CreateToken(context.Background(), "sdk-example", token, "admin:*"); err != nil {
		t.Fatal(err)
	}
	control, err := owner.Control()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{BrainDir: root, EnableAuth: true}
	cfg.Tenancy.Mode = tenant.ModeSingle
	idx := indexer.NewIndexer(root, store)
	svc := service.NewBrainService(&cfg, store, idx, nil, nil)
	tasks := service.NewTaskService(&cfg, store, idx)
	blobs, err := blobstore.NewFilesystemStore(filepath.Join(root, "attachments"), 8<<20)
	if err != nil {
		t.Fatal(err)
	}
	attachments := service.NewAttachmentService(store, blobs, svc, 8<<20)
	goals := service.NewGoalService(svc, tasks, store)
	inbox, err := attentionstore.Open(filepath.Join(root, "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	attention := service.NewAttentionService(inbox)
	reminders := service.NewReminderService(svc, store)
	webhooks := service.NewWebhookService(store)
	h := api.NewHandler(svc, api.WithTaskService(tasks), api.WithAttachmentService(attachments), api.WithGoalService(goals), api.WithReminderService(reminders), api.WithAttentionService(attention), api.WithWebhookService(webhooks))
	srv := httptest.NewServer(api.NewRouter(cfg, api.WithHandler(h), api.WithTokenValidator(control)))
	defer srv.Close()
	c, err := brain.New(brain.Config{BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Entries().List(context.Background(), nil); err == nil {
		t.Fatal("unauthenticated SDK request accepted")
	}
	authed, err := brain.New(brain.Config{BaseURL: srv.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	defer authed.Close()
	exerciseNotificationSDK(t, authed)
	exerciseWebhookSDK(t, authed)
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	goExample, err := os.ReadFile(filepath.Join(repo, "sdk/examples/go/main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "main.go"), goExample, 0600); err != nil {
		t.Fatal(err)
	}
	mod := "module sdk-consumer\n\ngo 1.25.0\n\nrequire github.com/huynle/brain-api v0.0.0\nreplace github.com/huynle/brain-api => " + repo + "\n"
	if err := os.WriteFile(filepath.Join(external, "go.mod"), []byte(mod), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "-mod=mod", ".")
	cmd.Dir = external
	cmd.Env = append(os.Environ(), "BRAIN_API_URL="+srv.URL, "BRAIN_API_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("external Go: %v\n%s", err, out)
	}
	t.Logf("external Go evidence: %s", out)
	if os.Getenv("BRAIN_SDK_NODE_INTEGRATION") != "1" {
		t.Log("Node package integration not requested; set BRAIN_SDK_NODE_INTEGRATION=1 after npm ci/build")
		return
	}
	pack := exec.CommandContext(ctx, "npm", "pack", "--ignore-scripts", "--json", "--pack-destination", external)
	pack.Dir = filepath.Join(repo, "sdk/typescript")
	packed, err := pack.Output()
	if err != nil {
		t.Fatal(err)
	}
	var archives []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(packed, &archives); err != nil || len(archives) != 1 {
		t.Fatalf("invalid npm pack output: %s (%v)", packed, err)
	}
	install := exec.CommandContext(ctx, "npm", "install", "--ignore-scripts", "--offline", "--no-audit", "--no-fund", "--prefix", external, filepath.Join(external, archives[0].Filename))
	if out, err := install.CombinedOutput(); err != nil {
		t.Fatalf("external package install: %v\n%s", err, out)
	}
	nodeExample, err := os.ReadFile(filepath.Join(repo, "sdk/examples/node/main.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, "main.mjs"), nodeExample, 0600); err != nil {
		t.Fatal(err)
	}
	node := exec.CommandContext(ctx, "node", filepath.Join(external, "main.mjs"))
	node.Env = cmd.Env
	if out, err := node.CombinedOutput(); err != nil {
		t.Fatalf("external Node: %v\n%s", err, out)
	} else {
		t.Logf("external Node package evidence: %s", out)
	}
}
