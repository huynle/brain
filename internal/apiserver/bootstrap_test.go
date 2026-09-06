package apiserver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
)

func TestBootstrapStartup_PasswordClaimPersistsWithoutRequest(t *testing.T) {
	t.Setenv(auth.EnvPasswordHash, "configured-hash")
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	_, dbPath, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: "localhost", BrainDir: dir})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	cleanup()
	// No bootstrap request was made. Removing the password and restarting must
	// not reopen this install, even though it never had an API or OAuth token.
	t.Setenv(auth.EnvPasswordHash, "")
	s, err := storage.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.BootstrapToken(context.Background(), "late", "secret", false)
	var closed *storage.BootstrapClosedError
	if !errors.As(err, &closed) {
		t.Fatalf("bootstrap after password removal = %v, want closed", err)
	}
}

func TestBootstrapStartup_PasswordClaimFailureStopsStartup(t *testing.T) {
	t.Setenv(auth.EnvPasswordHash, "configured-hash")
	dir := t.TempDir()
	dataDir := config.MigrateDataDir(dir)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := storage.New(filepath.Join(dataDir, "brain.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Backfill has no credentials to copy. Only the password startup write
	// hits this trigger, so this tests that specific error path.
	_, err = s.DB().Exec(`CREATE TRIGGER reject_install_claim BEFORE INSERT ON entry_meta
		WHEN NEW.path = 'brain:system/install_claimed'
		BEGIN SELECT RAISE(ABORT, 'claim persistence unavailable'); END`)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _, cleanup, err := buildHTTPHandler(ctx, ServerOptions{Host: "localhost", BrainDir: dir})
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil || !strings.Contains(err.Error(), "claim persistence unavailable") || h != nil {
		t.Fatalf("startup handler present = %v, error = %v; want persistence failure and no handler", h != nil, err)
	}
}
