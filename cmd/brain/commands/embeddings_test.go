package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
)

func TestEmbeddingsBackfillDryRunUsesServerDataDirDatabase(t *testing.T) {
	t.Setenv("TEST_EMBEDDING_API_KEY", "test-key")

	brainDir := t.TempDir()
	dataDir := filepath.Join(brainDir, config.DataDir)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}

	store, err := storage.New(filepath.Join(dataDir, "brain.db"))
	if err != nil {
		t.Fatalf("open server database: %v", err)
	}

	body := "Existing note content that needs an embedding."
	typ := "plan"
	status := "active"
	_, err = store.InsertNote(context.Background(), &storage.NoteRow{
		Path:       "projects/test/plan/existing.md",
		ShortID:    "embed01",
		Title:      "Existing Note",
		Body:       &body,
		RawContent: &body,
		WordCount:  7,
		Metadata:   `{}`,
		Type:       &typ,
		Status:     &status,
	})
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close server database: %v", err)
	}

	cfg := &UnifiedConfig{}
	cfg.Server.BrainDir = brainDir
	cfg.Server.Embedding.APIKeyEnv = "TEST_EMBEDDING_API_KEY"

	var out bytes.Buffer
	cmd := &EmbeddingsCommand{
		Subcommand: "backfill",
		Config:     cfg,
		Flags:      &EmbeddingsFlags{DryRun: true},
		Out:        &out,
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if got := out.String(); !strings.Contains(got, "Total notes to process: 1") {
		t.Fatalf("expected dry-run to read server data-dir database, got output:\n%s", got)
	}

	if _, err := os.Stat(filepath.Join(brainDir, "brain.db")); !os.IsNotExist(err) {
		t.Fatalf("expected backfill not to create root-level brain.db, stat error: %v", err)
	}
}
