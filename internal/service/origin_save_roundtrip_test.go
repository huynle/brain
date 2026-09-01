package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestSave_OriginFieldsReachDiskAndIndex is the whole-chain guard: Save ->
// markdown frontmatter on disk -> index -> Get. A field missing from any one
// registration point along the way still passes the unit tests for the others
// and silently disappears here.
func TestSave_OriginFieldsReachDiskAndIndex(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:            "task",
		Title:           "Run this where I made it",
		Content:         "body",
		Project:         "test-project",
		OriginMachineID: "machine_a1b2c3d4",
		OriginClientID:  "mcp-9f8e7d6c",
		OriginPath:      "/Users/huy/projects/brain-api",
		MachineAffinity: types.MachineAffinityLocal,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 1. On disk, in the frontmatter — not just in SQLite. Anything that
	//    lives only in the DB is lost on the next re-index from file.
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		t.Fatalf("read task dir: %v", err)
	}
	var raw string
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(taskDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(b), "Run this where I made it") {
			raw = string(b)
			break
		}
	}
	if raw == "" {
		t.Fatal("task file not found on disk")
	}
	for _, want := range []string{
		"origin_machine_id: machine_a1b2c3d4",
		"origin_client_id: mcp-9f8e7d6c",
		"origin_path: /Users/huy/projects/brain-api",
		"machine_affinity: local",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("frontmatter missing %q:\n%s", want, raw)
		}
	}

	// 2. Back out through the read path.
	got, err := svc.Recall(ctx, resp.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if got.OriginMachineID != "machine_a1b2c3d4" {
		t.Errorf("OriginMachineID = %q, want machine_a1b2c3d4", got.OriginMachineID)
	}
	if got.OriginClientID != "mcp-9f8e7d6c" {
		t.Errorf("OriginClientID = %q, want mcp-9f8e7d6c", got.OriginClientID)
	}
	if got.OriginPath != "/Users/huy/projects/brain-api" {
		t.Errorf("OriginPath = %q, want /Users/huy/projects/brain-api", got.OriginPath)
	}
	if got.MachineAffinity != types.MachineAffinityLocal {
		t.Errorf("MachineAffinity = %q, want local", got.MachineAffinity)
	}
}

// TestSave_InvalidMachineAffinityIsDropped: a typo must not be persisted where
// it could later be read as a stricter policy than intended.
func TestSave_InvalidMachineAffinityIsDropped(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:            "task",
		Title:           "Typo affinity",
		Content:         "body",
		Project:         "test-project",
		OriginMachineID: "machine_a",
		MachineAffinity: "lokal",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	got, err := svc.Recall(ctx, resp.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if got.MachineAffinity != "" {
		t.Errorf("MachineAffinity = %q, want empty (invalid value dropped)", got.MachineAffinity)
	}
	// And it resolves to the safe default rather than to "local".
	if eff := types.ResolveMachineAffinity(got.MachineAffinity, got.OriginMachineID); eff != types.MachineAffinityPreferred {
		t.Errorf("effective affinity = %q, want preferred", eff)
	}
}

// TestUpdate_CanRehomeAndRelaxAffinity covers the recovery path: a task whose
// origin machine is gone must be movable without recreating it.
func TestUpdate_CanRehomeAndRelaxAffinity(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:            "task",
		Title:           "Stranded",
		Content:         "body",
		Project:         "test-project",
		OriginMachineID: "machine_gone",
		MachineAffinity: types.MachineAffinityLocal,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	newMachine := "machine_new"
	none := types.MachineAffinityNone
	if _, err := svc.Update(ctx, resp.ID, types.UpdateEntryRequest{
		OriginMachineID: &newMachine,
		MachineAffinity: &none,
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := svc.Recall(ctx, resp.ID)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if got.OriginMachineID != newMachine {
		t.Errorf("OriginMachineID = %q, want %q", got.OriginMachineID, newMachine)
	}
	if got.MachineAffinity != types.MachineAffinityNone {
		t.Errorf("MachineAffinity = %q, want none", got.MachineAffinity)
	}
}
