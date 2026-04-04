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
// Test helpers
// =============================================================================

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
