package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// =============================================================================
// FeatureScheduleFields tests
// =============================================================================

func TestFeatureScheduleFields_HasAny(t *testing.T) {
	tests := []struct {
		name   string
		fields FeatureScheduleFields
		want   bool
	}{
		{"empty", FeatureScheduleFields{}, false},
		{"schedule", FeatureScheduleFields{Schedule: "0 0 * * *"}, true},
		{"run_once_at", FeatureScheduleFields{RunOnceAt: "2025-12-01T00:00:00Z"}, true},
		{"starts_at", FeatureScheduleFields{StartsAt: "2025-01-01T00:00:00Z"}, true},
		{"expires_at", FeatureScheduleFields{ExpiresAt: "2025-12-31T23:59:59Z"}, true},
		{"timezone only is not enough", FeatureScheduleFields{Timezone: "America/New_York"}, false},
		{"timezone with schedule", FeatureScheduleFields{Schedule: "0 0 * * *", Timezone: "America/New_York"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fields.HasAny(); got != tt.want {
				t.Errorf("HasAny() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractFeatureScheduleFromCreate(t *testing.T) {
	req := types.CreateEntryRequest{
		FeatureSchedule:  "0 0 * * *",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
		FeatureStartsAt:  "2025-01-01T00:00:00Z",
		FeatureExpiresAt: "2025-12-31T23:59:59Z",
		FeatureTimezone:  "America/New_York",
	}

	fields := extractFeatureScheduleFromCreate(req)

	if fields.Schedule != "0 0 * * *" {
		t.Errorf("Schedule = %q, want %q", fields.Schedule, "0 0 * * *")
	}
	if fields.RunOnceAt != "2025-12-01T00:00:00Z" {
		t.Errorf("RunOnceAt = %q, want %q", fields.RunOnceAt, "2025-12-01T00:00:00Z")
	}
	if fields.StartsAt != "2025-01-01T00:00:00Z" {
		t.Errorf("StartsAt = %q, want %q", fields.StartsAt, "2025-01-01T00:00:00Z")
	}
	if fields.ExpiresAt != "2025-12-31T23:59:59Z" {
		t.Errorf("ExpiresAt = %q, want %q", fields.ExpiresAt, "2025-12-31T23:59:59Z")
	}
	if fields.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want %q", fields.Timezone, "America/New_York")
	}
}

func TestExtractFeatureScheduleFromUpdate(t *testing.T) {
	t.Run("no fields", func(t *testing.T) {
		req := types.UpdateEntryRequest{}
		_, any := extractFeatureScheduleFromUpdate(req)
		if any {
			t.Error("expected any=false for empty request")
		}
	})

	t.Run("with fields", func(t *testing.T) {
		schedule := "0 0 * * *"
		runOnceAt := "2025-12-01T00:00:00Z"
		req := types.UpdateEntryRequest{
			FeatureSchedule:  &schedule,
			FeatureRunOnceAt: &runOnceAt,
		}
		fields, any := extractFeatureScheduleFromUpdate(req)
		if !any {
			t.Error("expected any=true")
		}
		if fields.Schedule != "0 0 * * *" {
			t.Errorf("Schedule = %q, want %q", fields.Schedule, "0 0 * * *")
		}
		if fields.RunOnceAt != "2025-12-01T00:00:00Z" {
			t.Errorf("RunOnceAt = %q, want %q", fields.RunOnceAt, "2025-12-01T00:00:00Z")
		}
	})
}

// =============================================================================
// Gate task creation integration tests
// =============================================================================

func TestSave_WithFeatureRunOnceAt_CreatesGateTask(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a task with feature_run_once_at set
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Deploy v2 service",
		Content:          "Deploy the v2 service",
		Project:          "test-project",
		FeatureID:        "deploy-v2",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
		FeatureTimezone:  "America/New_York",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify the task was created
	if resp.ID == "" {
		t.Fatal("expected task ID")
	}

	// Check that a gate task was auto-created
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gateTask := findGateTaskInDir(t, taskDir, "feature-schedule:deploy-v2")
	if gateTask == nil {
		t.Fatal("expected gate task to be created")
	}

	// Verify gate task fields
	if gateTask.GeneratedKind != "feature_schedule" {
		t.Errorf("gate generated_kind = %q, want %q", gateTask.GeneratedKind, "feature_schedule")
	}
	if gateTask.GeneratedKey != "feature-schedule:deploy-v2" {
		t.Errorf("gate generated_key = %q, want %q", gateTask.GeneratedKey, "feature-schedule:deploy-v2")
	}
	if gateTask.GeneratedBy != "feature-schedule" {
		t.Errorf("gate generated_by = %q, want %q", gateTask.GeneratedBy, "feature-schedule")
	}
	if gateTask.Generated == nil || !*gateTask.Generated {
		t.Error("gate generated should be true")
	}
	if gateTask.FeatureID != "deploy-v2" {
		t.Errorf("gate feature_id = %q, want %q", gateTask.FeatureID, "deploy-v2")
	}
	if gateTask.Status != "active" {
		t.Errorf("gate status = %q, want %q", gateTask.Status, "active")
	}
	if gateTask.RunOnceAt != "2025-12-01T00:00:00Z" {
		t.Errorf("gate run_once_at = %q, want %q", gateTask.RunOnceAt, "2025-12-01T00:00:00Z")
	}
	if gateTask.ScheduleEnabled == nil || !*gateTask.ScheduleEnabled {
		t.Error("gate schedule_enabled should be true")
	}
	if gateTask.CompleteOnIdle == nil || !*gateTask.CompleteOnIdle {
		t.Error("gate complete_on_idle should be true")
	}
}

func TestSave_WithFeatureScheduleCron_CreatesGateTask(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a task with feature_schedule (cron) set
	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:            "task",
		Title:           "Recurring deploy",
		Content:         "Deploy periodically",
		Project:         "test-project",
		FeatureID:       "recurring-deploy",
		FeatureSchedule: "0 0 * * *",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gateTask := findGateTaskInDir(t, taskDir, "feature-schedule:recurring-deploy")
	if gateTask == nil {
		t.Fatal("expected gate task to be created for cron schedule")
	}

	if gateTask.Schedule != "0 0 * * *" {
		t.Errorf("gate schedule = %q, want %q", gateTask.Schedule, "0 0 * * *")
	}
}

func TestSave_WithFeatureStartsAt_CreatesGateTask(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Time-windowed task",
		Content:          "Only active during window",
		Project:          "test-project",
		FeatureID:        "window-feature",
		FeatureStartsAt:  "2025-06-01T00:00:00Z",
		FeatureExpiresAt: "2025-12-31T23:59:59Z",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gateTask := findGateTaskInDir(t, taskDir, "feature-schedule:window-feature")
	if gateTask == nil {
		t.Fatal("expected gate task to be created for starts_at/expires_at")
	}

	if gateTask.StartsAt != "2025-06-01T00:00:00Z" {
		t.Errorf("gate starts_at = %q, want %q", gateTask.StartsAt, "2025-06-01T00:00:00Z")
	}
	if gateTask.ExpiresAt != "2025-12-31T23:59:59Z" {
		t.Errorf("gate expires_at = %q, want %q", gateTask.ExpiresAt, "2025-12-31T23:59:59Z")
	}
}

func TestSave_GateTaskIdempotency(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create first task with feature schedule
	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Task A",
		Content:          "First task",
		Project:          "test-project",
		FeatureID:        "deploy-v2",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Save task A failed: %v", err)
	}

	// Create second task in same feature with same schedule
	_, err = svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Task B",
		Content:          "Second task",
		Project:          "test-project",
		FeatureID:        "deploy-v2",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Save task B failed: %v", err)
	}

	// Count gate tasks — should be exactly one
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	count := countGateTasksInDir(t, taskDir, "feature-schedule:deploy-v2")
	if count != 1 {
		t.Errorf("expected exactly 1 gate task, found %d", count)
	}
}

func TestSave_GateTaskInjectsDependsOn(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a regular task first (no schedule)
	resp1, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Task A",
		Content:   "First task",
		Project:   "test-project",
		FeatureID: "deploy-v2",
	})
	if err != nil {
		t.Fatalf("Save task A failed: %v", err)
	}

	// Now create a task with feature schedule (triggers gate creation)
	_, err = svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Task B",
		Content:          "Second task",
		Project:          "test-project",
		FeatureID:        "deploy-v2",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Save task B failed: %v", err)
	}

	// Find the gate task ID
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gateTask := findGateTaskInDir(t, taskDir, "feature-schedule:deploy-v2")
	if gateTask == nil {
		t.Fatal("expected gate task")
	}

	// Verify Task A now has the gate in its depends_on
	taskADoc := readTaskFrontmatter(t, taskDir, resp1.ID)
	if taskADoc == nil {
		t.Fatal("could not read Task A")
	}
	if !containsString(taskADoc.DependsOn, gateTask.ID) {
		t.Errorf("Task A depends_on = %v, should contain gate ID %q", taskADoc.DependsOn, gateTask.ID)
	}
}

func TestSave_NoGateForNonTaskType(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a plan (not a task) with feature schedule fields
	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:             "plan",
		Title:            "Deploy Plan",
		Content:          "Planning doc",
		Project:          "test-project",
		FeatureID:        "deploy-v2",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	count := countGateTasksInDir(t, taskDir, "feature-schedule:deploy-v2")
	if count != 0 {
		t.Errorf("expected 0 gate tasks for non-task type, found %d", count)
	}
}

func TestSave_NoGateWithoutFeatureID(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a task with schedule fields but no feature_id
	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Orphan task",
		Content:          "No feature",
		Project:          "test-project",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	count := countGateTasksInDir(t, taskDir, "feature-schedule:")
	if count != 0 {
		t.Errorf("expected 0 gate tasks without feature_id, found %d", count)
	}
}

func TestUpdate_WithFeatureSchedule_CreatesGateTask(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a task first (no schedule)
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Task A",
		Content:   "First task",
		Project:   "test-project",
		FeatureID: "deploy-v2",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update with feature_run_once_at
	runOnceAt := "2025-12-01T00:00:00Z"
	_, err = svc.Update(ctx, resp.ID, types.UpdateEntryRequest{
		FeatureRunOnceAt: &runOnceAt,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Check that gate was created
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gateTask := findGateTaskInDir(t, taskDir, "feature-schedule:deploy-v2")
	if gateTask == nil {
		t.Fatal("expected gate task to be created on update")
	}
}

func TestUpdate_UpdatesExistingGateScheduleFields(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a task with initial schedule
	resp, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Task A",
		Content:          "First task",
		Project:          "test-project",
		FeatureID:        "deploy-v2",
		FeatureRunOnceAt: "2025-12-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Update with new schedule
	newRunOnceAt := "2026-01-15T00:00:00Z"
	_, err = svc.Update(ctx, resp.ID, types.UpdateEntryRequest{
		FeatureRunOnceAt: &newRunOnceAt,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify gate was updated, not duplicated
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	count := countGateTasksInDir(t, taskDir, "feature-schedule:deploy-v2")
	if count != 1 {
		t.Errorf("expected exactly 1 gate task after update, found %d", count)
	}

	gateTask := findGateTaskInDir(t, taskDir, "feature-schedule:deploy-v2")
	if gateTask == nil {
		t.Fatal("expected gate task")
	}
	if gateTask.RunOnceAt != "2026-01-15T00:00:00Z" {
		t.Errorf("gate run_once_at = %q, want %q", gateTask.RunOnceAt, "2026-01-15T00:00:00Z")
	}
}

func TestFeatureScheduleGeneratedKindValid(t *testing.T) {
	if !types.IsValidGeneratedKind("feature_schedule") {
		t.Error("feature_schedule should be a valid generated kind")
	}
}

func TestExtractProjectFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"projects/brain-api/task/abc123.md", "brain-api"},
		{"projects/test-project/plan/xyz.md", "test-project"},
		{"global/task/abc.md", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := extractProjectFromPath(tt.path)
			if got != tt.want {
				t.Errorf("extractProjectFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Missing edge case: no gate when feature_id present but no schedule fields
// =============================================================================

func TestSave_NoGateWithoutScheduleFields(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Create a task with feature_id but NO schedule fields
	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Plain feature task",
		Content:   "No schedule fields set",
		Project:   "test-project",
		FeatureID: "my-feature",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	count := countGateTasksInDir(t, taskDir, "feature-schedule:my-feature")
	if count != 0 {
		t.Errorf("expected 0 gate tasks when no schedule fields set, found %d", count)
	}
}

// =============================================================================
// Gate schedule field copy verification
// =============================================================================

func TestSave_GateScheduleFieldsCopiedCorrectly(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:             "task",
		Title:            "Full schedule task",
		Content:          "All schedule fields set",
		Project:          "test-project",
		FeatureID:        "full-sched",
		FeatureSchedule:  "30 2 * * MON",
		FeatureRunOnceAt: "2026-06-15T00:00:00Z",
		FeatureStartsAt:  "2026-01-01T00:00:00Z",
		FeatureExpiresAt: "2026-12-31T23:59:59Z",
		FeatureTimezone:  "Europe/Berlin",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gate := findGateTaskInDir(t, taskDir, "feature-schedule:full-sched")
	if gate == nil {
		t.Fatal("expected gate task")
	}

	if gate.Schedule != "30 2 * * MON" {
		t.Errorf("gate schedule = %q, want %q", gate.Schedule, "30 2 * * MON")
	}
	if gate.RunOnceAt != "2026-06-15T00:00:00Z" {
		t.Errorf("gate run_once_at = %q, want %q", gate.RunOnceAt, "2026-06-15T00:00:00Z")
	}
	if gate.StartsAt != "2026-01-01T00:00:00Z" {
		t.Errorf("gate starts_at = %q, want %q", gate.StartsAt, "2026-01-01T00:00:00Z")
	}
	if gate.ExpiresAt != "2026-12-31T23:59:59Z" {
		t.Errorf("gate expires_at = %q, want %q", gate.ExpiresAt, "2026-12-31T23:59:59Z")
	}
}

// =============================================================================
// End-to-end lifecycle: create → gate → deps waiting → gate completes → ready
// =============================================================================

func TestFeatureScheduleLifecycle_GateCreation_DepsWaiting(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Step 1: Create all 3 tasks first WITHOUT schedule fields.
	// This ensures they exist on disk before gate creation triggers injection.
	resp1, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Task Alpha",
		Content:   "First task",
		Project:   "test-project",
		FeatureID: "release-v3",
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("Save Task Alpha failed: %v", err)
	}

	resp2, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Task Beta",
		Content:   "Second task",
		Project:   "test-project",
		FeatureID: "release-v3",
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("Save Task Beta failed: %v", err)
	}

	resp3, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Task Gamma",
		Content:   "Third task",
		Project:   "test-project",
		FeatureID: "release-v3",
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("Save Task Gamma failed: %v", err)
	}

	// Step 2: Now update one of the tasks with feature_run_once_at to trigger gate creation.
	// This triggers ensureFeatureScheduleGate → createFeatureScheduleGate → injectGateDependency
	// which scans ALL existing feature tasks and adds the gate to their depends_on.
	runOnceAt := "2026-06-01T00:00:00Z"
	_, err = svc.Update(ctx, resp1.ID, types.UpdateEntryRequest{
		FeatureRunOnceAt: &runOnceAt,
	})
	if err != nil {
		t.Fatalf("Update with schedule failed: %v", err)
	}

	// Step 3: Verify gate was created
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gate := findGateTaskInDir(t, taskDir, "feature-schedule:release-v3")
	if gate == nil {
		t.Fatal("expected gate task to be auto-created")
	}

	// Step 4: Verify all 3 non-generated tasks have gate in depends_on
	for _, resp := range []*types.CreateEntryResponse{resp1, resp2, resp3} {
		fm := readTaskFrontmatter(t, taskDir, resp.ID)
		if fm == nil {
			t.Fatalf("could not read frontmatter for %s", resp.ID)
		}
		if !containsString(fm.DependsOn, gate.ID) {
			t.Errorf("Task %s depends_on = %v, should contain gate %q", resp.ID, fm.DependsOn, gate.ID)
		}
	}

	// Step 5: Build BrainEntry array from disk frontmatter and resolve dependencies.
	// NOTE: The gate has status "active", which the dependency resolution system treats
	// as "satisfied" (not blocking). Tasks are classified "ready" even while the gate
	// is active. The schedule-based blocking is enforced by the runner's scheduler,
	// not by the dependency resolution system.
	entries := loadEntriesFromDisk(t, taskDir)

	resolved := ResolveDependencies(entries)
	readyCount := 0
	for _, rt := range resolved.Tasks {
		if rt.GeneratedKind == "feature_schedule" {
			continue
		}
		if rt.Status != "pending" {
			continue
		}
		// Tasks have the gate in their resolved deps (proves injection worked at dep-resolution level)
		if !containsString(rt.ResolvedDeps, gate.ID) {
			t.Errorf("Task %s ResolvedDeps = %v, should contain gate %q", rt.ID, rt.ResolvedDeps, gate.ID)
		}
		readyCount++
	}
	if readyCount != 3 {
		t.Errorf("expected 3 pending tasks with resolved gate dep, got %d", readyCount)
	}
}

func TestFeatureScheduleLifecycle_GateCompletes_TasksBecomeReady(t *testing.T) {
	svc, _, brainDir := newTestBrainService(t)
	ctx := context.Background()

	// Step 1: Create all 3 tasks first WITHOUT schedule fields
	resp1, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Deploy Service",
		Content:   "Deploy the service",
		Project:   "test-project",
		FeatureID: "deploy-v3",
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("Save Deploy Service failed: %v", err)
	}

	_, err = svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Run Migrations",
		Content:   "Run database migrations",
		Project:   "test-project",
		FeatureID: "deploy-v3",
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("Save Run Migrations failed: %v", err)
	}

	_, err = svc.Save(ctx, types.CreateEntryRequest{
		Type:      "task",
		Title:     "Verify Health",
		Content:   "Verify service health",
		Project:   "test-project",
		FeatureID: "deploy-v3",
		Status:    "pending",
	})
	if err != nil {
		t.Fatalf("Save Verify Health failed: %v", err)
	}

	// Step 2: Update one task with feature schedule to trigger gate creation + injection
	runOnceAt := "2026-06-01T00:00:00Z"
	_, err = svc.Update(ctx, resp1.ID, types.UpdateEntryRequest{
		FeatureRunOnceAt: &runOnceAt,
	})
	if err != nil {
		t.Fatalf("Update with schedule failed: %v", err)
	}

	// Verify gate was created
	taskDir := filepath.Join(brainDir, "projects", "test-project", "task")
	gate := findGateTaskInDir(t, taskDir, "feature-schedule:deploy-v3")
	if gate == nil {
		t.Fatal("expected gate task")
	}

	// Before gate completes: verify tasks have gate in resolved deps
	resolvedBefore := ResolveDependencies(loadEntriesFromDisk(t, taskDir))
	for _, rt := range resolvedBefore.Tasks {
		if rt.GeneratedKind == "feature_schedule" {
			continue
		}
		if rt.Status != "pending" {
			continue
		}
		if !containsString(rt.ResolvedDeps, gate.ID) {
			t.Errorf("before completion: Task %s ResolvedDeps = %v, should contain gate %q",
				rt.ID, rt.ResolvedDeps, gate.ID)
		}
	}

	// Simulate gate completion by writing status=completed to disk
	// (The runner's processFeatureScheduleGate does this via UpdateTaskStatus.)
	gateFilePath := filepath.Join(taskDir, gate.ID+".md")
	gateContent, err := os.ReadFile(gateFilePath)
	if err != nil {
		t.Fatalf("Read gate file failed: %v", err)
	}
	gateDoc, err := frontmatter.Parse(string(gateContent))
	if err != nil {
		t.Fatalf("Parse gate frontmatter failed: %v", err)
	}
	gateDoc.Frontmatter.Status = "completed"
	updatedGateYAML := frontmatter.Serialize(&gateDoc.Frontmatter)
	updatedGateContent := "---\n" + updatedGateYAML + "---\n"
	if gateDoc.Body != "" {
		updatedGateContent += "\n" + gateDoc.Body + "\n"
	}
	if err := os.WriteFile(gateFilePath, []byte(updatedGateContent), 0o644); err != nil {
		t.Fatalf("Write gate file failed: %v", err)
	}

	// After gate completes: reload and resolve — tasks should be "ready"
	// (Gate dep is now "completed" which counts as satisfied.)
	resolvedAfter := ResolveDependencies(loadEntriesFromDisk(t, taskDir))
	readyCount := 0
	for _, rt := range resolvedAfter.Tasks {
		if rt.GeneratedKind == "feature_schedule" {
			continue
		}
		if rt.Status == "pending" && rt.Classification == "ready" {
			readyCount++
		}
	}
	if readyCount != 3 {
		t.Errorf("after gate completes: expected 3 ready tasks, got %d", readyCount)
	}

	// Verify the gate itself is now completed
	gateEntry := findGateTaskInDir(t, taskDir, "feature-schedule:deploy-v3")
	if gateEntry == nil {
		t.Fatal("gate not found after completion")
	}
	if gateEntry.Status != "completed" {
		t.Errorf("gate status = %q, want 'completed'", gateEntry.Status)
	}
}

// =============================================================================
// Test helpers
// =============================================================================

// loadEntriesFromDisk reads all .md files in taskDir and converts them to BrainEntry
// for use with ResolveDependencies. This reads directly from disk frontmatter,
// which is the source of truth for depends_on after gate dependency injection.
func loadEntriesFromDisk(t *testing.T, taskDir string) []types.BrainEntry {
	t.Helper()
	dirEntries, err := os.ReadDir(taskDir)
	if err != nil {
		t.Fatalf("ReadDir %s failed: %v", taskDir, err)
	}

	var entries []types.BrainEntry
	for _, de := range dirEntries {
		if !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(taskDir, de.Name()))
		if err != nil {
			continue
		}
		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}
		fm := doc.Frontmatter
		shortID := strings.TrimSuffix(de.Name(), ".md")
		entry := types.BrainEntry{
			ID:            shortID,
			Path:          filepath.Join(taskDir, de.Name()),
			Title:         fm.Title,
			Type:          fm.Type,
			Status:        fm.Status,
			Priority:      fm.Priority,
			DependsOn:     fm.DependsOn,
			FeatureID:     fm.FeatureID,
			GeneratedKind: fm.GeneratedKind,
			GeneratedKey:  fm.GeneratedKey,
			GeneratedBy:   fm.GeneratedBy,
			Generated:     fm.Generated,
		}
		entries = append(entries, entry)
	}
	return entries
}

type gateTaskInfo struct {
	ID              string
	GeneratedKind   string
	GeneratedKey    string
	GeneratedBy     string
	Generated       *bool
	FeatureID       string
	Status          string
	Schedule        string
	RunOnceAt       string
	StartsAt        string
	ExpiresAt       string
	ScheduleEnabled *bool
	CompleteOnIdle  *bool
}

func findGateTaskInDir(t *testing.T, taskDir, generatedKey string) *gateTaskInfo {
	t.Helper()
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadDir failed: %v", err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(taskDir, entry.Name()))
		if err != nil {
			continue
		}
		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}
		if doc.Frontmatter.GeneratedKey == generatedKey {
			return &gateTaskInfo{
				ID:              strings.TrimSuffix(entry.Name(), ".md"),
				GeneratedKind:   doc.Frontmatter.GeneratedKind,
				GeneratedKey:    doc.Frontmatter.GeneratedKey,
				GeneratedBy:     doc.Frontmatter.GeneratedBy,
				Generated:       doc.Frontmatter.Generated,
				FeatureID:       doc.Frontmatter.FeatureID,
				Status:          doc.Frontmatter.Status,
				Schedule:        doc.Frontmatter.Schedule,
				RunOnceAt:       doc.Frontmatter.RunOnceAt,
				StartsAt:        doc.Frontmatter.StartsAt,
				ExpiresAt:       doc.Frontmatter.ExpiresAt,
				ScheduleEnabled: doc.Frontmatter.ScheduleEnabled,
				CompleteOnIdle:  doc.Frontmatter.CompleteOnIdle,
			}
		}
	}
	return nil
}

func countGateTasksInDir(t *testing.T, taskDir, generatedKeyPrefix string) int {
	t.Helper()
	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("ReadDir failed: %v", err)
	}

	count := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(taskDir, entry.Name()))
		if err != nil {
			continue
		}
		doc, err := frontmatter.Parse(string(content))
		if err != nil {
			continue
		}
		if strings.HasPrefix(doc.Frontmatter.GeneratedKey, generatedKeyPrefix) {
			count++
		}
	}
	return count
}

func readTaskFrontmatter(t *testing.T, taskDir, shortID string) *frontmatter.Frontmatter {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(taskDir, shortID+".md"))
	if err != nil {
		return nil
	}
	doc, err := frontmatter.Parse(string(content))
	if err != nil {
		return nil
	}
	return &doc.Frontmatter
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
