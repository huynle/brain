package storage

import (
	"context"
	"reflect"
	"testing"
)

func TestProjectPlacement_DefaultAndPersistence(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	defaultPlacement, err := s.GetProjectPlacement(ctx, "brain")
	if err != nil {
		t.Fatalf("GetProjectPlacement default failed: %v", err)
	}
	if defaultPlacement == nil {
		t.Fatal("expected default placement, got nil")
	}
	if defaultPlacement.ProjectID != "brain" {
		t.Fatalf("ProjectID = %q, want %q", defaultPlacement.ProjectID, "brain")
	}
	if defaultPlacement.Affinity != "soft" {
		t.Fatalf("default Affinity = %q, want soft", defaultPlacement.Affinity)
	}

	want := &ProjectPlacementRow{
		ProjectID:            "brain",
		Affinity:             "strict",
		PreferredMachines:    []string{"runner-a", "runner-b"},
		AllowedMachines:      []string{"runner-a"},
		WorkspacePolicy:      "worktree",
		RequiredLabels:       map[string]string{"gpu": "true", "region": "us-east"},
		RequiredCapabilities: []string{"docker", "network"},
		ResourceRequirements: map[string]any{"memory_mb": float64(4096), "cpu": float64(2)},
	}
	if err := s.UpsertProjectPlacement(ctx, want); err != nil {
		t.Fatalf("UpsertProjectPlacement failed: %v", err)
	}

	got, err := s.GetProjectPlacement(ctx, "brain")
	if err != nil {
		t.Fatalf("GetProjectPlacement persisted failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected persisted placement, got nil")
	}
	if got.ProjectID != want.ProjectID || got.Affinity != want.Affinity || got.WorkspacePolicy != want.WorkspacePolicy {
		t.Fatalf("scalar fields = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(got.PreferredMachines, want.PreferredMachines) {
		t.Fatalf("PreferredMachines = %#v, want %#v", got.PreferredMachines, want.PreferredMachines)
	}
	if !reflect.DeepEqual(got.AllowedMachines, want.AllowedMachines) {
		t.Fatalf("AllowedMachines = %#v, want %#v", got.AllowedMachines, want.AllowedMachines)
	}
	if !reflect.DeepEqual(got.RequiredLabels, want.RequiredLabels) {
		t.Fatalf("RequiredLabels = %#v, want %#v", got.RequiredLabels, want.RequiredLabels)
	}
	if !reflect.DeepEqual(got.RequiredCapabilities, want.RequiredCapabilities) {
		t.Fatalf("RequiredCapabilities = %#v, want %#v", got.RequiredCapabilities, want.RequiredCapabilities)
	}
	if !reflect.DeepEqual(got.ResourceRequirements, want.ResourceRequirements) {
		t.Fatalf("ResourceRequirements = %#v, want %#v", got.ResourceRequirements, want.ResourceRequirements)
	}
}

func TestSchemaCreation_ProjectPlacementTableExists(t *testing.T) {
	s := newTestStorage(t)

	var name string
	err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name=?", "project_placement",
	).Scan(&name)
	if err != nil {
		t.Fatalf("table %q not found: %v", "project_placement", err)
	}
	if name != "project_placement" {
		t.Fatalf("got table name %q, want %q", name, "project_placement")
	}
}
