package frontmatter

import (
	"strings"
	"testing"
)

// =============================================================================
// Parse tests
// =============================================================================

func TestParse_BasicFrontmatter(t *testing.T) {
	content := "---\ntitle: Test Entry\ntype: task\nstatus: pending\n---\n\nBody content here."

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Title != "Test Entry" {
		t.Errorf("title = %q, want %q", doc.Frontmatter.Title, "Test Entry")
	}
	if doc.Frontmatter.Type != "task" {
		t.Errorf("type = %q, want %q", doc.Frontmatter.Type, "task")
	}
	if doc.Frontmatter.Status != "pending" {
		t.Errorf("status = %q, want %q", doc.Frontmatter.Status, "pending")
	}
	if doc.Body != "Body content here." {
		t.Errorf("body = %q, want %q", doc.Body, "Body content here.")
	}
}

func TestParse_NoFrontmatter(t *testing.T) {
	content := "Just some body content."

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Title != "" {
		t.Errorf("title should be empty, got %q", doc.Frontmatter.Title)
	}
	if doc.Body != "Just some body content." {
		t.Errorf("body = %q, want %q", doc.Body, "Just some body content.")
	}
}

func TestParse_EmptyFrontmatter(t *testing.T) {
	content := "---\n---\n\nBody only."

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Title != "" {
		t.Errorf("title should be empty, got %q", doc.Frontmatter.Title)
	}
	if doc.Body != "Body only." {
		t.Errorf("body = %q, want %q", doc.Body, "Body only.")
	}
}

func TestParse_Priority(t *testing.T) {
	content := "---\ntitle: Test\ntype: task\npriority: high\nstatus: active\n---\n\nContent"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Priority != "high" {
		t.Errorf("priority = %q, want %q", doc.Frontmatter.Priority, "high")
	}
}

func TestParse_Tags(t *testing.T) {
	content := "---\ntitle: Test\ntype: task\ntags:\n  - tag1\n  - tag2\nstatus: active\n---\n\nContent"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(doc.Frontmatter.Tags) != 2 {
		t.Fatalf("tags length = %d, want 2", len(doc.Frontmatter.Tags))
	}
	if doc.Frontmatter.Tags[0] != "tag1" || doc.Frontmatter.Tags[1] != "tag2" {
		t.Errorf("tags = %v, want [tag1, tag2]", doc.Frontmatter.Tags)
	}
}

func TestParse_QuotedTitleWithSpecialChars(t *testing.T) {
	content := "---\ntitle: \"Test: With Colon\"\ntype: task\nstatus: active\n---\n\nContent"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Title != "Test: With Colon" {
		t.Errorf("title = %q, want %q", doc.Frontmatter.Title, "Test: With Colon")
	}
}

func TestParse_SessionsMap(t *testing.T) {
	content := `---
title: Test Entry
type: task
status: pending
sessions:
  ses_new111aaa:
    timestamp: "2026-02-22T10:30:00.000Z"
    cron_id: "cron_123"
    run_id: "run_456"
  ses_new222bbb:
    timestamp: "2026-02-22T10:31:00.000Z"
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessions := doc.Frontmatter.Sessions
	if len(sessions) != 2 {
		t.Fatalf("sessions length = %d, want 2", len(sessions))
	}

	s1, ok := sessions["ses_new111aaa"]
	if !ok {
		t.Fatal("missing session ses_new111aaa")
	}
	if s1.Timestamp != "2026-02-22T10:30:00.000Z" {
		t.Errorf("timestamp = %q, want %q", s1.Timestamp, "2026-02-22T10:30:00.000Z")
	}
	if s1.CronID != "cron_123" {
		t.Errorf("cron_id = %q, want %q", s1.CronID, "cron_123")
	}
	if s1.RunID != "run_456" {
		t.Errorf("run_id = %q, want %q", s1.RunID, "run_456")
	}

	s2, ok := sessions["ses_new222bbb"]
	if !ok {
		t.Fatal("missing session ses_new222bbb")
	}
	if s2.Timestamp != "2026-02-22T10:31:00.000Z" {
		t.Errorf("timestamp = %q, want %q", s2.Timestamp, "2026-02-22T10:31:00.000Z")
	}
}

func TestParse_LegacySessionNormalization(t *testing.T) {
	content := `---
title: Legacy Sessions
type: task
status: pending
session_ids:
  - ses_old111aaa
  - ses_old222bbb
session_timestamps:
  ses_old111aaa: "2026-02-22T11:00:00.000Z"
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessions := doc.Frontmatter.Sessions
	if len(sessions) != 2 {
		t.Fatalf("sessions length = %d, want 2", len(sessions))
	}

	s1 := sessions["ses_old111aaa"]
	if s1.Timestamp != "2026-02-22T11:00:00.000Z" {
		t.Errorf("timestamp = %q, want %q", s1.Timestamp, "2026-02-22T11:00:00.000Z")
	}

	s2 := sessions["ses_old222bbb"]
	if s2.Timestamp != "" {
		t.Errorf("timestamp = %q, want empty", s2.Timestamp)
	}
}

func TestParse_PrefersNewSessionsOverLegacy(t *testing.T) {
	content := `---
title: Mixed Sessions
type: task
status: pending
sessions:
  ses_new999zzz:
    timestamp: "2026-02-22T12:00:00.000Z"
session_ids:
  - ses_old999zzz
session_timestamps:
  ses_old999zzz: "1999-01-01T00:00:00.000Z"
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessions := doc.Frontmatter.Sessions
	if len(sessions) != 1 {
		t.Fatalf("sessions length = %d, want 1 (new format only)", len(sessions))
	}

	s, ok := sessions["ses_new999zzz"]
	if !ok {
		t.Fatal("missing session ses_new999zzz")
	}
	if s.Timestamp != "2026-02-22T12:00:00.000Z" {
		t.Errorf("timestamp = %q, want %q", s.Timestamp, "2026-02-22T12:00:00.000Z")
	}

	if _, ok := sessions["ses_old999zzz"]; ok {
		t.Error("legacy session ses_old999zzz should not be present")
	}
}

func TestParse_ScheduleFields(t *testing.T) {
	content := `---
title: Scheduled Task
type: task
status: pending
schedule: "0 */2 * * *"
next_run: "2026-03-01T10:00:00.000Z"
runs:
  - run_id: "run_001"
    status: completed
    started: "2026-03-01T08:00:00.000Z"
    completed: "2026-03-01T08:02:00.000Z"
    duration: "120s"
    tasks: "3"
    failed_task: ""
    skip_reason: ""
  - run_id: "run_002"
    status: skipped
    started: "2026-03-01T09:00:00.000Z"
    completed: "2026-03-01T09:00:10.000Z"
    duration: "10s"
    tasks: "0"
    failed_task: ""
    skip_reason: "dependency pending"
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fm := doc.Frontmatter
	if fm.Schedule != "0 */2 * * *" {
		t.Errorf("schedule = %q, want %q", fm.Schedule, "0 */2 * * *")
	}
	if fm.NextRun != "2026-03-01T10:00:00.000Z" {
		t.Errorf("next_run = %q, want %q", fm.NextRun, "2026-03-01T10:00:00.000Z")
	}
	if len(fm.Runs) != 2 {
		t.Fatalf("runs length = %d, want 2", len(fm.Runs))
	}

	r1 := fm.Runs[0]
	if r1.RunID != "run_001" {
		t.Errorf("run[0].run_id = %q, want %q", r1.RunID, "run_001")
	}
	if r1.Status != "completed" {
		t.Errorf("run[0].status = %q, want %q", r1.Status, "completed")
	}
	if r1.Duration != "120s" {
		t.Errorf("run[0].duration = %q, want %q", r1.Duration, "120s")
	}
	if r1.Tasks != "3" {
		t.Errorf("run[0].tasks = %q, want %q", r1.Tasks, "3")
	}

	r2 := fm.Runs[1]
	if r2.RunID != "run_002" {
		t.Errorf("run[1].run_id = %q, want %q", r2.RunID, "run_002")
	}
	if r2.SkipReason != "dependency pending" {
		t.Errorf("run[1].skip_reason = %q, want %q", r2.SkipReason, "dependency pending")
	}
}

func TestParse_RunFinalizations(t *testing.T) {
	content := `---
title: Finalized Task
type: task
status: completed
run_finalizations:
  run_20260225_001:
    status: completed
    finalized_at: "2026-02-25T10:05:00.000Z"
    session_id: ses_abc123
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rf := doc.Frontmatter.RunFinalizations
	if len(rf) != 1 {
		t.Fatalf("run_finalizations length = %d, want 1", len(rf))
	}

	f, ok := rf["run_20260225_001"]
	if !ok {
		t.Fatal("missing run_finalizations entry")
	}
	if f.Status != "completed" {
		t.Errorf("status = %q, want %q", f.Status, "completed")
	}
	if f.FinalizedAt != "2026-02-25T10:05:00.000Z" {
		t.Errorf("finalized_at = %q, want %q", f.FinalizedAt, "2026-02-25T10:05:00.000Z")
	}
	if f.SessionID != "ses_abc123" {
		t.Errorf("session_id = %q, want %q", f.SessionID, "ses_abc123")
	}
}

func TestParse_MissingScheduleFields(t *testing.T) {
	content := "---\ntitle: Legacy Task\ntype: task\nstatus: active\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Schedule != "" {
		t.Errorf("schedule should be empty, got %q", doc.Frontmatter.Schedule)
	}
	if doc.Frontmatter.NextRun != "" {
		t.Errorf("next_run should be empty, got %q", doc.Frontmatter.NextRun)
	}
	if len(doc.Frontmatter.Runs) != 0 {
		t.Errorf("runs should be empty, got %v", doc.Frontmatter.Runs)
	}
}

func TestParse_GeneratedMetadata(t *testing.T) {
	content := `---
title: Generated Task
type: task
status: pending
generated: true
generated_kind: gap_task
generated_key: "feature-checkout:missing-tests"
generated_by: feature-checkout
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fm := doc.Frontmatter
	if fm.Generated == nil || *fm.Generated != true {
		t.Errorf("generated = %v, want true", fm.Generated)
	}
	if fm.GeneratedKind != "gap_task" {
		t.Errorf("generated_kind = %q, want %q", fm.GeneratedKind, "gap_task")
	}
	if fm.GeneratedKey != "feature-checkout:missing-tests" {
		t.Errorf("generated_key = %q, want %q", fm.GeneratedKey, "feature-checkout:missing-tests")
	}
	if fm.GeneratedBy != "feature-checkout" {
		t.Errorf("generated_by = %q, want %q", fm.GeneratedBy, "feature-checkout")
	}
}

func TestParse_MergeIntentFields(t *testing.T) {
	content := `---
title: Merge Intent Task
type: task
status: pending
merge_target_branch: main
merge_policy: auto_merge
merge_strategy: squash
open_pr_before_merge: false
execution_mode: current_branch
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fm := doc.Frontmatter
	if fm.MergeTargetBranch != "main" {
		t.Errorf("merge_target_branch = %q, want %q", fm.MergeTargetBranch, "main")
	}
	if fm.MergePolicy != "auto_merge" {
		t.Errorf("merge_policy = %q, want %q", fm.MergePolicy, "auto_merge")
	}
	if fm.MergeStrategy != "squash" {
		t.Errorf("merge_strategy = %q, want %q", fm.MergeStrategy, "squash")
	}
	if fm.OpenPRBeforeMerge == nil || *fm.OpenPRBeforeMerge != false {
		t.Errorf("open_pr_before_merge = %v, want false", fm.OpenPRBeforeMerge)
	}
	if fm.ExecutionMode != "current_branch" {
		t.Errorf("execution_mode = %q, want %q", fm.ExecutionMode, "current_branch")
	}
}

func TestParse_ExplicitGeneratedFalse(t *testing.T) {
	content := "---\ntitle: Manual Task\ntype: task\nstatus: pending\ngenerated: false\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Generated == nil || *doc.Frontmatter.Generated != false {
		t.Errorf("generated = %v, want false", doc.Frontmatter.Generated)
	}
}

func TestParse_DependsOn(t *testing.T) {
	content := "---\ntitle: Task\ntype: task\nstatus: active\ndepends_on:\n  - abc12def\n  - xyz99876\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(doc.Frontmatter.DependsOn) != 2 {
		t.Fatalf("depends_on length = %d, want 2", len(doc.Frontmatter.DependsOn))
	}
	if doc.Frontmatter.DependsOn[0] != "abc12def" {
		t.Errorf("depends_on[0] = %q, want %q", doc.Frontmatter.DependsOn[0], "abc12def")
	}
	if doc.Frontmatter.DependsOn[1] != "xyz99876" {
		t.Errorf("depends_on[1] = %q, want %q", doc.Frontmatter.DependsOn[1], "xyz99876")
	}
}

func TestParse_QuotedDependsOn(t *testing.T) {
	content := "---\ntitle: Task\ntype: task\nstatus: active\ndepends_on:\n  - \"abc12def\"\n  - \"xyz99876\"\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(doc.Frontmatter.DependsOn) != 2 {
		t.Fatalf("depends_on length = %d, want 2", len(doc.Frontmatter.DependsOn))
	}
	if doc.Frontmatter.DependsOn[0] != "abc12def" {
		t.Errorf("depends_on[0] = %q, want %q", doc.Frontmatter.DependsOn[0], "abc12def")
	}
}

func TestParse_ScheduleEnabled(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    *bool
	}{
		{
			name:    "false",
			content: "---\ntitle: Test\ntype: task\nstatus: completed\nschedule_enabled: false\n---\n\nBody",
			want:    boolPtr(false),
		},
		{
			name:    "true",
			content: "---\ntitle: Test\ntype: task\nstatus: active\nschedule_enabled: true\n---\n\nBody",
			want:    boolPtr(true),
		},
		{
			name:    "absent",
			content: "---\ntitle: Test\ntype: task\nstatus: active\n---\n\nBody",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := Parse(tt.content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.want == nil {
				if doc.Frontmatter.ScheduleEnabled != nil {
					t.Errorf("schedule_enabled = %v, want nil", *doc.Frontmatter.ScheduleEnabled)
				}
			} else {
				if doc.Frontmatter.ScheduleEnabled == nil {
					t.Fatalf("schedule_enabled = nil, want %v", *tt.want)
				}
				if *doc.Frontmatter.ScheduleEnabled != *tt.want {
					t.Errorf("schedule_enabled = %v, want %v", *doc.Frontmatter.ScheduleEnabled, *tt.want)
				}
			}
		})
	}
}

func TestParse_MaxRuns(t *testing.T) {
	content := "---\ntitle: Test\ntype: task\nstatus: active\nmax_runs: 5\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.MaxRuns == nil || *doc.Frontmatter.MaxRuns != 5 {
		t.Errorf("max_runs = %v, want 5", doc.Frontmatter.MaxRuns)
	}
}

func TestParse_DirectPrompt_SingleLine(t *testing.T) {
	content := "---\ntitle: Test Task\ntype: task\nstatus: pending\ndirect_prompt: Run the tests\n---\n\nContent"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.DirectPrompt != "Run the tests" {
		t.Errorf("direct_prompt = %q, want %q", doc.Frontmatter.DirectPrompt, "Run the tests")
	}
}

func TestParse_DirectPrompt_BlockScalar(t *testing.T) {
	content := `---
title: Test Task
type: task
status: pending
direct_prompt: |
  Step 1: Read the code
  Step 2: Fix the bug
  Step 3: Run tests
---

Content`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Step 1: Read the code\nStep 2: Fix the bug\nStep 3: Run tests"
	if doc.Frontmatter.DirectPrompt != want {
		t.Errorf("direct_prompt = %q, want %q", doc.Frontmatter.DirectPrompt, want)
	}
}

func TestParse_AgentField(t *testing.T) {
	content := "---\ntitle: Test Task\ntype: task\nstatus: pending\nagent: explore\n---\n\nContent"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Agent != "explore" {
		t.Errorf("agent = %q, want %q", doc.Frontmatter.Agent, "explore")
	}
}

func TestParse_ModelField(t *testing.T) {
	content := "---\ntitle: Test Task\ntype: task\nstatus: pending\nmodel: anthropic/claude-sonnet-4-20250514\n---\n\nContent"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", doc.Frontmatter.Model, "anthropic/claude-sonnet-4-20250514")
	}
}

func TestParse_UserOriginalRequest_SingleLine(t *testing.T) {
	content := "---\ntitle: Test Task\ntype: task\nstatus: pending\nuser_original_request: Add a simple button\n---\n\nTask content"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.UserOriginalRequest != "Add a simple button" {
		t.Errorf("user_original_request = %q, want %q", doc.Frontmatter.UserOriginalRequest, "Add a simple button")
	}
}

func TestParse_UserOriginalRequest_BlockScalar(t *testing.T) {
	content := `---
title: Test Task
type: task
status: pending
user_original_request: |
  Add a button with:
  - Blue color
  - Round corners
  - 10px padding
---

Task content`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Add a button with:\n- Blue color\n- Round corners\n- 10px padding"
	if doc.Frontmatter.UserOriginalRequest != want {
		t.Errorf("user_original_request = %q, want %q", doc.Frontmatter.UserOriginalRequest, want)
	}
}

func TestParse_CompleteOnIdle(t *testing.T) {
	content := "---\ntitle: Test\ntype: task\nstatus: active\ncomplete_on_idle: true\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.CompleteOnIdle == nil || *doc.Frontmatter.CompleteOnIdle != true {
		t.Errorf("complete_on_idle = %v, want true", doc.Frontmatter.CompleteOnIdle)
	}
}

func TestParse_AllStringFields(t *testing.T) {
	content := `---
title: Full Task
type: task
status: pending
priority: high
projectId: my-project
name: my-execution
created: "2024-01-01T00:00:00Z"
parent_id: abc12def
feature_id: feat-001
feature_priority: medium
workdir: projects/test
git_remote: "git@github.com:user/repo.git"
git_branch: feature/new-feature
target_workdir: /tmp/work
remote_branch_policy: delete
---

Body`

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fm := doc.Frontmatter
	if fm.ProjectID != "my-project" {
		t.Errorf("projectId = %q, want %q", fm.ProjectID, "my-project")
	}
	if fm.Name != "my-execution" {
		t.Errorf("name = %q, want %q", fm.Name, "my-execution")
	}
	if fm.Created != "2024-01-01T00:00:00Z" {
		t.Errorf("created = %q, want %q", fm.Created, "2024-01-01T00:00:00Z")
	}
	if fm.ParentID != "abc12def" {
		t.Errorf("parent_id = %q, want %q", fm.ParentID, "abc12def")
	}
	if fm.FeatureID != "feat-001" {
		t.Errorf("feature_id = %q, want %q", fm.FeatureID, "feat-001")
	}
	if fm.FeaturePriority != "medium" {
		t.Errorf("feature_priority = %q, want %q", fm.FeaturePriority, "medium")
	}
	if fm.Workdir != "projects/test" {
		t.Errorf("workdir = %q, want %q", fm.Workdir, "projects/test")
	}
	if fm.GitRemote != "git@github.com:user/repo.git" {
		t.Errorf("git_remote = %q, want %q", fm.GitRemote, "git@github.com:user/repo.git")
	}
	if fm.GitBranch != "feature/new-feature" {
		t.Errorf("git_branch = %q, want %q", fm.GitBranch, "feature/new-feature")
	}
	if fm.TargetWorkdir != "/tmp/work" {
		t.Errorf("target_workdir = %q, want %q", fm.TargetWorkdir, "/tmp/work")
	}
	if fm.RemoteBranchPolicy != "delete" {
		t.Errorf("remote_branch_policy = %q, want %q", fm.RemoteBranchPolicy, "delete")
	}
}

func TestParse_FeatureDependsOn(t *testing.T) {
	content := "---\ntitle: Task\ntype: task\nstatus: active\nfeature_depends_on:\n  - feat-a\n  - feat-b\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(doc.Frontmatter.FeatureDependsOn) != 2 {
		t.Fatalf("feature_depends_on length = %d, want 2", len(doc.Frontmatter.FeatureDependsOn))
	}
	if doc.Frontmatter.FeatureDependsOn[0] != "feat-a" {
		t.Errorf("feature_depends_on[0] = %q, want %q", doc.Frontmatter.FeatureDependsOn[0], "feat-a")
	}
}

func TestParse_UnknownFieldsInExtra(t *testing.T) {
	content := "---\ntitle: Test\ntype: task\nstatus: active\ncustom_field: custom_value\nanother_field: 42\n---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Extra == nil {
		t.Fatal("Extra should not be nil")
	}
	if v, ok := doc.Frontmatter.Extra["custom_field"]; !ok || v != "custom_value" {
		t.Errorf("Extra[custom_field] = %v, want %q", v, "custom_value")
	}
	if v, ok := doc.Frontmatter.Extra["another_field"]; !ok || v != 42 {
		t.Errorf("Extra[another_field] = %v, want 42", v)
	}
}

// =============================================================================
// EscapeYamlValue tests
// =============================================================================

func TestEscapeYamlValue_PlainString(t *testing.T) {
	if got := EscapeYamlValue("simple"); got != "simple" {
		t.Errorf("got %q, want %q", got, "simple")
	}
	if got := EscapeYamlValue("path/to/file"); got != "path/to/file" {
		t.Errorf("got %q, want %q", got, "path/to/file")
	}
}

func TestEscapeYamlValue_QuotesColons(t *testing.T) {
	if got := EscapeYamlValue("key: value"); got != `"key: value"` {
		t.Errorf("got %q, want %q", got, `"key: value"`)
	}
}

func TestEscapeYamlValue_QuotesSpecialChars(t *testing.T) {
	if got := EscapeYamlValue("test#comment"); got != `"test#comment"` {
		t.Errorf("got %q, want %q", got, `"test#comment"`)
	}
	if got := EscapeYamlValue("value@domain"); got != `"value@domain"` {
		t.Errorf("got %q, want %q", got, `"value@domain"`)
	}
}

func TestEscapeYamlValue_EscapesInternalQuotes(t *testing.T) {
	if got := EscapeYamlValue(`say "hello"`); got != `"say \"hello\""` {
		t.Errorf("got %q, want %q", got, `"say \"hello\""`)
	}
}

func TestEscapeYamlValue_EscapesNewlines(t *testing.T) {
	if got := EscapeYamlValue("line1\nline2"); got != `"line1\nline2"` {
		t.Errorf("got %q, want %q", got, `"line1\nline2"`)
	}
}

func TestEscapeYamlValue_EscapesCarriageReturns(t *testing.T) {
	if got := EscapeYamlValue("line1\rline2"); got != `"line1\rline2"` {
		t.Errorf("got %q, want %q", got, `"line1\rline2"`)
	}
}

func TestEscapeYamlValue_EscapesTabs(t *testing.T) {
	if got := EscapeYamlValue("col1\tcol2"); got != `"col1\tcol2"` {
		t.Errorf("got %q, want %q", got, `"col1\tcol2"`)
	}
}

// =============================================================================
// FormatMultilineValue tests
// =============================================================================

func TestFormatMultilineValue_SimpleSingleLine(t *testing.T) {
	if got := FormatMultilineValue("key", "simple value"); got != "key: simple value" {
		t.Errorf("got %q, want %q", got, "key: simple value")
	}
}

func TestFormatMultilineValue_MultilineContent(t *testing.T) {
	got := FormatMultilineValue("key", "line 1\nline 2\nline 3")
	want := "key: |\n  line 1\n  line 2\n  line 3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMultilineValue_SpecialYAMLChars(t *testing.T) {
	got := FormatMultilineValue("key", "value: with colon")
	want := "key: |\n  value: with colon"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMultilineValue_PreservesEmptyLines(t *testing.T) {
	got := FormatMultilineValue("key", "line 1\n\nline 3")
	want := "key: |\n  line 1\n  \n  line 3"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMultilineValue_BackslashesAlone(t *testing.T) {
	got := FormatMultilineValue("key", `path\to\file`)
	want := `key: path\to\file`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMultilineValue_BackslashesWithSpecialChars(t *testing.T) {
	got := FormatMultilineValue("key", `path\to\file: with colon`)
	want := "key: |\n  path\\to\\file: with colon"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// =============================================================================
// Serialize tests
// =============================================================================

func TestSerialize_BasicFrontmatter(t *testing.T) {
	fm := &Frontmatter{
		Title:  "Test Title",
		Type:   "task",
		Status: "active",
	}
	result := Serialize(fm)
	if !strings.Contains(result, "title: Test Title") {
		t.Errorf("missing title in:\n%s", result)
	}
	if !strings.Contains(result, "type: task") {
		t.Errorf("missing type in:\n%s", result)
	}
	if !strings.Contains(result, "status: active") {
		t.Errorf("missing status in:\n%s", result)
	}
}

func TestSerialize_EscapesTitleWithSpecialChars(t *testing.T) {
	fm := &Frontmatter{
		Title:  "Fix: the bug",
		Type:   "task",
		Status: "active",
	}
	result := Serialize(fm)
	if !strings.Contains(result, `title: "Fix: the bug"`) {
		t.Errorf("title not properly escaped in:\n%s", result)
	}
}

func TestSerialize_TagsArray(t *testing.T) {
	fm := &Frontmatter{
		Title:  "Task",
		Type:   "task",
		Status: "active",
		Tags:   []string{"feature", "urgent"},
	}
	result := Serialize(fm)
	if !strings.Contains(result, "tags:") {
		t.Errorf("missing tags in:\n%s", result)
	}
	if !strings.Contains(result, "  - feature") {
		t.Errorf("missing tag 'feature' in:\n%s", result)
	}
	if !strings.Contains(result, "  - urgent") {
		t.Errorf("missing tag 'urgent' in:\n%s", result)
	}
}

func TestSerialize_DependsOn(t *testing.T) {
	fm := &Frontmatter{
		Title:     "Task",
		Type:      "task",
		Status:    "active",
		DependsOn: []string{"dep1", "dep2"},
	}
	result := Serialize(fm)
	if !strings.Contains(result, "depends_on:") {
		t.Errorf("missing depends_on in:\n%s", result)
	}
	if !strings.Contains(result, `  - "dep1"`) {
		t.Errorf("missing dep1 in:\n%s", result)
	}
	if !strings.Contains(result, `  - "dep2"`) {
		t.Errorf("missing dep2 in:\n%s", result)
	}
}

func TestSerialize_DependsOnWithQuotes(t *testing.T) {
	fm := &Frontmatter{
		Title:     "Task",
		Type:      "task",
		Status:    "active",
		DependsOn: []string{`abc"def`},
	}
	result := Serialize(fm)
	if !strings.Contains(result, `  - "abc\"def"`) {
		t.Errorf("depends_on not properly escaped in:\n%s", result)
	}
}

func TestSerialize_MultilineUserOriginalRequest(t *testing.T) {
	fm := &Frontmatter{
		Title:               "Task",
		Type:                "task",
		Status:              "active",
		UserOriginalRequest: "Line 1\nLine 2\nLine 3",
	}
	result := Serialize(fm)
	if !strings.Contains(result, "user_original_request: |") {
		t.Errorf("missing block scalar in:\n%s", result)
	}
	if !strings.Contains(result, "  Line 1") {
		t.Errorf("missing Line 1 in:\n%s", result)
	}
}

func TestSerialize_OmitsEmptyFields(t *testing.T) {
	fm := &Frontmatter{
		Title:  "Task",
		Type:   "task",
		Status: "active",
	}
	result := Serialize(fm)
	for _, field := range []string{"workdir:", "git_remote:", "depends_on:", "user_original_request:"} {
		if strings.Contains(result, field) {
			t.Errorf("should not contain %s in:\n%s", field, result)
		}
	}
}

func TestSerialize_SessionsMap(t *testing.T) {
	fm := &Frontmatter{
		Title:  "Task",
		Type:   "task",
		Status: "active",
		Sessions: map[string]SessionInfo{
			"ses_new111aaa": {
				Timestamp: "2026-02-22T13:00:00.000Z",
				CronID:    "cron_789",
				RunID:     "run_987",
			},
			"ses_new222bbb": {
				Timestamp: "2026-02-22T13:01:00.000Z",
			},
		},
	}
	result := Serialize(fm)
	if !strings.Contains(result, "sessions:") {
		t.Errorf("missing sessions in:\n%s", result)
	}
	if !strings.Contains(result, "  ses_new111aaa:") {
		t.Errorf("missing session key in:\n%s", result)
	}
	if !strings.Contains(result, `    timestamp: "2026-02-22T13:00:00.000Z"`) {
		t.Errorf("missing timestamp in:\n%s", result)
	}
	if !strings.Contains(result, "    cron_id: cron_789") {
		t.Errorf("missing cron_id in:\n%s", result)
	}
	if !strings.Contains(result, "    run_id: run_987") {
		t.Errorf("missing run_id in:\n%s", result)
	}
	if strings.Contains(result, "session_ids:") {
		t.Errorf("should not contain legacy session_ids in:\n%s", result)
	}
}

func TestSerialize_RunsArray(t *testing.T) {
	fm := &Frontmatter{
		Title:  "Scheduled Task",
		Type:   "task",
		Status: "pending",
		Runs: []CronRun{
			{
				RunID:      "run_001",
				Status:     "completed",
				Started:    "2026-03-01T08:00:00.000Z",
				Completed:  "2026-03-01T08:02:00.000Z",
				Duration:   "120s",
				Tasks:      "3",
				FailedTask: "",
				SkipReason: "",
			},
		},
	}
	result := Serialize(fm)
	if !strings.Contains(result, "runs:") {
		t.Errorf("missing runs in:\n%s", result)
	}
	if !strings.Contains(result, "  - run_id: run_001") {
		t.Errorf("missing run_id in:\n%s", result)
	}
	if !strings.Contains(result, "    status: completed") {
		t.Errorf("missing status in:\n%s", result)
	}
	if !strings.Contains(result, `    failed_task: ""`) {
		t.Errorf("missing empty failed_task in:\n%s", result)
	}
}

func TestSerialize_RunFinalizations(t *testing.T) {
	fm := &Frontmatter{
		Title:  "Finalized Task",
		Type:   "task",
		Status: "completed",
		RunFinalizations: map[string]RunFinalization{
			"run_20260225_001": {
				Status:      "completed",
				FinalizedAt: "2026-02-25T10:05:00.000Z",
				SessionID:   "ses_abc123",
			},
		},
	}
	result := Serialize(fm)
	if !strings.Contains(result, "run_finalizations:") {
		t.Errorf("missing run_finalizations in:\n%s", result)
	}
	if !strings.Contains(result, "  run_20260225_001:") {
		t.Errorf("missing run key in:\n%s", result)
	}
	if !strings.Contains(result, "    status: completed") {
		t.Errorf("missing status in:\n%s", result)
	}
	if !strings.Contains(result, `    finalized_at: "2026-02-25T10:05:00.000Z"`) {
		t.Errorf("missing finalized_at in:\n%s", result)
	}
	if !strings.Contains(result, "    session_id: ses_abc123") {
		t.Errorf("missing session_id in:\n%s", result)
	}
}

func TestSerialize_BooleanFields(t *testing.T) {
	f := false
	tr := true
	fm := &Frontmatter{
		Title:             "Task",
		Type:              "task",
		Status:            "pending",
		Generated:         &tr,
		OpenPRBeforeMerge: &f,
		ScheduleEnabled:   &f,
		CompleteOnIdle:    &tr,
	}
	result := Serialize(fm)
	if !strings.Contains(result, "generated: true") {
		t.Errorf("missing generated in:\n%s", result)
	}
	if !strings.Contains(result, "open_pr_before_merge: false") {
		t.Errorf("missing open_pr_before_merge in:\n%s", result)
	}
	if !strings.Contains(result, "schedule_enabled: false") {
		t.Errorf("missing schedule_enabled in:\n%s", result)
	}
	if !strings.Contains(result, "complete_on_idle: true") {
		t.Errorf("missing complete_on_idle in:\n%s", result)
	}
}

func TestSerialize_GeneratedFalse(t *testing.T) {
	f := false
	fm := &Frontmatter{
		Title:     "Manual Task",
		Type:      "task",
		Status:    "pending",
		Generated: &f,
	}
	result := Serialize(fm)
	if !strings.Contains(result, "generated: false") {
		t.Errorf("missing generated: false in:\n%s", result)
	}
}

func TestSerialize_MaxRuns(t *testing.T) {
	mr := 10
	fm := &Frontmatter{
		Title:   "Task",
		Type:    "task",
		Status:  "active",
		MaxRuns: &mr,
	}
	result := Serialize(fm)
	if !strings.Contains(result, "max_runs: 10") {
		t.Errorf("missing max_runs in:\n%s", result)
	}
}

// =============================================================================
// Generate tests
// =============================================================================

func TestGenerate_MinimalFrontmatter(t *testing.T) {
	result := Generate(&GenerateOptions{
		Title: "Test Entry",
		Type:  "task",
	})
	if !strings.Contains(result, "title: Test Entry") {
		t.Errorf("missing title in:\n%s", result)
	}
	if !strings.Contains(result, "type: task") {
		t.Errorf("missing type in:\n%s", result)
	}
	if !strings.Contains(result, "status: active") {
		t.Errorf("missing default status in:\n%s", result)
	}
	// Type should be added to tags
	if !strings.Contains(result, "  - task") {
		t.Errorf("type should be in tags in:\n%s", result)
	}
}

func TestGenerate_WithTags(t *testing.T) {
	result := Generate(&GenerateOptions{
		Title: "Test",
		Type:  "task",
		Tags:  []string{"tag1", "tag2"},
	})
	if !strings.Contains(result, "  - tag1") {
		t.Errorf("missing tag1 in:\n%s", result)
	}
	if !strings.Contains(result, "  - tag2") {
		t.Errorf("missing tag2 in:\n%s", result)
	}
	if !strings.Contains(result, "  - task") {
		t.Errorf("type should be added to tags in:\n%s", result)
	}
}

func TestGenerate_WithDependsOn(t *testing.T) {
	result := Generate(&GenerateOptions{
		Title:     "Task with deps",
		Type:      "task",
		DependsOn: []string{"abc12def", "xyz99876"},
	})
	if !strings.Contains(result, "depends_on:") {
		t.Errorf("missing depends_on in:\n%s", result)
	}
	if !strings.Contains(result, `  - "abc12def"`) {
		t.Errorf("missing dep in:\n%s", result)
	}
}

func TestGenerate_OmitsEmptyDependsOn(t *testing.T) {
	result := Generate(&GenerateOptions{
		Title:     "Task",
		Type:      "task",
		DependsOn: []string{},
	})
	if strings.Contains(result, "depends_on:") {
		t.Errorf("should not contain depends_on for empty array in:\n%s", result)
	}
}

func TestGenerate_WithExecutionContext(t *testing.T) {
	result := Generate(&GenerateOptions{
		Title:     "Full task context",
		Type:      "task",
		Status:    "pending",
		Priority:  "high",
		Workdir:   "projects/my-project",
		GitRemote: "git@github.com:user/repo.git",
		GitBranch: "main",
	})
	if !strings.Contains(result, "workdir: projects/my-project") {
		t.Errorf("missing workdir in:\n%s", result)
	}
	if !strings.Contains(result, `git_remote: "git@github.com:user/repo.git"`) {
		t.Errorf("missing git_remote in:\n%s", result)
	}
	if !strings.Contains(result, "git_branch: main") {
		t.Errorf("missing git_branch in:\n%s", result)
	}
}

func TestGenerate_WithGeneratedMetadata(t *testing.T) {
	tr := true
	result := Generate(&GenerateOptions{
		Title:         "Generated task",
		Type:          "task",
		Generated:     &tr,
		GeneratedKind: "gap_task",
		GeneratedKey:  "feature-checkout:missing-tests",
		GeneratedBy:   "feature-checkout",
	})
	if !strings.Contains(result, "generated: true") {
		t.Errorf("missing generated in:\n%s", result)
	}
	if !strings.Contains(result, "generated_kind: gap_task") {
		t.Errorf("missing generated_kind in:\n%s", result)
	}
}

func TestGenerate_WithMergeIntent(t *testing.T) {
	tr := true
	result := Generate(&GenerateOptions{
		Title:             "Task with merge intent",
		Type:              "task",
		MergeTargetBranch: "main",
		MergePolicy:       "auto_merge",
		MergeStrategy:     "squash",
		OpenPRBeforeMerge: &tr,
		ExecutionMode:     "worktree",
	})
	if !strings.Contains(result, "merge_target_branch: main") {
		t.Errorf("missing merge_target_branch in:\n%s", result)
	}
	if !strings.Contains(result, "merge_policy: auto_merge") {
		t.Errorf("missing merge_policy in:\n%s", result)
	}
	if !strings.Contains(result, "open_pr_before_merge: true") {
		t.Errorf("missing open_pr_before_merge in:\n%s", result)
	}
}

func TestGenerate_OmitsUnsetFields(t *testing.T) {
	result := Generate(&GenerateOptions{
		Title: "Task without context",
		Type:  "task",
	})
	for _, field := range []string{"workdir:", "git_remote:", "git_branch:"} {
		if strings.Contains(result, field) {
			t.Errorf("should not contain %s in:\n%s", field, result)
		}
	}
}

// =============================================================================
// Sanitization tests
// =============================================================================

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "Simple Title", "Simple Title"},
		{"newlines", "Line1\nLine2", "Line1 Line2"},
		{"preserves quotes", `Say "hello"`, `Say "hello"`},
		{"preserves backslashes", `path\to\file`, `path\to\file`},
		{"preserves colons", "Fix: this bug", "Fix: this bug"},
		{"collapses whitespace", "too   many   spaces", "too many spaces"},
		{"trims", "  spaced  ", "spaced"},
		{"tabs", "Title\twith\ttabs", "Title with tabs"},
		{"truncates to 200", strings.Repeat("a", 250), strings.Repeat("a", 200)},
		{"strips null bytes", "Title\x00here", "Titlehere"},
		{"strips control chars", "Title\x01\x02here", "Titlehere"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeTitle(tt.input); got != tt.want {
				t.Errorf("NormalizeTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeTitle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips newlines", "Fix:\nbug", "Fix: bug"},
		{"strips CR", "Title\r\nhere", "Title here"},
		{"strips null bytes", "Title\x00here", "Titlehere"},
		{"trims", "  spaced  ", "spaced"},
		{"collapses whitespace", "too   many   spaces", "too many spaces"},
		{"truncates to 200", strings.Repeat("a", 250), strings.Repeat("a", 200)},
		{"preserves colons", "Fix: Update API", "Fix: Update API"},
		{"tabs", "Title\twith\ttabs", "Title with tabs"},
		{"escapes double quotes", `Say "hello"`, `Say \"hello\"`},
		{"escapes backslashes", `path\to\file`, `path\\to\\file`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeTitle(tt.input); got != tt.want {
				t.Errorf("SanitizeTitle(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSanitizeTag(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
		isNil bool
	}{
		{"empty", "", "", true},
		{"whitespace only", "   ", "", true},
		{"colon+space", "bad: tag", "", true},
		{"key: value", "key: value", "", true},
		{"bare colon", "key:value", "key:value", false},
		{"complex colon", "monitor:feature-review:feature:my-feat:my-proj", "monitor:feature-review:feature:my-feat:my-proj", false},
		{"strips newlines", "bad\ntag", "badtag", false},
		{"trims", "  tag  ", "tag", false},
		{"strips CR", "tag\rwith\rcr", "tagwithcr", false},
		{"strips null bytes", "tag\x00null", "tagnull", false},
		{"strips tabs", "tag\twith\ttab", "tagwithtab", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SanitizeTag(tt.input)
			if tt.isNil {
				if ok {
					t.Errorf("SanitizeTag(%q) = (%q, true), want (_, false)", tt.input, got)
				}
			} else {
				if !ok {
					t.Errorf("SanitizeTag(%q) = (_, false), want (%q, true)", tt.input, tt.want)
				} else if got != tt.want {
					t.Errorf("SanitizeTag(%q) = %q, want %q", tt.input, got, tt.want)
				}
			}
		})
	}
}

func TestSanitizeSimpleValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"strips newlines", "path/to\n/file", "path/to /file"},
		{"preserves colons", "git@github.com:user/repo", "git@github.com:user/repo"},
		{"strips CR", "path\r\nto", "path to"},
		{"strips null bytes", "path\x00to", "pathto"},
		{"trims", "  path  ", "path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeSimpleValue(tt.input); got != tt.want {
				t.Errorf("SanitizeSimpleValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// =============================================================================
// Round-trip tests
// =============================================================================

func TestRoundTrip_BasicFields(t *testing.T) {
	fm := &Frontmatter{
		Title:    "Round Trip Test",
		Type:     "task",
		Status:   "pending",
		Priority: "high",
	}

	serialized := Serialize(fm)
	content := "---\n" + serialized + "---\n\nBody content"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Title != fm.Title {
		t.Errorf("title = %q, want %q", doc.Frontmatter.Title, fm.Title)
	}
	if doc.Frontmatter.Type != fm.Type {
		t.Errorf("type = %q, want %q", doc.Frontmatter.Type, fm.Type)
	}
	if doc.Frontmatter.Status != fm.Status {
		t.Errorf("status = %q, want %q", doc.Frontmatter.Status, fm.Status)
	}
	if doc.Frontmatter.Priority != fm.Priority {
		t.Errorf("priority = %q, want %q", doc.Frontmatter.Priority, fm.Priority)
	}
}

func TestRoundTrip_ScheduleFields(t *testing.T) {
	fm := &Frontmatter{
		Title:    "Schedule Round Trip",
		Type:     "task",
		Status:   "pending",
		Schedule: "*/15 * * * *",
		NextRun:  "2026-03-01T11:15:00.000Z",
		Runs: []CronRun{
			{
				RunID:      "run_100",
				Status:     "completed",
				Started:    "2026-03-01T11:00:00.000Z",
				Completed:  "2026-03-01T11:01:30.000Z",
				Duration:   "90s",
				Tasks:      "2",
				FailedTask: "",
				SkipReason: "",
			},
		},
	}

	serialized := Serialize(fm)
	content := "---\n" + serialized + "---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.Schedule != fm.Schedule {
		t.Errorf("schedule = %q, want %q", doc.Frontmatter.Schedule, fm.Schedule)
	}
	if doc.Frontmatter.NextRun != fm.NextRun {
		t.Errorf("next_run = %q, want %q", doc.Frontmatter.NextRun, fm.NextRun)
	}
	if len(doc.Frontmatter.Runs) != 1 {
		t.Fatalf("runs length = %d, want 1", len(doc.Frontmatter.Runs))
	}
	r := doc.Frontmatter.Runs[0]
	if r.RunID != "run_100" {
		t.Errorf("run_id = %q, want %q", r.RunID, "run_100")
	}
}

func TestRoundTrip_ScheduleEnabled(t *testing.T) {
	f := false
	fm := &Frontmatter{
		Title:           "Round Trip False",
		Type:            "task",
		Status:          "completed",
		Schedule:        "0 * * * *",
		ScheduleEnabled: &f,
	}

	serialized := Serialize(fm)
	content := "---\n" + serialized + "---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.ScheduleEnabled == nil || *doc.Frontmatter.ScheduleEnabled != false {
		t.Errorf("schedule_enabled = %v, want false", doc.Frontmatter.ScheduleEnabled)
	}
}

func TestRoundTrip_DirectPromptWithCodeBlocks(t *testing.T) {
	prompt := "Fix this function:\n```typescript\nfunction add(a: number, b: number): number {\n  return a - b; // bug: should be +\n}\n```\nThen run the tests."

	fm := &Frontmatter{
		Title:        "Fix Bug",
		Type:         "task",
		Status:       "pending",
		DirectPrompt: prompt,
	}

	serialized := Serialize(fm)
	content := "---\n" + serialized + "---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.DirectPrompt != prompt {
		t.Errorf("direct_prompt = %q, want %q", doc.Frontmatter.DirectPrompt, prompt)
	}
}

func TestRoundTrip_UserOriginalRequestComplex(t *testing.T) {
	request := "User request with everything:\n- Bullet points\n- Colons: like this\n- Quotes: \"double\" and 'single'\n- Special: @mentions #hashtags"

	fm := &Frontmatter{
		Title:               "Complex Task",
		Type:                "task",
		Status:              "pending",
		UserOriginalRequest: request,
	}

	serialized := Serialize(fm)
	content := "---\n" + serialized + "---\n\nBody"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.UserOriginalRequest != request {
		t.Errorf("user_original_request = %q, want %q", doc.Frontmatter.UserOriginalRequest, request)
	}
}

func TestRoundTrip_AllOpenCodeOptions(t *testing.T) {
	fm := &Frontmatter{
		Title:        "Round Trip Task",
		Type:         "task",
		Status:       "pending",
		DirectPrompt: "Step 1: Read\nStep 2: Fix\nSpecial: @#$%",
		Agent:        "tdd-dev",
		Model:        "anthropic/claude-sonnet-4-20250514",
	}

	serialized := Serialize(fm)
	content := "---\n" + serialized + "---\n\nTask body"

	doc, err := Parse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if doc.Frontmatter.DirectPrompt != fm.DirectPrompt {
		t.Errorf("direct_prompt = %q, want %q", doc.Frontmatter.DirectPrompt, fm.DirectPrompt)
	}
	if doc.Frontmatter.Agent != fm.Agent {
		t.Errorf("agent = %q, want %q", doc.Frontmatter.Agent, fm.Agent)
	}
	if doc.Frontmatter.Model != fm.Model {
		t.Errorf("model = %q, want %q", doc.Frontmatter.Model, fm.Model)
	}
}

// =============================================================================
// Helpers
// =============================================================================

func boolPtr(b bool) *bool {
	return &b
}
