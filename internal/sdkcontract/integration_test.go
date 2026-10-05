package sdkcontract_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/sdk/brain"
)

func TestExternalGoAgainstAuthenticatedRealHandler(t *testing.T) {
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
	h := api.NewHandler(svc, api.WithTaskService(tasks))
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
	cmd := exec.Command("go", "run", "../../sdk/examples/go")
	cmd.Env = append(os.Environ(), "BRAIN_API_URL="+srv.URL, "BRAIN_API_TOKEN="+token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("external Go: %v\n%s", err, out)
	}
	t.Logf("external Go evidence: %s", out)
}
