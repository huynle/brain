package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

func TestEmbeddingsBackfillDryRunUsesConfiguredAPI(t *testing.T) {
	project := "brain-api"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/entries" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Query().Get("project"); got != project {
			t.Fatalf("project = %q, want %q", got, project)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.ListEntriesResponse{
			Entries: []types.BrainEntry{
				{ID: "42", Path: "projects/brain-api/plan/dream-tab.md", Title: "Dream Tab + Configurable Keybindings for TUI", ProjectID: project, Type: "plan", EmbeddingStatus: "missing"},
				{ID: "43", Path: "projects/brain-api/plan/current.md", Title: "Current", ProjectID: project, Type: "plan", EmbeddingStatus: "current"},
			},
			Total: 2,
			Limit: 500,
		})
	}))
	defer server.Close()

	cfg := &UnifiedConfig{}
	cfg.Runner = runner.RunnerConfig{BrainAPIURL: server.URL, APITimeout: 5000}

	var out bytes.Buffer
	cmd := &EmbeddingsCommand{
		Subcommand: "backfill",
		Config:     cfg,
		Flags:      &EmbeddingsFlags{DryRun: true, Verbose: true, Project: project},
		Out:        &out,
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := out.String()
	for _, want := range []string{server.URL, "Total notes to process: 1", "projects/brain-api/plan/dream-tab.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestEmbeddingsBackfillUsesConfiguredAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/embeddings/backfill" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req types.EmbeddingBackfillRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.DryRun {
			t.Fatalf("did not expect dry_run request")
		}
		if !req.Force {
			t.Fatalf("expected force request")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.EmbeddingBackfillResponse{Processed: 2, Duration: "12ms"})
	}))
	defer server.Close()

	cfg := &UnifiedConfig{}
	cfg.Runner = runner.RunnerConfig{BrainAPIURL: server.URL, APITimeout: 5000}

	var out bytes.Buffer
	cmd := &EmbeddingsCommand{
		Subcommand: "backfill",
		Config:     cfg,
		Flags:      &EmbeddingsFlags{Force: true},
		Out:        &out,
	}

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Processed: 2 notes") {
		t.Fatalf("expected processed count in output, got:\n%s", got)
	}
}
