package service

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func newTestProjectPlacementService(t *testing.T) *ProjectPlacementService {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return NewProjectPlacementService(store)
}

func TestProjectPlacementService_DefaultAndPersistence(t *testing.T) {
	svc := newTestProjectPlacementService(t)
	ctx := context.Background()

	defaultPlacement, err := svc.Get(ctx, "brain")
	if err != nil {
		t.Fatalf("Get default failed: %v", err)
	}
	if defaultPlacement.Affinity != types.PlacementAffinitySoft {
		t.Fatalf("default Affinity = %q, want soft", defaultPlacement.Affinity)
	}

	saved, err := svc.Put(ctx, "brain", types.ProjectPlacement{
		Affinity:             types.PlacementAffinityStrict,
		PreferredMachines:    []string{"runner-a"},
		AllowedMachines:      []string{"runner-a", "runner-b"},
		WorkspacePolicy:      types.WorkspacePolicyWorktree,
		RequiredLabels:       map[string]string{"region": "west"},
		RequiredCapabilities: []string{"docker"},
		Resources:            map[string]any{"memory_mb": float64(2048)},
	})
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if saved.ProjectID != "brain" || saved.Affinity != types.PlacementAffinityStrict || saved.WorkspacePolicy != types.WorkspacePolicyWorktree {
		t.Fatalf("saved placement = %+v", saved)
	}

	got, err := svc.Get(ctx, "brain")
	if err != nil {
		t.Fatalf("Get persisted failed: %v", err)
	}
	if got.Affinity != types.PlacementAffinityStrict || got.RequiredLabels["region"] != "west" || got.Resources["memory_mb"] != float64(2048) {
		t.Fatalf("got placement = %+v, want persisted metadata", got)
	}
}

func TestProjectPlacementService_RejectsInvalidValues(t *testing.T) {
	svc := newTestProjectPlacementService(t)
	ctx := context.Background()

	_, err := svc.Put(ctx, "brain", types.ProjectPlacement{Affinity: "hard"})
	if err == nil || !strings.Contains(err.Error(), "affinity") {
		t.Fatalf("invalid affinity error = %v, want affinity validation", err)
	}

	_, err = svc.Put(ctx, "brain", types.ProjectPlacement{WorkspacePolicy: "teleport"})
	if err == nil || !strings.Contains(err.Error(), "workspace_policy") {
		t.Fatalf("invalid workspace policy error = %v, want workspace_policy validation", err)
	}
}
