package apiserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunServerRejectsInvalidTenancyBeforeStorage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("BRAIN_TENANT_MODE", "invalid")
	brainDir := filepath.Join(t.TempDir(), "brain")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := RunServer(ctx, ServerOptions{BrainDir: brainDir, Host: "127.0.0.1"})
	if err == nil || !strings.Contains(err.Error(), "single") || !strings.Contains(err.Error(), "multi") {
		t.Fatalf("startup error = %v; want valid tenant modes", err)
	}
	if _, err := os.Stat(brainDir); !os.IsNotExist(err) {
		t.Fatalf("invalid config must not create storage: %v", err)
	}
}
