package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"

	_ "github.com/glebarez/go-sqlite"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestTaskService creates a TaskServiceImpl with an in-memory DB and temp brainDir.
func newTestTaskService(t *testing.T) (*TaskServiceImpl, *storage.StorageLayer, string) {
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

	brainDir := t.TempDir()
	cfg := &config.Config{BrainDir: brainDir}

	svc := NewTaskService(cfg, store)
	return svc, store, brainDir
}

// insertTaskNote inserts a task NoteRow into the storage layer.
func insertTaskNote(t *testing.T, store *storage.StorageLayer, shortID, title, status, priority, projectID string, metadata map[string]interface{}) {
	t.Helper()
	ctx := context.Background()

	metaJSON, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}

	typ := "task"
	created := "2025-01-01T00:00:00Z"
	modified := "2025-01-02T00:00:00Z"
	path := "projects/" + projectID + "/task/" + shortID + ".md"

	note := &storage.NoteRow{
		Path:      path,
		ShortID:   shortID,
		Title:     title,
		Metadata:  string(metaJSON),
		Type:      &typ,
		Status:    &status,
		Priority:  &priority,
		ProjectID: &projectID,
		Created:   &created,
		Modified:  &modified,
	}

	_, err = store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}
}

// createProjectDir creates the projects/<name>/task/ directory structure.
func createProjectDir(t *testing.T, brainDir, projectName string) {
	t.Helper()
	taskDir := filepath.Join(brainDir, "projects", projectName, "task")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NoteRowToBrainEntry
// ---------------------------------------------------------------------------

func TestNoteRowToBrainEntry_BasicFields(t *testing.T) {
	typ := "task"
	status := "pending"
	priority := "high"
	projectID := "my-project"
	featureID := "feat-1"
	created := "2025-01-01T00:00:00Z"
	modified := "2025-01-02T00:00:00Z"
	body := "task body content"

	row := &storage.NoteRow{
		Path:      "projects/my-project/task/abc12def.md",
		ShortID:   "abc12def",
		Title:     "My Task",
		Body:      &body,
		Metadata:  "{}",
		Type:      &typ,
		Status:    &status,
		Priority:  &priority,
		ProjectID: &projectID,
		FeatureID: &featureID,
		Created:   &created,
		Modified:  &modified,
	}

	entry := NoteRowToBrainEntry(row)

	if entry.ID != "abc12def" {
		t.Errorf("ID = %q, want %q", entry.ID, "abc12def")
	}
	if entry.Path != "projects/my-project/task/abc12def.md" {
		t.Errorf("Path = %q, want %q", entry.Path, "projects/my-project/task/abc12def.md")
	}
	if entry.Title != "My Task" {
		t.Errorf("Title = %q, want %q", entry.Title, "My Task")
	}
	if entry.Type != "task" {
		t.Errorf("Type = %q, want %q", entry.Type, "task")
	}
	if entry.Status != "pending" {
		t.Errorf("Status = %q, want %q", entry.Status, "pending")
	}
	if entry.Priority != "high" {
		t.Errorf("Priority = %q, want %q", entry.Priority, "high")
	}
	if entry.ProjectID != "my-project" {
		t.Errorf("ProjectID = %q, want %q", entry.ProjectID, "my-project")
	}
	if entry.FeatureID != "feat-1" {
		t.Errorf("FeatureID = %q, want %q", entry.FeatureID, "feat-1")
	}
	if entry.Created != "2025-01-01T00:00:00Z" {
		t.Errorf("Created = %q, want %q", entry.Created, "2025-01-01T00:00:00Z")
	}
	if entry.Modified != "2025-01-02T00:00:00Z" {
		t.Errorf("Modified = %q, want %q", entry.Modified, "2025-01-02T00:00:00Z")
	}
	if entry.Content != "task body content" {
		t.Errorf("Content = %q, want %q", entry.Content, "task body content")
	}
}

func TestNoteRowToBrainEntry_MetadataParsing(t *testing.T) {
	metadata := map[string]interface{}{
		"depends_on":           []interface{}{"dep1", "dep2"},
		"tags":                 []interface{}{"tag1", "tag2"},
		"workdir":              "/home/user/project",
		"git_branch":           "feature-branch",
		"git_remote":           "origin",
		"direct_prompt":        "do the thing",
		"agent":                "dev",
		"model":                "claude-4",
		"feature_priority":     "high",
		"feature_depends_on":   []interface{}{"feat-0"},
		"schedule":             "0 * * * *",
		"schedule_enabled":     true,
		"generated":            true,
		"generated_kind":       "feature_checkout",
		"merge_policy":         "auto_merge",
		"merge_strategy":       "squash",
		"execution_mode":       "worktree",
		"complete_on_idle":     true,
		"target_workdir":       "/tmp/work",
		"open_pr_before_merge": true,
	}

	metaJSON, _ := json.Marshal(metadata)
	typ := "task"
	status := "pending"

	row := &storage.NoteRow{
		Path:     "projects/test/task/xyz.md",
		ShortID:  "xyz98765",
		Title:    "Test Task",
		Metadata: string(metaJSON),
		Type:     &typ,
		Status:   &status,
	}

	entry := NoteRowToBrainEntry(row)

	// depends_on
	if len(entry.DependsOn) != 2 || entry.DependsOn[0] != "dep1" || entry.DependsOn[1] != "dep2" {
		t.Errorf("DependsOn = %v, want [dep1, dep2]", entry.DependsOn)
	}

	// tags
	if len(entry.Tags) != 2 || entry.Tags[0] != "tag1" {
		t.Errorf("Tags = %v, want [tag1, tag2]", entry.Tags)
	}

	// git/execution
	if entry.Workdir != "/home/user/project" {
		t.Errorf("Workdir = %q, want %q", entry.Workdir, "/home/user/project")
	}
	if entry.GitBranch != "feature-branch" {
		t.Errorf("GitBranch = %q, want %q", entry.GitBranch, "feature-branch")
	}
	if entry.GitRemote != "origin" {
		t.Errorf("GitRemote = %q, want %q", entry.GitRemote, "origin")
	}
	if entry.DirectPrompt != "do the thing" {
		t.Errorf("DirectPrompt = %q, want %q", entry.DirectPrompt, "do the thing")
	}
	if entry.Agent != "dev" {
		t.Errorf("Agent = %q, want %q", entry.Agent, "dev")
	}
	if entry.Model != "claude-4" {
		t.Errorf("Model = %q, want %q", entry.Model, "claude-4")
	}

	// feature
	if entry.FeaturePriority != "high" {
		t.Errorf("FeaturePriority = %q, want %q", entry.FeaturePriority, "high")
	}
	if len(entry.FeatureDependsOn) != 1 || entry.FeatureDependsOn[0] != "feat-0" {
		t.Errorf("FeatureDependsOn = %v, want [feat-0]", entry.FeatureDependsOn)
	}

	// schedule
	if entry.Schedule != "0 * * * *" {
		t.Errorf("Schedule = %q, want %q", entry.Schedule, "0 * * * *")
	}
	if entry.ScheduleEnabled == nil || !*entry.ScheduleEnabled {
		t.Error("ScheduleEnabled should be true")
	}

	// generated
	if entry.Generated == nil || !*entry.Generated {
		t.Error("Generated should be true")
	}
	if entry.GeneratedKind != "feature_checkout" {
		t.Errorf("GeneratedKind = %q, want %q", entry.GeneratedKind, "feature_checkout")
	}

	// merge
	if entry.MergePolicy != "auto_merge" {
		t.Errorf("MergePolicy = %q, want %q", entry.MergePolicy, "auto_merge")
	}
	if entry.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy = %q, want %q", entry.MergeStrategy, "squash")
	}
	if entry.ExecutionMode != "worktree" {
		t.Errorf("ExecutionMode = %q, want %q", entry.ExecutionMode, "worktree")
	}
	if entry.CompleteOnIdle == nil || !*entry.CompleteOnIdle {
		t.Error("CompleteOnIdle should be true")
	}
	if entry.TargetWorkdir != "/tmp/work" {
		t.Errorf("TargetWorkdir = %q, want %q", entry.TargetWorkdir, "/tmp/work")
	}
	if entry.OpenPRBeforeMerge == nil || !*entry.OpenPRBeforeMerge {
		t.Error("OpenPRBeforeMerge should be true")
	}
}

func TestNoteRowToBrainEntry_EmptyMetadata(t *testing.T) {
	typ := "task"
	status := "pending"

	row := &storage.NoteRow{
		Path:     "projects/test/task/abc.md",
		ShortID:  "abc12345",
		Title:    "Empty Meta Task",
		Metadata: "{}",
		Type:     &typ,
		Status:   &status,
	}

	entry := NoteRowToBrainEntry(row)

	if entry.ID != "abc12345" {
		t.Errorf("ID = %q, want %q", entry.ID, "abc12345")
	}
	if len(entry.DependsOn) != 0 {
		t.Errorf("DependsOn = %v, want empty", entry.DependsOn)
	}
	if len(entry.Tags) != 0 {
		t.Errorf("Tags = %v, want empty", entry.Tags)
	}
}

func TestNoteRowToBrainEntry_NullableFields(t *testing.T) {
	// All nullable fields are nil
	row := &storage.NoteRow{
		Path:     "projects/test/task/abc.md",
		ShortID:  "abc12345",
		Title:    "Minimal Task",
		Metadata: "{}",
	}

	entry := NoteRowToBrainEntry(row)

	if entry.Type != "" {
		t.Errorf("Type = %q, want empty", entry.Type)
	}
	if entry.Status != "" {
		t.Errorf("Status = %q, want empty", entry.Status)
	}
	if entry.Priority != "" {
		t.Errorf("Priority = %q, want empty", entry.Priority)
	}
	if entry.Content != "" {
		t.Errorf("Content = %q, want empty", entry.Content)
	}
}

// ---------------------------------------------------------------------------
// ListProjects
// ---------------------------------------------------------------------------

func TestListProjects_Empty(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	projects, err := svc.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

func TestListProjects_WithProjects(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()

	// Create project directories with task/ subfolder
	createProjectDir(t, brainDir, "project-a")
	createProjectDir(t, brainDir, "project-b")

	// Create a directory WITHOUT task/ subfolder (should be excluded)
	os.MkdirAll(filepath.Join(brainDir, "projects", "no-tasks"), 0o755)

	projects, err := svc.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("expected 2 projects, got %d: %v", len(projects), projects)
	}

	// Check both projects are present (order may vary)
	found := map[string]bool{}
	for _, p := range projects {
		found[p] = true
	}
	if !found["project-a"] {
		t.Error("expected project-a in results")
	}
	if !found["project-b"] {
		t.Error("expected project-b in results")
	}
}

func TestListProjects_NoProjectsDir(t *testing.T) {
	// brainDir exists but has no projects/ subdirectory
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	projects, err := svc.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(projects))
	}
}

// ---------------------------------------------------------------------------
// GetTasks
// ---------------------------------------------------------------------------

func TestGetTasks_Empty(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	result, err := svc.GetTasks(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(result.Tasks))
	}
	if result.Stats == nil {
		t.Fatal("expected non-nil stats")
	}
}

func TestGetTasks_WithTasks(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "pending", "high", "myproj", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Task B", "pending", "medium", "myproj", map[string]interface{}{
		"depends_on": []interface{}{"aaa11111"},
	})

	result, err := svc.GetTasks(ctx, "myproj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(result.Tasks))
	}
	if result.Stats.Total != 2 {
		t.Errorf("stats.total = %d, want 2", result.Stats.Total)
	}
	if result.Stats.Ready != 1 {
		t.Errorf("stats.ready = %d, want 1", result.Stats.Ready)
	}
	if result.Stats.Waiting != 1 {
		t.Errorf("stats.waiting = %d, want 1", result.Stats.Waiting)
	}
}

func TestGetTasks_ProjectIsolation(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "pending", "high", "proj-1", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Task B", "pending", "medium", "proj-2", map[string]interface{}{})

	result, err := svc.GetTasks(ctx, "proj-1")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task for proj-1, got %d", len(result.Tasks))
	}
	if result.Tasks[0].ID != "aaa11111" {
		t.Errorf("task ID = %q, want %q", result.Tasks[0].ID, "aaa11111")
	}
}

// ---------------------------------------------------------------------------
// GetReady / GetWaiting / GetBlocked
// ---------------------------------------------------------------------------

func TestGetReady(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Ready Task", "pending", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Waiting Task", "pending", "low", "proj", map[string]interface{}{
		"depends_on": []interface{}{"aaa11111"},
	})

	ready, err := svc.GetReady(ctx, "proj", nil)
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready task, got %d", len(ready))
	}
	if ready[0].ID != "aaa11111" {
		t.Errorf("ready task ID = %q, want %q", ready[0].ID, "aaa11111")
	}
}

func TestGetWaiting(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Dep Task", "pending", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Waiting Task", "pending", "low", "proj", map[string]interface{}{
		"depends_on": []interface{}{"aaa11111"},
	})

	waiting, err := svc.GetWaiting(ctx, "proj")
	if err != nil {
		t.Fatalf("GetWaiting failed: %v", err)
	}
	if len(waiting) != 1 {
		t.Fatalf("expected 1 waiting task, got %d", len(waiting))
	}
	if waiting[0].ID != "bbb22222" {
		t.Errorf("waiting task ID = %q, want %q", waiting[0].ID, "bbb22222")
	}
}

func TestGetBlocked(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Blocked Dep", "blocked", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Blocked Task", "pending", "low", "proj", map[string]interface{}{
		"depends_on": []interface{}{"aaa11111"},
	})

	blocked, err := svc.GetBlocked(ctx, "proj")
	if err != nil {
		t.Fatalf("GetBlocked failed: %v", err)
	}
	if len(blocked) != 1 {
		t.Fatalf("expected 1 blocked task, got %d", len(blocked))
	}
	if blocked[0].ID != "bbb22222" {
		t.Errorf("blocked task ID = %q, want %q", blocked[0].ID, "bbb22222")
	}
}

// ---------------------------------------------------------------------------
// GetNext
// ---------------------------------------------------------------------------

func TestGetNext_ReturnsHighestPriority(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "low11111", "Low Task", "pending", "low", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "high1111", "High Task", "pending", "high", "proj", map[string]interface{}{})

	next, err := svc.GetNext(ctx, "proj", nil)
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected non-nil task")
	}
	if next.ID != "high1111" {
		t.Errorf("next.ID = %q, want %q", next.ID, "high1111")
	}
}

func TestGetNext_NoTasks(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	next, err := svc.GetNext(ctx, "empty-proj", nil)
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next != nil {
		t.Errorf("expected nil, got %v", next)
	}
}

// ---------------------------------------------------------------------------
// ClaimTask / ReleaseTask / GetClaimStatus
// ---------------------------------------------------------------------------

func TestClaimTask_Success(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	resp, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.TaskID != "task1" {
		t.Errorf("TaskID = %q, want %q", resp.TaskID, "task1")
	}
	if resp.RunnerID != "runner-1" {
		t.Errorf("RunnerID = %q, want %q", resp.RunnerID, "runner-1")
	}
	if resp.ClaimedAt == "" {
		t.Error("expected non-empty ClaimedAt")
	}
}

func TestClaimTask_Conflict(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// First claim succeeds
	_, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("first ClaimTask failed: %v", err)
	}

	// Second claim by different runner should conflict
	resp, err := svc.ClaimTask(ctx, "proj", "task1", "runner-2")
	if err != api.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.ClaimedBy != "runner-1" {
		t.Errorf("ClaimedBy = %q, want %q", resp.ClaimedBy, "runner-1")
	}
}

func TestClaimTask_SameRunnerReclaim(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// First claim
	_, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("first ClaimTask failed: %v", err)
	}

	// Same runner re-claims — should succeed
	resp, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("re-claim failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true for re-claim")
	}
}

func TestReleaseTask_Success(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// Claim then release
	_, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	err = svc.ReleaseTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ReleaseTask failed: %v", err)
	}

	// Verify claim is gone
	status, err := svc.GetClaimStatus(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetClaimStatus failed: %v", err)
	}
	if status.Claimed {
		t.Error("expected claimed=false after release")
	}
}

func TestReleaseTask_NotFound(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	err := svc.ReleaseTask(ctx, "proj", "nonexistent", "runner-1")
	if err != api.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestReleaseTask_WrongRunner(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	_, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	err = svc.ReleaseTask(ctx, "proj", "task1", "runner-2")
	if err != api.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestGetClaimStatus_NotClaimed(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	status, err := svc.GetClaimStatus(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetClaimStatus failed: %v", err)
	}
	if status.Claimed {
		t.Error("expected claimed=false")
	}
	if status.TaskID != "task1" {
		t.Errorf("TaskID = %q, want %q", status.TaskID, "task1")
	}
}

func TestGetClaimStatus_Claimed(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	_, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	status, err := svc.GetClaimStatus(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetClaimStatus failed: %v", err)
	}
	if !status.Claimed {
		t.Error("expected claimed=true")
	}
	if status.RunnerID != "runner-1" {
		t.Errorf("RunnerID = %q, want %q", status.RunnerID, "runner-1")
	}
	if status.ClaimedAt == "" {
		t.Error("expected non-empty ClaimedAt")
	}
}

// ---------------------------------------------------------------------------
// GetMultiTaskStatus
// ---------------------------------------------------------------------------

func TestGetMultiTaskStatus(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "completed", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Task B", "pending", "medium", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "ccc33333", "Task C", "completed", "low", "proj", map[string]interface{}{})

	resp, err := svc.GetMultiTaskStatus(ctx, "proj", types.MultiTaskStatusRequest{
		TaskIDs: []string{"aaa11111", "bbb22222"},
	})
	if err != nil {
		t.Fatalf("GetMultiTaskStatus failed: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(resp.Tasks))
	}
	if resp.AllCompleted {
		t.Error("expected allCompleted=false (bbb22222 is pending)")
	}
}

func TestGetMultiTaskStatus_AllCompleted(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "completed", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Task B", "validated", "medium", "proj", map[string]interface{}{})

	resp, err := svc.GetMultiTaskStatus(ctx, "proj", types.MultiTaskStatusRequest{
		TaskIDs: []string{"aaa11111", "bbb22222"},
	})
	if err != nil {
		t.Fatalf("GetMultiTaskStatus failed: %v", err)
	}
	if !resp.AllCompleted {
		t.Error("expected allCompleted=true")
	}
}

func TestGetMultiTaskStatus_UnknownIDs(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	resp, err := svc.GetMultiTaskStatus(ctx, "proj", types.MultiTaskStatusRequest{
		TaskIDs: []string{"nonexistent"},
	})
	if err != nil {
		t.Fatalf("GetMultiTaskStatus failed: %v", err)
	}
	if len(resp.Tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(resp.Tasks))
	}
}

// ---------------------------------------------------------------------------
// GetFeatures / GetReadyFeatures / GetFeature
// ---------------------------------------------------------------------------

func TestGetFeatures(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feat-1",
	})
	insertTaskNote(t, store, "bbb22222", "Task B", "pending", "medium", "proj", map[string]interface{}{
		"feature_id": "feat-1",
	})
	insertTaskNote(t, store, "ccc33333", "Task C", "pending", "low", "proj", map[string]interface{}{
		"feature_id": "feat-2",
	})

	resp, err := svc.GetFeatures(ctx, "proj")
	if err != nil {
		t.Fatalf("GetFeatures failed: %v", err)
	}
	if len(resp.Features) != 2 {
		t.Fatalf("expected 2 features, got %d", len(resp.Features))
	}
}

func TestTaskService_GetReadyFeatures(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	// feat-1: ready (no deps)
	insertTaskNote(t, store, "aaa11111", "Task A", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feat-1",
	})
	// feat-2: depends on feat-1 (waiting)
	insertTaskNote(t, store, "bbb22222", "Task B", "pending", "medium", "proj", map[string]interface{}{
		"feature_id":         "feat-2",
		"feature_depends_on": []interface{}{"feat-1"},
	})

	resp, err := svc.GetReadyFeatures(ctx, "proj")
	if err != nil {
		t.Fatalf("GetReadyFeatures failed: %v", err)
	}
	if len(resp.Features) != 1 {
		t.Fatalf("expected 1 ready feature, got %d", len(resp.Features))
	}
	if resp.Features[0].FeatureID != "feat-1" {
		t.Errorf("ready feature ID = %q, want %q", resp.Features[0].FeatureID, "feat-1")
	}
}

func TestGetFeature_Found(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feat-1",
	})

	resp, err := svc.GetFeature(ctx, "proj", "feat-1")
	if err != nil {
		t.Fatalf("GetFeature failed: %v", err)
	}
	if resp.Feature.FeatureID != "feat-1" {
		t.Errorf("FeatureID = %q, want %q", resp.Feature.FeatureID, "feat-1")
	}
	if len(resp.Feature.Tasks) != 1 {
		t.Errorf("expected 1 task in feature, got %d", len(resp.Feature.Tasks))
	}
}

func TestGetFeature_NotFound(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feat-1",
	})

	_, err := svc.GetFeature(ctx, "proj", "nonexistent")
	if err != api.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Stubs: CheckoutFeature / TriggerTask
// ---------------------------------------------------------------------------

func TestCheckoutFeature_Stub(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	result, err := svc.CheckoutFeature(ctx, "proj", "feat-1", nil)
	if err != nil {
		t.Fatalf("CheckoutFeature stub failed: %v", err)
	}
	// After implementation, result should be non-nil
	if result == nil {
		t.Error("CheckoutFeature should return non-nil result")
	}
}

func TestTriggerTask_ScheduledTask(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	ctx := context.Background()

	// Create required project directory
	createProjectDir(t, brainDir, "proj")

	// Create a scheduled task with status=active
	trueVal := true
	insertTaskNote(t, store, "sched1", "Test Scheduled Task", "active", "medium", "proj", map[string]interface{}{
		"schedule":         "*/5 * * * *",
		"schedule_enabled": trueVal,
	})

	resp, err := svc.TriggerTask(ctx, "proj", "sched1")
	if err != nil {
		t.Fatalf("TriggerTask failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if !resp.Triggered {
		t.Errorf("expected Triggered=true, got false. Reason: %s", resp.Reason)
	}
	if resp.RunID == "" {
		t.Error("expected non-empty RunID")
	}
	if resp.NextRun == "" {
		t.Error("expected non-empty NextRun")
	}
	// Validate NextRun is valid RFC3339
	if _, parseErr := time.Parse(time.RFC3339, resp.NextRun); parseErr != nil {
		t.Errorf("NextRun %q is not valid RFC3339: %v", resp.NextRun, parseErr)
	}
	if resp.TaskID != "sched1" {
		t.Errorf("TaskID = %q, want %q", resp.TaskID, "sched1")
	}

	// Verify side effects: task status should now be "pending"
	tasks, err := svc.getAllTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("getAllTasks failed: %v", err)
	}
	var found bool
	for _, task := range tasks {
		if task.ID == "sched1" {
			found = true
			if task.Status != "pending" {
				t.Errorf("task status after trigger = %q, want %q", task.Status, "pending")
			}
			// Verify a run entry was created
			if len(task.Runs) == 0 {
				t.Fatal("expected at least one run entry after trigger")
			}
			lastRun := task.Runs[len(task.Runs)-1]
			if lastRun.RunID != resp.RunID {
				t.Errorf("last run RunID = %q, want %q", lastRun.RunID, resp.RunID)
			}
			if lastRun.Status != "in_progress" {
				t.Errorf("last run status = %q, want %q", lastRun.Status, "in_progress")
			}
			if lastRun.Started == "" {
				t.Error("expected non-empty Started timestamp on run")
			}
			break
		}
	}
	if !found {
		t.Fatal("triggered task not found in getAllTasks result")
	}
}

func TestTriggerTask_NoSchedule(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	ctx := context.Background()

	createProjectDir(t, brainDir, "proj")

	// Task without schedule
	insertTaskNote(t, store, "nosched", "No Schedule Task", "active", "medium", "proj", map[string]interface{}{})

	resp, err := svc.TriggerTask(ctx, "proj", "nosched")
	if err != nil {
		t.Fatalf("TriggerTask failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Triggered {
		t.Error("expected Triggered=false for task with no schedule")
	}
	if resp.Reason != "task has no schedule" {
		t.Errorf("Reason = %q, want %q", resp.Reason, "task has no schedule")
	}
}

func TestTriggerTask_DisabledSchedule(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	ctx := context.Background()

	createProjectDir(t, brainDir, "proj")

	falseVal := false
	insertTaskNote(t, store, "disabled1", "Disabled Schedule", "active", "medium", "proj", map[string]interface{}{
		"schedule":         "*/5 * * * *",
		"schedule_enabled": falseVal,
	})

	resp, err := svc.TriggerTask(ctx, "proj", "disabled1")
	if err != nil {
		t.Fatalf("TriggerTask failed: %v", err)
	}
	if resp.Triggered {
		t.Error("expected Triggered=false for disabled schedule")
	}
	if resp.Reason != "schedule is disabled" {
		t.Errorf("Reason = %q, want %q", resp.Reason, "schedule is disabled")
	}
}

func TestTriggerTask_IneligibleStatus(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	ctx := context.Background()

	createProjectDir(t, brainDir, "proj")

	trueVal := true
	insertTaskNote(t, store, "pending1", "Pending Task", "pending", "medium", "proj", map[string]interface{}{
		"schedule":         "*/5 * * * *",
		"schedule_enabled": trueVal,
	})

	resp, err := svc.TriggerTask(ctx, "proj", "pending1")
	if err != nil {
		t.Fatalf("TriggerTask failed: %v", err)
	}
	if resp.Triggered {
		t.Error("expected Triggered=false for pending status")
	}
	if resp.Reason == "" {
		t.Error("expected non-empty Reason for ineligible status")
	}
}

func TestTriggerTask_MaxRunsReached(t *testing.T) {
	svc, store, brainDir := newTestTaskService(t)
	ctx := context.Background()

	createProjectDir(t, brainDir, "proj")

	trueVal := true
	maxRuns := 2
	insertTaskNote(t, store, "maxed1", "Max Runs Task", "active", "medium", "proj", map[string]interface{}{
		"schedule":         "*/5 * * * *",
		"schedule_enabled": trueVal,
		"max_runs":         maxRuns,
		"runs": []interface{}{
			map[string]interface{}{
				"run_id":    "run-001",
				"status":    "completed",
				"started":   "2025-01-01T00:00:00Z",
				"completed": "2025-01-01T00:05:00Z",
			},
			map[string]interface{}{
				"run_id":    "run-002",
				"status":    "completed",
				"started":   "2025-01-01T01:00:00Z",
				"completed": "2025-01-01T01:05:00Z",
			},
		},
	})

	resp, err := svc.TriggerTask(ctx, "proj", "maxed1")
	if err != nil {
		t.Fatalf("TriggerTask failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Triggered {
		t.Error("expected Triggered=false when max_runs reached")
	}
	if resp.Reason == "" {
		t.Error("expected non-empty Reason for max_runs")
	}
	if !strings.Contains(resp.Reason, "max_runs") {
		t.Errorf("Reason = %q, expected to contain 'max_runs'", resp.Reason)
	}
}

func TestTriggerTask_NotFound(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()

	createProjectDir(t, brainDir, "proj")

	_, err := svc.TriggerTask(ctx, "proj", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

func TestTaskServiceImpl_ImplementsInterface(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	var _ api.TaskService = svc
}

// ---------------------------------------------------------------------------
// Stale claim handling
// ---------------------------------------------------------------------------

func TestClaimTask_StaleClaim(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// Manually insert a stale claim (11 minutes ago)
	key := claimKey("proj", "task1")
	svc.mu.Lock()
	svc.claims[key] = &types.TaskClaim{
		RunnerID:  "old-runner",
		ClaimedAt: time.Now().Add(-11 * time.Minute).UnixMilli(),
	}
	svc.mu.Unlock()

	// New runner should be able to claim the stale task
	resp, err := svc.ClaimTask(ctx, "proj", "task1", "new-runner")
	if err != nil {
		t.Fatalf("ClaimTask on stale claim failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true for stale claim override")
	}
	if resp.RunnerID != "new-runner" {
		t.Errorf("RunnerID = %q, want %q", resp.RunnerID, "new-runner")
	}
}

// ---------------------------------------------------------------------------
// Integration: metadata flows through to dependency resolution
// ---------------------------------------------------------------------------

func TestGetTasks_MetadataFlowsToResolution(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "aaa11111", "Task A", "completed", "high", "proj", map[string]interface{}{
		"workdir":    "/home/user/project",
		"git_branch": "main",
	})
	insertTaskNote(t, store, "bbb22222", "Task B", "pending", "medium", "proj", map[string]interface{}{
		"depends_on":    []interface{}{"aaa11111"},
		"direct_prompt": "implement feature X",
		"agent":         "dev",
		"model":         "claude-4",
	})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	// Find task B
	var taskB *types.ResolvedTask
	for i := range result.Tasks {
		if result.Tasks[i].ID == "bbb22222" {
			taskB = &result.Tasks[i]
			break
		}
	}
	if taskB == nil {
		t.Fatal("task B not found in results")
	}

	// Verify metadata fields survived the conversion
	if taskB.DirectPrompt != "implement feature X" {
		t.Errorf("DirectPrompt = %q, want %q", taskB.DirectPrompt, "implement feature X")
	}
	if taskB.Agent != "dev" {
		t.Errorf("Agent = %q, want %q", taskB.Agent, "dev")
	}
	if taskB.Model != "claude-4" {
		t.Errorf("Model = %q, want %q", taskB.Model, "claude-4")
	}

	// Verify dependency resolution worked
	if taskB.Classification != "ready" {
		t.Errorf("Classification = %q, want %q (dep is completed)", taskB.Classification, "ready")
	}
	if len(taskB.ResolvedDeps) != 1 || taskB.ResolvedDeps[0] != "aaa11111" {
		t.Errorf("ResolvedDeps = %v, want [aaa11111]", taskB.ResolvedDeps)
	}
}

// ---------------------------------------------------------------------------
// Feature Checkout Tests
// ---------------------------------------------------------------------------

// TestCheckoutFeature_CreatesCheckoutTask tests that CheckoutFeature creates
// a checkout task with proper frontmatter.
func TestCheckoutFeature_CreatesCheckoutTask(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()
	projectID := "test-project"
	featureID := "feature-123"

	// Create project task directory
	taskDir := filepath.Join(brainDir, "projects", projectID, "task")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	// Create a non-generated feature task
	task1Path := filepath.Join(taskDir, "abc12def.md")
	task1Content := `---
type: task
title: Implement auth
status: pending
priority: high
feature_id: feature-123
---
Task content`
	if err := os.WriteFile(task1Path, []byte(task1Content), 0644); err != nil {
		t.Fatalf("failed to write task: %v", err)
	}

	// Call CheckoutFeature
	opts := &types.FeatureCheckoutOptions{
		ExecutionBranch:    "feature/auth",
		MergeTargetBranch:  "main",
		MergePolicy:        "auto_merge",
		MergeStrategy:      "squash",
		RemoteBranchPolicy: "delete",
		OpenPRBeforeMerge:  true,
		ExecutionMode:      "worktree",
	}

	result, err := svc.CheckoutFeature(ctx, projectID, featureID, opts)
	if err != nil {
		t.Fatalf("CheckoutFeature failed: %v", err)
	}

	// Verify result
	if !result.Created {
		t.Error("expected result.Created = true")
	}
	if result.GeneratedKey != "feature-checkout:feature-123:round-1" {
		t.Errorf("GeneratedKey = %q, want %q", result.GeneratedKey, "feature-checkout:feature-123:round-1")
	}
	if result.Task == nil {
		t.Fatal("expected result.Task to be non-nil")
	}

	// Verify the task file was created
	files, err := os.ReadDir(taskDir)
	if err != nil {
		t.Fatalf("failed to read task dir: %v", err)
	}

	var checkoutFiles []string
	for _, f := range files {
		if f.Name() != "abc12def.md" {
			checkoutFiles = append(checkoutFiles, f.Name())
		}
	}

	if len(checkoutFiles) != 1 {
		t.Fatalf("expected 1 checkout task file, got %d", len(checkoutFiles))
	}

	// Read and verify the checkout task content
	checkoutPath := filepath.Join(taskDir, checkoutFiles[0])
	content, err := os.ReadFile(checkoutPath)
	if err != nil {
		t.Fatalf("failed to read checkout task: %v", err)
	}

	contentStr := string(content)

	// Verify key frontmatter fields (checking for presence, not exact format)
	// YAML Serialize may quote strings with special chars
	expectedSubstrings := []string{
		"Feature checkout: feature-123", // title content (may be quoted)
		"type: task",
		"status: pending",
		"priority: medium",
		"feature_id: feature-123",
		"abc12def",    // depends_on entry (may be quoted)
		"checkout",    // tag
		"feature-123", // tag
		"generated: true",
		"generated_kind: feature_checkout",
		"feature-checkout:feature-123:round-1", // generated_key value
		"generated_by: feature-checkout",
		"git_branch: feature/auth",
		"merge_target_branch: main",
		"merge_policy: auto_merge",
		"merge_strategy: squash",
		"remote_branch_policy: delete",
		"open_pr_before_merge: true",
		"execution_mode: worktree",
	}

	for _, expected := range expectedSubstrings {
		if !contains(contentStr, expected) {
			t.Errorf("checkout task missing expected substring: %q\nFull content:\n%s", expected, contentStr)
		}
	}

	// Verify content body
	if !contains(contentStr, "Automated feature checkout for feature-123") {
		t.Error("checkout task missing expected content body")
	}
	if !contains(contentStr, "Merge intent:") {
		t.Error("checkout task missing merge intent section")
	}
}

// TestCheckoutFeature_Idempotency tests that calling CheckoutFeature twice
// returns the existing task.
func TestCheckoutFeature_Idempotency(t *testing.T) {
	svc, _, brainDir := newTestTaskService(t)
	ctx := context.Background()
	projectID := "test-project"
	featureID := "feature-456"

	taskDir := filepath.Join(brainDir, "projects", projectID, "task")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	// Create a feature task
	task1Path := filepath.Join(taskDir, "xyz98765.md")
	task1Content := `---
type: task
title: Add logging
status: pending
feature_id: feature-456
---
Task content`
	if err := os.WriteFile(task1Path, []byte(task1Content), 0644); err != nil {
		t.Fatalf("failed to write task: %v", err)
	}

	opts := &types.FeatureCheckoutOptions{
		MergePolicy: "prompt_only",
	}

	// First call - should create
	result1, err := svc.CheckoutFeature(ctx, projectID, featureID, opts)
	if err != nil {
		t.Fatalf("CheckoutFeature (1st) failed: %v", err)
	}
	if !result1.Created {
		t.Error("expected result1.Created = true")
	}

	// Second call - should return existing
	result2, err := svc.CheckoutFeature(ctx, projectID, featureID, opts)
	if err != nil {
		t.Fatalf("CheckoutFeature (2nd) failed: %v", err)
	}
	if result2.Created {
		t.Error("expected result2.Created = false (should return existing)")
	}
	if result2.Task == nil {
		t.Fatal("expected result2.Task to be non-nil")
	}

	// Verify no duplicate files
	files, err := os.ReadDir(taskDir)
	if err != nil {
		t.Fatalf("failed to read task dir: %v", err)
	}

	checkoutCount := 0
	for _, f := range files {
		if f.Name() != "xyz98765.md" {
			checkoutCount++
		}
	}
	if checkoutCount != 1 {
		t.Errorf("expected 1 checkout task, got %d", checkoutCount)
	}
}

// TestExtractUniqueNonGeneratedTaskIds tests the helper function.
func TestExtractUniqueNonGeneratedTaskIds(t *testing.T) {
	trueVal := true
	falseVal := false

	tasks := []types.BrainEntry{
		{ID: "task1", Generated: &falseVal},
		{ID: "task2", Generated: &falseVal},
		{ID: "task3", Generated: &trueVal},  // should be excluded
		{ID: "task1", Generated: &falseVal}, // duplicate
		{ID: "", Generated: &falseVal},      // empty ID
	}

	result := extractUniqueNonGeneratedTaskIds(tasks)

	expected := []string{"task1", "task2"}
	if len(result) != len(expected) {
		t.Fatalf("length = %d, want %d", len(result), len(expected))
	}

	for i, id := range expected {
		if result[i] != id {
			t.Errorf("result[%d] = %q, want %q", i, result[i], id)
		}
	}
}

// TestIsTerminalCheckoutStatus tests the terminal status check.
func TestIsTerminalCheckoutStatus(t *testing.T) {
	tests := []struct {
		status   string
		terminal bool
	}{
		{"pending", false},
		{"active", false},
		{"in_progress", false},
		{"blocked", false},
		{"completed", true},
		{"validated", true},
		{"cancelled", true},
		{"superseded", true},
		{"archived", true},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			result := isTerminalCheckoutStatus(tt.status)
			if result != tt.terminal {
				t.Errorf("isTerminalCheckoutStatus(%q) = %v, want %v", tt.status, result, tt.terminal)
			}
		})
	}
}

// TestExtractGeneratedDependentTasks tests extraction of checkout/review tasks.
func TestExtractGeneratedDependentTasks(t *testing.T) {
	trueVal := true
	falseVal := false

	tasks := []types.BrainEntry{
		{ID: "task1", Generated: &falseVal},
		{ID: "checkout1", Generated: &trueVal, GeneratedKind: "feature_checkout"},
		{ID: "review1", Generated: &trueVal, GeneratedKind: "feature_review"},
		{ID: "gap1", Generated: &trueVal, GeneratedKind: "gap_task"}, // excluded
		{ID: "checkout2", Generated: &trueVal, GeneratedKind: "feature_checkout"},
	}

	result := extractGeneratedDependentTasks(tasks)

	expected := []string{"checkout1", "review1", "checkout2"}
	if len(result) != len(expected) {
		t.Fatalf("length = %d, want %d", len(result), len(expected))
	}

	for i, id := range expected {
		if result[i].ID != id {
			t.Errorf("result[%d].ID = %q, want %q", i, result[i].ID, id)
		}
	}
}

// contains is a helper to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
