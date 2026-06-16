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

func insertRunnerForTaskSelectionTest(t *testing.T, store *storage.StorageLayer, runnerID string, executors, capabilities []string) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := store.UpsertRunner(context.Background(), &storage.RunnerRow{
		RunnerID:      runnerID,
		Hostname:      runnerID + "-host",
		Labels:        map[string]string{},
		Executors:     executors,
		Capabilities:  capabilities,
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	}); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}
}

func assertTaskIDs(t *testing.T, tasks []types.ResolvedTask, expected ...string) {
	t.Helper()
	if len(tasks) != len(expected) {
		t.Fatalf("expected %d tasks %v, got %d: %v", len(expected), expected, len(tasks), taskIDs(tasks))
	}
	for i, id := range expected {
		if tasks[i].ID != id {
			t.Fatalf("task[%d].ID = %q, want %q (all: %v)", i, tasks[i].ID, id, taskIDs(tasks))
		}
	}
}

func taskIDs(tasks []types.ResolvedTask) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
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

func TestGetTask_ResolvesBuiltinMonitorPromptFromTag(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	projectID := "brain-api"
	stalePrompt := "stale prompt copied when monitor was created"

	insertTaskNote(t, store, "dream01", "Monitor: Dream Consolidation (project brain-api)", "pending", "medium", projectID, map[string]interface{}{
		"tags":             []interface{}{"scheduled", "dream", "monitor:dream:project:brain-api"},
		"direct_prompt":    stalePrompt,
		"schedule":         "0 3 * * *",
		"schedule_enabled": true,
	})

	task, err := svc.GetTask(context.Background(), projectID, "dream01")
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}

	if task.DirectPrompt == stalePrompt {
		t.Fatal("expected built-in monitor prompt to be resolved live, got stale stored direct_prompt")
	}
	if !strings.Contains(task.DirectPrompt, "You are the **Dream Consolidator**") {
		t.Fatalf("DirectPrompt does not contain current dream prompt: %q", task.DirectPrompt)
	}
	if !strings.Contains(task.DirectPrompt, `project: "brain-api"`) {
		t.Fatalf("DirectPrompt does not include project scope: %q", task.DirectPrompt)
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

func TestGetReady_WithRunnerIDExcludesTasksMissingCapabilities(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerForTaskSelectionTest(t, store, "runner-1", []string{"opencode"}, []string{"docker"})
	insertTaskNote(t, store, "needgpu1", "Needs GPU", "pending", "high", "proj", map[string]interface{}{
		"requires_capability": []interface{}{"gpu"},
	})
	insertTaskNote(t, store, "plain111", "No Capability Required", "pending", "medium", "proj", map[string]interface{}{})

	ready, err := svc.GetReady(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-1"})
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	assertTaskIDs(t, ready, "plain111")
}

func TestGetReady_WithRunnerIDIncludesTasksWhenCapabilitiesSatisfied(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerForTaskSelectionTest(t, store, "runner-1", []string{"opencode"}, []string{"docker", "gpu"})
	insertTaskNote(t, store, "capok111", "Needs Docker And GPU", "pending", "high", "proj", map[string]interface{}{
		"requires_capability": []interface{}{"docker", "gpu"},
	})

	ready, err := svc.GetReady(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-1"})
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	assertTaskIDs(t, ready, "capok111")
}

func TestGetReady_ExplicitExecutorsCombineWithRunnerEligibility(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerForTaskSelectionTest(t, store, "runner-1", []string{"pi"}, []string{"gpu"})
	insertTaskNote(t, store, "piok1111", "Pi GPU Task", "pending", "high", "proj", map[string]interface{}{
		"executor":            "pi",
		"requires_capability": []interface{}{"gpu"},
	})
	insertTaskNote(t, store, "opencode", "OpenCode GPU Task", "pending", "medium", "proj", map[string]interface{}{
		"executor":            "opencode",
		"requires_capability": []interface{}{"gpu"},
	})
	insertTaskNote(t, store, "pimiss11", "Pi CPU Missing Capability", "pending", "low", "proj", map[string]interface{}{
		"executor":            "pi",
		"requires_capability": []interface{}{"docker"},
	})

	ready, err := svc.GetReady(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-1", Executors: []string{"pi"}})
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	assertTaskIDs(t, ready, "piok1111")
}

func TestGetReady_NoRunnerContextPreservesCapabilityAgnosticBehavior(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "needgpu1", "Needs GPU", "pending", "high", "proj", map[string]interface{}{
		"requires_capability": []interface{}{"gpu"},
	})
	insertTaskNote(t, store, "plain111", "No Capability Required", "pending", "medium", "proj", map[string]interface{}{})

	ready, err := svc.GetReady(ctx, "proj", nil)
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	assertTaskIDs(t, ready, "needgpu1", "plain111")

	next, err := svc.GetNext(ctx, "proj", nil)
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil || next.ID != "needgpu1" {
		t.Fatalf("GetNext returned %v, want needgpu1", next)
	}
}

func TestGetReady_MissingRunnerIDPreservesOldBehavior(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "needgpu1", "Needs GPU", "pending", "high", "proj", map[string]interface{}{
		"requires_capability": []interface{}{"gpu"},
	})

	ready, err := svc.GetReady(ctx, "proj", &api.TaskFilterOptions{RunnerID: "missing-runner"})
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	assertTaskIDs(t, ready, "needgpu1")
}

func TestGetReady_WithRunnerIDExcludesFeaturesAssignedToOtherRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerForTaskSelectionTest(t, store, "runner-1", []string{"opencode"}, nil)
	insertRunnerForTaskSelectionTest(t, store, "runner-2", []string{"opencode"}, nil)
	if _, err := store.ForceAssignFeature(ctx, "proj", "feature-other", "runner-2", "test", "active"); err != nil {
		t.Fatalf("ForceAssignFeature other failed: %v", err)
	}
	if _, err := store.ForceAssignFeature(ctx, "proj", "feature-own", "runner-1", "test", "active"); err != nil {
		t.Fatalf("ForceAssignFeature own failed: %v", err)
	}
	insertTaskNote(t, store, "other111", "Other Runner Feature", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feature-other",
	})
	insertTaskNote(t, store, "own11111", "Own Runner Feature", "pending", "medium", "proj", map[string]interface{}{
		"feature_id": "feature-own",
	})
	insertTaskNote(t, store, "free1111", "Unassigned Feature", "pending", "low", "proj", map[string]interface{}{
		"feature_id": "feature-free",
	})

	ready, err := svc.GetReady(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-1"})
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	assertTaskIDs(t, ready, "own11111", "free1111")
}

func TestGetNext_WithRunnerIDSkipsHigherPriorityFeatureAssignedToOtherRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerForTaskSelectionTest(t, store, "runner-1", []string{"opencode"}, nil)
	insertRunnerForTaskSelectionTest(t, store, "runner-2", []string{"opencode"}, nil)
	if _, err := store.ForceAssignFeature(ctx, "proj", "feature-other", "runner-2", "test", "active"); err != nil {
		t.Fatalf("ForceAssignFeature other failed: %v", err)
	}
	insertTaskNote(t, store, "other111", "Other Runner Feature", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feature-other",
	})
	insertTaskNote(t, store, "free1111", "Unassigned Feature", "pending", "medium", "proj", map[string]interface{}{
		"feature_id": "feature-free",
	})

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-1"})
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil || next.ID != "free1111" {
		t.Fatalf("GetNext returned %v, want free1111", next)
	}
}

func TestClaimTask_AssignsFeatureToFirstRunnerAndBlocksOtherFeatureTasks(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "task1111", "First feature task", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feature-auth",
	})
	insertTaskNote(t, store, "task2222", "Second feature task", "pending", "medium", "proj", map[string]interface{}{
		"feature_id": "feature-auth",
	})

	first, err := svc.ClaimTask(ctx, "proj", "task1111", "runner-a")
	if err != nil {
		t.Fatalf("runner-a first claim failed: %v", err)
	}
	if !first.Success {
		t.Fatalf("runner-a first claim success = false: %+v", first)
	}

	assignment, err := store.GetFeatureAssignment(ctx, "proj", "feature-auth")
	if err != nil {
		t.Fatalf("GetFeatureAssignment failed: %v", err)
	}
	if assignment == nil || assignment.RunnerID != "runner-a" || assignment.Source != "auto" || assignment.Status != "active" {
		t.Fatalf("feature assignment = %+v, want runner-a auto active", assignment)
	}

	second, err := svc.ClaimTask(ctx, "proj", "task2222", "runner-b")
	if err != api.ErrConflict {
		t.Fatalf("runner-b second task claim error = %v, want ErrConflict", err)
	}
	if second == nil || second.Success || second.ClaimedBy != "runner-a" {
		t.Fatalf("runner-b conflict response = %+v, want claimed_by runner-a", second)
	}
	status, err := svc.GetClaimStatus(ctx, "proj", "task2222")
	if err != nil {
		t.Fatalf("GetClaimStatus task2222 failed: %v", err)
	}
	if status.Claimed {
		t.Fatalf("runner-b feature conflict should release task2222 claim, got %+v", status)
	}

	second, err = svc.ClaimTask(ctx, "proj", "task2222", "runner-a")
	if err != nil {
		t.Fatalf("assigned runner should claim remaining feature task: %v", err)
	}
	if !second.Success {
		t.Fatalf("runner-a second claim success = false: %+v", second)
	}
}

func TestManualFeatureAssignmentRoutesTasksToSelectedRunnerAndReassignment(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	insertFeatureAssignmentRunnerForFeatureTest(t, store, "runner-a", now)
	insertFeatureAssignmentRunnerForFeatureTest(t, store, "runner-b", now)
	insertTaskNote(t, store, "auth1111", "Auth task", "pending", "high", "proj", map[string]interface{}{
		"feature_id": "feature-auth",
	})

	if _, err := svc.AssignFeatureToRunner(ctx, "proj", "feature-auth", types.FeatureAssignmentRequest{RunnerID: "runner-a", Intent: "assign"}); err != nil {
		t.Fatalf("assign feature to runner-a failed: %v", err)
	}

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-a"})
	if err != nil {
		t.Fatalf("runner-a GetNext failed: %v", err)
	}
	if next == nil || next.ID != "auth1111" {
		t.Fatalf("runner-a GetNext = %v, want auth1111", next)
	}

	next, err = svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-b"})
	if err != nil {
		t.Fatalf("runner-b GetNext failed: %v", err)
	}
	if next != nil {
		t.Fatalf("runner-b should not receive runner-a assignment, got %v", next)
	}

	resp, err := svc.AssignFeatureToRunner(ctx, "proj", "feature-auth", types.FeatureAssignmentRequest{RunnerID: "runner-b", Intent: "reassign"})
	if err != nil {
		t.Fatalf("reassign feature to runner-b failed: %v", err)
	}
	if resp.RunnerID != "runner-b" || resp.PreviousRunner != "runner-a" {
		t.Fatalf("reassign response = %+v, want runner-b previous runner-a", resp)
	}

	next, err = svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-a"})
	if err != nil {
		t.Fatalf("runner-a GetNext after reassign failed: %v", err)
	}
	if next != nil {
		t.Fatalf("runner-a should not receive reassigned feature, got %v", next)
	}

	next, err = svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-b"})
	if err != nil {
		t.Fatalf("runner-b GetNext after reassign failed: %v", err)
	}
	if next == nil || next.ID != "auth1111" {
		t.Fatalf("runner-b GetNext after reassign = %v, want auth1111", next)
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

	// A task depending on a "blocked" dep is classified as "waiting", not "blocked".
	// Only cancelled deps and cycles cause hard-blocking.
	// So GetBlocked should NOT return bbb22222 here.
	insertTaskNote(t, store, "aaa11111", "Blocked Dep", "blocked", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "bbb22222", "Waiting Task", "pending", "low", "proj", map[string]interface{}{
		"depends_on": []interface{}{"aaa11111"},
	})

	blocked, err := svc.GetBlocked(ctx, "proj")
	if err != nil {
		t.Fatalf("GetBlocked failed: %v", err)
	}
	if len(blocked) != 0 {
		t.Fatalf("expected 0 blocked tasks (blocked deps cause waiting, not blocking), got %d", len(blocked))
	}
}

func TestGetBlocked_CancelledDepCausesBlocked(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	// A task depending on a "cancelled" dep IS hard-blocked.
	insertTaskNote(t, store, "aaa11111", "Cancelled Dep", "cancelled", "high", "proj", map[string]interface{}{})
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

func TestGetNext_SkipsActiveDispatchLeaseOwnedByOtherRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()
	now := time.Now()

	insertTaskNote(t, store, "high1111", "High Task", "pending", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "low11111", "Low Task", "pending", "low", "proj", map[string]interface{}{})
	_, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:        "proj",
		TaskID:           "high1111",
		AssignedRunnerID: "runner-assigned",
		PushedAt:         now.UnixMilli(),
		ExpiresAt:        now.Add(time.Minute).UnixMilli(),
	})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-other"})
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected non-nil task")
	}
	if next.ID != "low11111" {
		t.Errorf("next.ID = %q, want %q", next.ID, "low11111")
	}
}

func TestGetNext_AllowsActiveDispatchLeaseOwnedByRequestingRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()
	now := time.Now()

	insertTaskNote(t, store, "high1111", "High Task", "pending", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "low11111", "Low Task", "pending", "low", "proj", map[string]interface{}{})
	_, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:        "proj",
		TaskID:           "high1111",
		AssignedRunnerID: "runner-assigned",
		PushedAt:         now.UnixMilli(),
		ExpiresAt:        now.Add(time.Minute).UnixMilli(),
	})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-assigned"})
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

func TestGetNext_IgnoresExpiredDispatchLeaseForOtherRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()
	now := time.Now()

	insertTaskNote(t, store, "high1111", "High Task", "pending", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "low11111", "Low Task", "pending", "low", "proj", map[string]interface{}{})
	_, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:        "proj",
		TaskID:           "high1111",
		AssignedRunnerID: "runner-assigned",
		PushedAt:         now.Add(-2 * time.Minute).UnixMilli(),
		ExpiresAt:        now.Add(-time.Minute).UnixMilli(),
	})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-other"})
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

func TestClaimTask_ConflictsWhenActiveDispatchLeaseOwnedByOtherRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()
	now := time.Now()

	_, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:        "proj",
		TaskID:           "task1",
		AssignedRunnerID: "runner-assigned",
		PushedAt:         now.UnixMilli(),
		ExpiresAt:        now.Add(time.Minute).UnixMilli(),
	})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}

	resp, err := svc.ClaimTask(ctx, "proj", "task1", "runner-other")
	if err != api.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if resp == nil {
		t.Fatal("expected conflict response")
	}
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.ClaimedBy != "runner-assigned" {
		t.Errorf("ClaimedBy = %q, want %q", resp.ClaimedBy, "runner-assigned")
	}
}

func TestClaimTask_AllowsActiveDispatchLeaseOwnedByRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()
	now := time.Now()

	_, ok, err := store.CreateDispatchLease(ctx, storage.DispatchLeaseCreate{
		ProjectID:        "proj",
		TaskID:           "task1",
		AssignedRunnerID: "runner-assigned",
		PushedAt:         now.UnixMilli(),
		ExpiresAt:        now.Add(time.Minute).UnixMilli(),
	})
	if err != nil || !ok {
		t.Fatalf("CreateDispatchLease ok=%v err=%v", ok, err)
	}

	resp, err := svc.ClaimTask(ctx, "proj", "task1", "runner-assigned")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
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
// RenewClaim
// ---------------------------------------------------------------------------

func TestRenewClaim_Success(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// Claim a task first
	_, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	// Renew the claim
	resp, err := svc.RenewClaim(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("RenewClaim failed: %v", err)
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
	if resp.ExpiresAt == "" {
		t.Error("expected non-empty ExpiresAt")
	}
}

func TestRenewClaim_NotFound(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// Renew a claim that doesn't exist
	resp, err := svc.RenewClaim(ctx, "proj", "nonexistent", "runner-1")
	if err != api.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if resp.Success {
		t.Error("expected success=false")
	}
}

func TestRenewClaim_WrongRunner(t *testing.T) {
	svc, _, _ := newTestTaskService(t)
	ctx := context.Background()

	// Claim as runner-1
	_, err := svc.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}

	// Renew as runner-2 — should be rejected
	resp, err := svc.RenewClaim(ctx, "proj", "task1", "runner-2")
	if err != api.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if resp.Success {
		t.Error("expected success=false")
	}
	if resp.Error != "claim owned by different runner" {
		t.Errorf("Error = %q, want %q", resp.Error, "claim owned by different runner")
	}
}

func TestRenewClaim_ExpiredClaim(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	// Seed an expired claim via storage (1ms lease)
	ok, _, err := store.ClaimTask(ctx, "proj", "task1", "runner-1", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("seed claim failed: %v", err)
	}
	if !ok {
		t.Fatal("seed claim should succeed")
	}

	// Wait for expiry
	time.Sleep(5 * time.Millisecond)

	// Renew should fail with not found (expired)
	resp, err := svc.RenewClaim(ctx, "proj", "task1", "runner-1")
	if err != api.ErrNotFound {
		t.Fatalf("expected ErrNotFound for expired claim, got %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for expired claim")
	}
	if resp.Error != "claim expired" {
		t.Errorf("Error = %q, want %q", resp.Error, "claim expired")
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
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	// Insert a claim via storage with a very short lease so it's already expired
	// Use a 1ms lease duration which will expire immediately
	ok, _, err := store.ClaimTask(ctx, "proj", "task1", "old-runner", 1*time.Millisecond)
	if err != nil {
		t.Fatalf("seed claim failed: %v", err)
	}
	if !ok {
		t.Fatal("seed claim should succeed")
	}

	// Wait for the claim to expire
	time.Sleep(5 * time.Millisecond)

	// New runner should be able to claim the expired task
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

// TestClaimTask_PersistsSurvivesRestart verifies claims survive service recreation.
func TestClaimTask_PersistsSurvivesRestart(t *testing.T) {
	// Create first service instance
	svc1, store, brainDir := newTestTaskService(t)
	ctx := context.Background()

	// Claim a task
	resp, err := svc1.ClaimTask(ctx, "proj", "task1", "runner-1")
	if err != nil {
		t.Fatalf("ClaimTask failed: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success=true")
	}

	// Create a NEW service instance (simulating restart) with same storage
	cfg := &config.Config{BrainDir: brainDir}
	svc2 := NewTaskService(cfg, store)

	// Verify the claim persists in the new instance
	status, err := svc2.GetClaimStatus(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetClaimStatus failed: %v", err)
	}
	if !status.Claimed {
		t.Error("expected claimed=true after restart")
	}
	if status.RunnerID != "runner-1" {
		t.Errorf("RunnerID = %q, want %q", status.RunnerID, "runner-1")
	}

	// Verify the claim blocks other runners in the new instance
	resp2, err := svc2.ClaimTask(ctx, "proj", "task1", "runner-2")
	if err != api.ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
	if resp2.Success {
		t.Error("expected success=false after restart")
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

func TestGetTasks_MetadataRunFinalizationsFlowToEntries(t *testing.T) {
	metaJSON, err := json.Marshal(map[string]interface{}{
		"run_finalizations": map[string]interface{}{
			"run1": map[string]interface{}{
				"status":       "completed",
				"finalized_at": "2026-06-10T21:00:00Z",
				"session_id":   "ses_finalized",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	entryType := "automation"
	status := "active"
	priority := "medium"
	project := "proj"
	row := &storage.NoteRow{
		Path:      "projects/proj/automation/auto1111.md",
		ShortID:   "auto1111",
		Title:     "Automation",
		Type:      &entryType,
		Status:    &status,
		Priority:  &priority,
		ProjectID: &project,
		Metadata:  string(metaJSON),
	}

	entry := NoteRowToBrainEntry(row)
	finalization, ok := entry.RunFinalizations["run1"]
	if !ok {
		t.Fatalf("missing run finalization in %#v", entry.RunFinalizations)
	}
	if finalization.SessionID != "ses_finalized" {
		t.Fatalf("SessionID = %q, want ses_finalized", finalization.SessionID)
	}
	if finalization.Status != "completed" {
		t.Fatalf("Status = %q, want completed", finalization.Status)
	}
	if finalization.FinalizedAt != "2026-06-10T21:00:00Z" {
		t.Fatalf("FinalizedAt = %q, want timestamp", finalization.FinalizedAt)
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

// newTestTaskServiceWithDefaults creates a TaskServiceImpl with pre-configured TaskDefaults.
func newTestTaskServiceWithDefaults(t *testing.T, defaults config.TaskDefaultsConfig) (*TaskServiceImpl, *storage.StorageLayer, string) {
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
	cfg := &config.Config{
		BrainDir:     brainDir,
		TaskDefaults: defaults,
	}

	svc := NewTaskService(cfg, store)
	return svc, store, brainDir
}

// ---------------------------------------------------------------------------
// applyTaskDefaults
// ---------------------------------------------------------------------------

func TestApplyTaskDefaults_FillsEmptyStringFields(t *testing.T) {
	trueVal := true
	defaults := config.TaskDefaultsConfig{
		Agent:              "tdd-dev",
		Model:              "claude-sonnet-4-20250514",
		ExecutionMode:      "worktree",
		CompleteOnIdle:     &trueVal,
		MergePolicy:        "auto_merge",
		MergeStrategy:      "squash",
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
		OpenPRBeforeMerge:  &trueVal,
		TargetWorkdir:      "/default/workdir",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	// Insert a task with NO execution fields set
	insertTaskNote(t, store, "empty111", "Empty Task", "pending", "high", "proj", map[string]interface{}{})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	if task.Agent != "tdd-dev" {
		t.Errorf("Agent = %q, want %q", task.Agent, "tdd-dev")
	}
	if task.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", task.Model, "claude-sonnet-4-20250514")
	}
	if task.ExecutionMode != "worktree" {
		t.Errorf("ExecutionMode = %q, want %q", task.ExecutionMode, "worktree")
	}
	if task.CompleteOnIdle == nil || !*task.CompleteOnIdle {
		t.Error("CompleteOnIdle should be true from defaults")
	}
	if task.MergePolicy != "auto_merge" {
		t.Errorf("MergePolicy = %q, want %q", task.MergePolicy, "auto_merge")
	}
	if task.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy = %q, want %q", task.MergeStrategy, "squash")
	}
	if task.MergeTargetBranch != "main" {
		t.Errorf("MergeTargetBranch = %q, want %q", task.MergeTargetBranch, "main")
	}
	if task.RemoteBranchPolicy != "delete" {
		t.Errorf("RemoteBranchPolicy = %q, want %q", task.RemoteBranchPolicy, "delete")
	}
	if task.OpenPRBeforeMerge == nil || !*task.OpenPRBeforeMerge {
		t.Error("OpenPRBeforeMerge should be true from defaults")
	}
	if task.TargetWorkdir != "/default/workdir" {
		t.Errorf("TargetWorkdir = %q, want %q", task.TargetWorkdir, "/default/workdir")
	}
}

func TestApplyTaskDefaults_TaskValuesWin(t *testing.T) {
	trueVal := true
	defaults := config.TaskDefaultsConfig{
		Agent:              "tdd-dev",
		Model:              "claude-sonnet-4-20250514",
		ExecutionMode:      "worktree",
		CompleteOnIdle:     &trueVal,
		MergePolicy:        "auto_merge",
		MergeStrategy:      "squash",
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
		OpenPRBeforeMerge:  &trueVal,
		TargetWorkdir:      "/default/workdir",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	// Insert a task with ALL fields explicitly set — these should win
	falseVal := false
	insertTaskNote(t, store, "full1111", "Full Task", "pending", "high", "proj", map[string]interface{}{
		"agent":                "explore",
		"model":                "claude-opus-4-20250514",
		"execution_mode":       "current_branch",
		"complete_on_idle":     falseVal,
		"merge_policy":         "prompt_only",
		"merge_strategy":       "rebase",
		"merge_target_branch":  "develop",
		"remote_branch_policy": "keep",
		"open_pr_before_merge": falseVal,
		"target_workdir":       "/task/specific/dir",
	})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	if task.Agent != "explore" {
		t.Errorf("Agent = %q, want %q (task value should win)", task.Agent, "explore")
	}
	if task.Model != "claude-opus-4-20250514" {
		t.Errorf("Model = %q, want %q (task value should win)", task.Model, "claude-opus-4-20250514")
	}
	if task.ExecutionMode != "current_branch" {
		t.Errorf("ExecutionMode = %q, want %q (task value should win)", task.ExecutionMode, "current_branch")
	}
	if task.CompleteOnIdle == nil || *task.CompleteOnIdle {
		t.Error("CompleteOnIdle should be false (task value should win)")
	}
	if task.MergePolicy != "prompt_only" {
		t.Errorf("MergePolicy = %q, want %q (task value should win)", task.MergePolicy, "prompt_only")
	}
	if task.MergeStrategy != "rebase" {
		t.Errorf("MergeStrategy = %q, want %q (task value should win)", task.MergeStrategy, "rebase")
	}
	if task.MergeTargetBranch != "develop" {
		t.Errorf("MergeTargetBranch = %q, want %q (task value should win)", task.MergeTargetBranch, "develop")
	}
	if task.RemoteBranchPolicy != "keep" {
		t.Errorf("RemoteBranchPolicy = %q, want %q (task value should win)", task.RemoteBranchPolicy, "keep")
	}
	if task.OpenPRBeforeMerge == nil || *task.OpenPRBeforeMerge {
		t.Error("OpenPRBeforeMerge should be false (task value should win)")
	}
	if task.TargetWorkdir != "/task/specific/dir" {
		t.Errorf("TargetWorkdir = %q, want %q (task value should win)", task.TargetWorkdir, "/task/specific/dir")
	}
}

func TestApplyTaskDefaults_NoOpWhenZeroValue(t *testing.T) {
	// Zero-value defaults — nothing should change
	svc, store, _ := newTestTaskServiceWithDefaults(t, config.TaskDefaultsConfig{})
	ctx := context.Background()

	insertTaskNote(t, store, "zero1111", "Zero Task", "pending", "high", "proj", map[string]interface{}{})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	// All fields should remain empty/nil
	if task.Agent != "" {
		t.Errorf("Agent = %q, want empty", task.Agent)
	}
	if task.Model != "" {
		t.Errorf("Model = %q, want empty", task.Model)
	}
	if task.ExecutionMode != "" {
		t.Errorf("ExecutionMode = %q, want empty", task.ExecutionMode)
	}
	if task.CompleteOnIdle != nil {
		t.Errorf("CompleteOnIdle = %v, want nil", task.CompleteOnIdle)
	}
	if task.MergePolicy != "" {
		t.Errorf("MergePolicy = %q, want empty", task.MergePolicy)
	}
	if task.MergeStrategy != "" {
		t.Errorf("MergeStrategy = %q, want empty", task.MergeStrategy)
	}
	if task.MergeTargetBranch != "" {
		t.Errorf("MergeTargetBranch = %q, want empty", task.MergeTargetBranch)
	}
	if task.RemoteBranchPolicy != "" {
		t.Errorf("RemoteBranchPolicy = %q, want empty", task.RemoteBranchPolicy)
	}
	if task.OpenPRBeforeMerge != nil {
		t.Errorf("OpenPRBeforeMerge = %v, want nil", task.OpenPRBeforeMerge)
	}
	if task.TargetWorkdir != "" {
		t.Errorf("TargetWorkdir = %q, want empty", task.TargetWorkdir)
	}
}

func TestApplyTaskDefaults_PartialDefaults(t *testing.T) {
	// Only some defaults set
	defaults := config.TaskDefaultsConfig{
		Agent: "tdd-dev",
		Model: "claude-sonnet-4-20250514",
		// Everything else is zero-value
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	insertTaskNote(t, store, "part1111", "Partial Task", "pending", "high", "proj", map[string]interface{}{
		"agent": "explore", // This should win over default
	})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	// Agent should keep task value
	if task.Agent != "explore" {
		t.Errorf("Agent = %q, want %q (task value should win)", task.Agent, "explore")
	}
	// Model should get default since task has none
	if task.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q (should get default)", task.Model, "claude-sonnet-4-20250514")
	}
	// ExecutionMode should remain empty (no default, no task value)
	if task.ExecutionMode != "" {
		t.Errorf("ExecutionMode = %q, want empty", task.ExecutionMode)
	}
}

func TestApplyTaskDefaults_AppliedToAllTasks(t *testing.T) {
	defaults := config.TaskDefaultsConfig{
		Agent: "tdd-dev",
		Model: "claude-sonnet-4-20250514",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	// Insert multiple tasks — defaults should apply to ALL of them, not just the first
	insertTaskNote(t, store, "multi_a1", "Task A", "pending", "high", "proj", map[string]interface{}{})
	insertTaskNote(t, store, "multi_b1", "Task B", "pending", "medium", "proj", map[string]interface{}{
		"agent": "explore", // This task has its own agent — should keep it
	})
	insertTaskNote(t, store, "multi_c1", "Task C", "pending", "low", "proj", map[string]interface{}{})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(result.Tasks))
	}

	// Build a map by ID for easier assertions
	byID := make(map[string]types.ResolvedTask)
	for _, task := range result.Tasks {
		byID[task.ID] = task
	}

	// Task A: both defaults applied
	if a := byID["multi_a1"]; a.Agent != "tdd-dev" || a.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Task A: Agent=%q Model=%q, want tdd-dev/claude-sonnet-4-20250514", a.Agent, a.Model)
	}
	// Task B: agent from task wins, model from defaults
	if b := byID["multi_b1"]; b.Agent != "explore" || b.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Task B: Agent=%q Model=%q, want explore/claude-sonnet-4-20250514", b.Agent, b.Model)
	}
	// Task C: both defaults applied
	if c := byID["multi_c1"]; c.Agent != "tdd-dev" || c.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Task C: Agent=%q Model=%q, want tdd-dev/claude-sonnet-4-20250514", c.Agent, c.Model)
	}
}

func TestApplyTaskDefaults_AppliedViaGetReady(t *testing.T) {
	defaults := config.TaskDefaultsConfig{
		Agent: "tdd-dev",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	insertTaskNote(t, store, "ready111", "Ready Task", "pending", "high", "proj", map[string]interface{}{})

	ready, err := svc.GetReady(ctx, "proj", nil)
	if err != nil {
		t.Fatalf("GetReady failed: %v", err)
	}
	if len(ready) != 1 {
		t.Fatalf("expected 1 ready task, got %d", len(ready))
	}
	if ready[0].Agent != "tdd-dev" {
		t.Errorf("Agent = %q, want %q (defaults should apply via GetReady)", ready[0].Agent, "tdd-dev")
	}
}

func TestApplyTaskDefaults_AppliedViaGetNext(t *testing.T) {
	defaults := config.TaskDefaultsConfig{
		Agent: "tdd-dev",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	insertTaskNote(t, store, "next1111", "Next Task", "pending", "high", "proj", map[string]interface{}{})

	next, err := svc.GetNext(ctx, "proj", nil)
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected non-nil task")
	}
	if next.Agent != "tdd-dev" {
		t.Errorf("Agent = %q, want %q (defaults should apply via GetNext)", next.Agent, "tdd-dev")
	}
}

func TestApplyTaskDefaults_AppliedViaGetMultiTaskStatus(t *testing.T) {
	defaults := config.TaskDefaultsConfig{
		Agent: "tdd-dev",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	insertTaskNote(t, store, "multi111", "Multi Task", "pending", "high", "proj", map[string]interface{}{})

	resp, err := svc.GetMultiTaskStatus(ctx, "proj", types.MultiTaskStatusRequest{
		TaskIDs: []string{"multi111"},
	})
	if err != nil {
		t.Fatalf("GetMultiTaskStatus failed: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(resp.Tasks))
	}
	if resp.Tasks[0].Agent != "tdd-dev" {
		t.Errorf("Agent = %q, want %q (defaults should apply via GetMultiTaskStatus)", resp.Tasks[0].Agent, "tdd-dev")
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

// ---------------------------------------------------------------------------
// Tests: applyTaskDefaults
// ---------------------------------------------------------------------------

func TestApplyTaskDefaults_EmptyTaskGetsDefaults(t *testing.T) {
	// Task has no agent/model/etc set; config provides defaults.
	// Expected: all empty fields filled from defaults.
	trueVal := true
	defaults := config.TaskDefaultsConfig{
		Agent:              "tdd-dev",
		Model:              "sonnet",
		ExecutionMode:      "worktree",
		CompleteOnIdle:     &trueVal,
		MergePolicy:        "auto_merge",
		MergeStrategy:      "squash",
		MergeTargetBranch:  "main",
		RemoteBranchPolicy: "delete",
		OpenPRBeforeMerge:  &trueVal,
		TargetWorkdir:      "/home/user/projects",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)

	// Insert a task with no execution fields set
	insertTaskNote(t, store, "task01", "Empty task", "pending", "high", "proj1", map[string]interface{}{})
	createProjectDir(t, svc.config.BrainDir, "proj1")

	result, err := svc.GetTasks(context.Background(), "proj1")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	if task.Agent != "tdd-dev" {
		t.Errorf("Agent = %q, want %q", task.Agent, "tdd-dev")
	}
	if task.Model != "sonnet" {
		t.Errorf("Model = %q, want %q", task.Model, "sonnet")
	}
	if task.ExecutionMode != "worktree" {
		t.Errorf("ExecutionMode = %q, want %q", task.ExecutionMode, "worktree")
	}
	if task.CompleteOnIdle == nil || !*task.CompleteOnIdle {
		t.Errorf("CompleteOnIdle = %v, want true", task.CompleteOnIdle)
	}
	if task.MergePolicy != "auto_merge" {
		t.Errorf("MergePolicy = %q, want %q", task.MergePolicy, "auto_merge")
	}
	if task.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy = %q, want %q", task.MergeStrategy, "squash")
	}
	if task.MergeTargetBranch != "main" {
		t.Errorf("MergeTargetBranch = %q, want %q", task.MergeTargetBranch, "main")
	}
	if task.RemoteBranchPolicy != "delete" {
		t.Errorf("RemoteBranchPolicy = %q, want %q", task.RemoteBranchPolicy, "delete")
	}
	if task.OpenPRBeforeMerge == nil || !*task.OpenPRBeforeMerge {
		t.Errorf("OpenPRBeforeMerge = %v, want true", task.OpenPRBeforeMerge)
	}
	if task.TargetWorkdir != "/home/user/projects" {
		t.Errorf("TargetWorkdir = %q, want %q", task.TargetWorkdir, "/home/user/projects")
	}
}

func TestApplyTaskDefaults_FullTaskNotOverwritten(t *testing.T) {
	// Task has all fields set; config provides different defaults.
	// Expected: task values preserved, NOT overwritten by defaults.
	trueVal := true
	falseVal := false
	defaults := config.TaskDefaultsConfig{
		Agent:              "default-agent",
		Model:              "default-model",
		ExecutionMode:      "default-mode",
		CompleteOnIdle:     &trueVal,
		MergePolicy:        "default-merge-policy",
		MergeStrategy:      "default-strategy",
		MergeTargetBranch:  "default-branch",
		RemoteBranchPolicy: "default-remote-policy",
		OpenPRBeforeMerge:  &trueVal,
		TargetWorkdir:      "/default/workdir",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)

	// Insert a task with ALL execution fields set to task-specific values
	insertTaskNote(t, store, "task02", "Full task", "pending", "high", "proj1", map[string]interface{}{
		"agent":                "task-agent",
		"model":                "task-model",
		"execution_mode":       "current_branch",
		"complete_on_idle":     false,
		"merge_policy":         "prompt_only",
		"merge_strategy":       "merge",
		"merge_target_branch":  "develop",
		"remote_branch_policy": "keep",
		"open_pr_before_merge": false,
		"target_workdir":       "/task/workdir",
	})
	createProjectDir(t, svc.config.BrainDir, "proj1")

	result, err := svc.GetTasks(context.Background(), "proj1")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	if task.Agent != "task-agent" {
		t.Errorf("Agent = %q, want %q", task.Agent, "task-agent")
	}
	if task.Model != "task-model" {
		t.Errorf("Model = %q, want %q", task.Model, "task-model")
	}
	if task.ExecutionMode != "current_branch" {
		t.Errorf("ExecutionMode = %q, want %q", task.ExecutionMode, "current_branch")
	}
	if task.CompleteOnIdle == nil || *task.CompleteOnIdle != false {
		t.Errorf("CompleteOnIdle = %v, want false", task.CompleteOnIdle)
	}
	if task.MergePolicy != "prompt_only" {
		t.Errorf("MergePolicy = %q, want %q", task.MergePolicy, "prompt_only")
	}
	if task.MergeStrategy != "merge" {
		t.Errorf("MergeStrategy = %q, want %q", task.MergeStrategy, "merge")
	}
	if task.MergeTargetBranch != "develop" {
		t.Errorf("MergeTargetBranch = %q, want %q", task.MergeTargetBranch, "develop")
	}
	if task.RemoteBranchPolicy != "keep" {
		t.Errorf("RemoteBranchPolicy = %q, want %q", task.RemoteBranchPolicy, "keep")
	}
	if task.OpenPRBeforeMerge == nil || *task.OpenPRBeforeMerge != false {
		t.Errorf("OpenPRBeforeMerge = %v, want false", task.OpenPRBeforeMerge)
	}
	_ = falseVal // used above via metadata
	if task.TargetWorkdir != "/task/workdir" {
		t.Errorf("TargetWorkdir = %q, want %q", task.TargetWorkdir, "/task/workdir")
	}
}

func TestApplyTaskDefaults_NoDefaultsConfigured(t *testing.T) {
	// TaskDefaults is zero-value (empty). No defaults should be applied.
	svc, store, _ := newTestTaskServiceWithDefaults(t, config.TaskDefaultsConfig{})

	insertTaskNote(t, store, "task03", "Task no defaults", "pending", "high", "proj1", map[string]interface{}{})
	createProjectDir(t, svc.config.BrainDir, "proj1")

	result, err := svc.GetTasks(context.Background(), "proj1")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	// All execution fields should remain empty/nil
	if task.Agent != "" {
		t.Errorf("Agent = %q, want empty", task.Agent)
	}
	if task.Model != "" {
		t.Errorf("Model = %q, want empty", task.Model)
	}
	if task.ExecutionMode != "" {
		t.Errorf("ExecutionMode = %q, want empty", task.ExecutionMode)
	}
	if task.CompleteOnIdle != nil {
		t.Errorf("CompleteOnIdle = %v, want nil", task.CompleteOnIdle)
	}
	if task.MergePolicy != "" {
		t.Errorf("MergePolicy = %q, want empty", task.MergePolicy)
	}
	if task.MergeStrategy != "" {
		t.Errorf("MergeStrategy = %q, want empty", task.MergeStrategy)
	}
	if task.MergeTargetBranch != "" {
		t.Errorf("MergeTargetBranch = %q, want empty", task.MergeTargetBranch)
	}
	if task.RemoteBranchPolicy != "" {
		t.Errorf("RemoteBranchPolicy = %q, want empty", task.RemoteBranchPolicy)
	}
	if task.OpenPRBeforeMerge != nil {
		t.Errorf("OpenPRBeforeMerge = %v, want nil", task.OpenPRBeforeMerge)
	}
	if task.TargetWorkdir != "" {
		t.Errorf("TargetWorkdir = %q, want empty", task.TargetWorkdir)
	}
}

func TestApplyTaskDefaults_GetNextAlsoAppliesDefaults(t *testing.T) {
	// Defaults should also be applied when calling GetNext (via GetTasks).
	defaults := config.TaskDefaultsConfig{
		Agent: "default-agent",
		Model: "default-model",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)

	insertTaskNote(t, store, "task04", "Next task", "pending", "high", "proj1", map[string]interface{}{})
	createProjectDir(t, svc.config.BrainDir, "proj1")

	next, err := svc.GetNext(context.Background(), "proj1", nil)
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil {
		t.Fatal("expected task, got nil")
	}

	if next.Agent != "default-agent" {
		t.Errorf("Agent = %q, want %q", next.Agent, "default-agent")
	}
	if next.Model != "default-model" {
		t.Errorf("Model = %q, want %q", next.Model, "default-model")
	}
}

func TestApplyTaskDefaults_PartialOverlap(t *testing.T) {
	// Task has some fields set, config has defaults for all fields.
	// Expected: only empty task fields get defaults; set fields preserved.
	trueVal := true
	defaults := config.TaskDefaultsConfig{
		Agent:             "default-agent",
		Model:             "default-model",
		ExecutionMode:     "worktree",
		CompleteOnIdle:    &trueVal,
		MergePolicy:       "auto_merge",
		MergeStrategy:     "squash",
		MergeTargetBranch: "main",
		TargetWorkdir:     "/default/workdir",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)

	// Task only sets agent and model
	insertTaskNote(t, store, "task05", "Partial task", "pending", "high", "proj1", map[string]interface{}{
		"agent": "my-agent",
		"model": "my-model",
	})
	createProjectDir(t, svc.config.BrainDir, "proj1")

	result, err := svc.GetTasks(context.Background(), "proj1")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	// Task-specific values preserved
	if task.Agent != "my-agent" {
		t.Errorf("Agent = %q, want %q", task.Agent, "my-agent")
	}
	if task.Model != "my-model" {
		t.Errorf("Model = %q, want %q", task.Model, "my-model")
	}

	// Defaults fill in the rest
	if task.ExecutionMode != "worktree" {
		t.Errorf("ExecutionMode = %q, want %q", task.ExecutionMode, "worktree")
	}
	if task.CompleteOnIdle == nil || !*task.CompleteOnIdle {
		t.Errorf("CompleteOnIdle = %v, want true", task.CompleteOnIdle)
	}
	if task.MergePolicy != "auto_merge" {
		t.Errorf("MergePolicy = %q, want %q", task.MergePolicy, "auto_merge")
	}
	if task.MergeStrategy != "squash" {
		t.Errorf("MergeStrategy = %q, want %q", task.MergeStrategy, "squash")
	}
	if task.MergeTargetBranch != "main" {
		t.Errorf("MergeTargetBranch = %q, want %q", task.MergeTargetBranch, "main")
	}
	if task.TargetWorkdir != "/default/workdir" {
		t.Errorf("TargetWorkdir = %q, want %q", task.TargetWorkdir, "/default/workdir")
	}
}

func TestApplyTaskDefaults_BoolFieldNilGetsDefault(t *testing.T) {
	// Test *bool fields specifically: nil gets default, non-nil preserved.
	trueVal := true
	falseVal := false

	defaults := config.TaskDefaultsConfig{
		CompleteOnIdle:    &trueVal,
		OpenPRBeforeMerge: &falseVal,
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)

	// Task with nil *bool fields (not set in metadata)
	insertTaskNote(t, store, "task06", "Bool test", "pending", "high", "proj1", map[string]interface{}{})
	createProjectDir(t, svc.config.BrainDir, "proj1")

	result, err := svc.GetTasks(context.Background(), "proj1")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	if task.CompleteOnIdle == nil {
		t.Fatal("CompleteOnIdle should not be nil")
	}
	if *task.CompleteOnIdle != true {
		t.Errorf("CompleteOnIdle = %v, want true", *task.CompleteOnIdle)
	}

	if task.OpenPRBeforeMerge == nil {
		t.Fatal("OpenPRBeforeMerge should not be nil")
	}
	if *task.OpenPRBeforeMerge != false {
		t.Errorf("OpenPRBeforeMerge = %v, want false", *task.OpenPRBeforeMerge)
	}
}

func TestApplyTaskDefaults_BoolFieldSetNotOverwritten(t *testing.T) {
	// Task has *bool set to false; default is true. Task value must win.
	trueVal := true
	defaults := config.TaskDefaultsConfig{
		CompleteOnIdle:    &trueVal,
		OpenPRBeforeMerge: &trueVal,
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)

	insertTaskNote(t, store, "task07", "Bool override test", "pending", "high", "proj1", map[string]interface{}{
		"complete_on_idle":     false,
		"open_pr_before_merge": false,
	})
	createProjectDir(t, svc.config.BrainDir, "proj1")

	result, err := svc.GetTasks(context.Background(), "proj1")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]

	if task.CompleteOnIdle == nil {
		t.Fatal("CompleteOnIdle should not be nil")
	}
	if *task.CompleteOnIdle != false {
		t.Errorf("CompleteOnIdle = %v, want false (task value should win)", *task.CompleteOnIdle)
	}

	if task.OpenPRBeforeMerge == nil {
		t.Fatal("OpenPRBeforeMerge should not be nil")
	}
	if *task.OpenPRBeforeMerge != false {
		t.Errorf("OpenPRBeforeMerge = %v, want false (task value should win)", *task.OpenPRBeforeMerge)
	}
}

// ---------------------------------------------------------------------------
// StartClaimCleanup - background stale claim cleanup
// ---------------------------------------------------------------------------

func TestStartClaimCleanup_ExpiresStale(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	// Seed two expired claims directly into storage with past expiry.
	db := store.DB()
	pastMs := time.Now().Add(-5 * time.Minute).UnixMilli()
	for _, taskID := range []string{"task1", "task2"} {
		_, err := db.Exec(
			"INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at) VALUES (?, ?, ?, ?, ?)",
			"proj", taskID, "dead-runner", pastMs, pastMs,
		)
		if err != nil {
			t.Fatalf("seed expired claim %s: %v", taskID, err)
		}
	}

	// Seed one active (non-expired) claim.
	ok, _, err := store.ClaimTask(ctx, "proj", "task3", "alive-runner", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("seed active claim: err=%v ok=%v", err, ok)
	}

	// Run cleanup with a very short interval so it fires quickly, then cancel.
	cleanupCtx, cancel := context.WithCancel(ctx)
	svc.StartClaimCleanup(cleanupCtx, 50*time.Millisecond)

	// Wait enough for at least one tick.
	time.Sleep(200 * time.Millisecond)
	cancel()

	// Verify expired claims are gone.
	for _, taskID := range []string{"task1", "task2"} {
		claim, err := store.GetClaim(ctx, "proj", taskID)
		if err != nil {
			t.Fatalf("GetClaim %s: %v", taskID, err)
		}
		if claim != nil {
			t.Errorf("expected expired claim for %s to be removed, still exists", taskID)
		}
	}

	// Verify active claim is still present.
	claim, err := store.GetClaim(ctx, "proj", "task3")
	if err != nil {
		t.Fatalf("GetClaim task3: %v", err)
	}
	if claim == nil {
		t.Error("expected active claim for task3 to survive cleanup")
	}
}

func TestStartClaimCleanup_RespectsContextCancellation(t *testing.T) {
	svc, _, _ := newTestTaskService(t)

	ctx, cancel := context.WithCancel(context.Background())

	// Start cleanup goroutine.
	svc.StartClaimCleanup(ctx, 50*time.Millisecond)

	// Cancel immediately.
	cancel()

	// Wait to ensure goroutine exits without panic or hang.
	// If it doesn't respect cancellation, the test will hang until timeout.
	time.Sleep(200 * time.Millisecond)
}

func TestStartClaimCleanup_NoExpiredClaims(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	// Seed only active claims.
	ok, _, err := store.ClaimTask(ctx, "proj", "task1", "runner-1", 5*time.Minute)
	if err != nil || !ok {
		t.Fatalf("seed active claim: err=%v ok=%v", err, ok)
	}

	cleanupCtx, cancel := context.WithCancel(ctx)
	svc.StartClaimCleanup(cleanupCtx, 50*time.Millisecond)

	time.Sleep(200 * time.Millisecond)
	cancel()

	// Active claim should still exist.
	claim, err := store.GetClaim(ctx, "proj", "task1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if claim == nil {
		t.Error("expected active claim to survive cleanup")
	}
}

// TestApplyTaskDefaults_DerivesGitBranchFromFeatureID verifies that when a task
// has execution_mode=worktree and feature_id set but git_branch empty, the
// service layer auto-derives git_branch = feature_id.
func TestApplyTaskDefaults_DerivesGitBranchFromFeatureID(t *testing.T) {
	defaults := config.TaskDefaultsConfig{
		ExecutionMode: "worktree",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	// Task with feature_id set but git_branch empty and execution_mode=worktree
	insertTaskNote(t, store, "feat1111", "Feature Task", "pending", "high", "proj", map[string]interface{}{
		"feature_id":     "auth-refactor",
		"execution_mode": "worktree",
	})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]
	if task.GitBranch != "auth-refactor" {
		t.Errorf("GitBranch = %q, want %q (should be derived from feature_id)", task.GitBranch, "auth-refactor")
	}
}

// TestApplyTaskDefaults_DoesNotOverwriteExplicitGitBranch verifies that an
// explicit git_branch on a task is never overwritten by feature_id derivation.
func TestApplyTaskDefaults_DoesNotOverwriteExplicitGitBranch(t *testing.T) {
	defaults := config.TaskDefaultsConfig{
		ExecutionMode: "worktree",
	}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	// Task with both git_branch and feature_id explicitly set
	insertTaskNote(t, store, "feat2222", "Explicit Branch Task", "pending", "high", "proj", map[string]interface{}{
		"feature_id":     "auth-refactor",
		"git_branch":     "my-explicit-branch",
		"execution_mode": "worktree",
	})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]
	if task.GitBranch != "my-explicit-branch" {
		t.Errorf("GitBranch = %q, want %q (explicit git_branch must not be overwritten)", task.GitBranch, "my-explicit-branch")
	}
}

// TestApplyTaskDefaults_NoGitBranchDerivationForCurrentBranch verifies that
// git_branch is NOT derived when execution_mode=current_branch.
func TestApplyTaskDefaults_NoGitBranchDerivationForCurrentBranch(t *testing.T) {
	defaults := config.TaskDefaultsConfig{}

	svc, store, _ := newTestTaskServiceWithDefaults(t, defaults)
	ctx := context.Background()

	insertTaskNote(t, store, "feat3333", "Current Branch Task", "pending", "high", "proj", map[string]interface{}{
		"feature_id":     "auth-refactor",
		"execution_mode": "current_branch",
	})

	result, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(result.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(result.Tasks))
	}

	task := result.Tasks[0]
	if task.GitBranch != "" {
		t.Errorf("GitBranch = %q, want empty (should not derive for current_branch mode)", task.GitBranch)
	}
}
