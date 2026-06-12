package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/types"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// =============================================================================
// TaskDetail - Brain Entry Attachments
// =============================================================================

func TestTaskDetail_EntryMode_RendersAttachmentMetadata(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(100, 30)
	attachments := []types.AttachmentReference{{
		ID:          "att_123",
		Role:        "source",
		Filename:    "evidence.pdf",
		ContentType: "application/pdf",
		Size:        1536,
		Derived: []types.AttachmentDerived{{
			ID:          "drv_1",
			Kind:        "text",
			ContentType: "text/markdown",
			Size:        512,
		}},
		DerivedText: &types.AttachmentDerivedText{
			Status: types.AttachmentExtractionStatusReady,
			Metadata: map[string]string{
				"provider": "openrouter",
				"model":    "google/gemini-2.5-flash",
			},
		},
	}}
	td.SetEntryContentWithAttachments("projects/brain-api/report/r.md", "Report", "report", "entry body", attachments, "Entry Detail")

	view := stripANSI(td.View())
	for _, want := range []string{
		"Attachments (1)",
		"att_123",
		"source",
		"evidence.pdf",
		"application/pdf",
		"1.5 KB",
		"extracted: text (text/markdown, 512 B)",
		"Text: ready",
		"Extraction: ready",
		"Model: openrouter / google/gemini-2.5-flash",
		"search: available",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected entry attachment detail to contain %q, got:\n%s", want, view)
		}
	}
}

// =============================================================================
// TaskDetail - No Task Selected
// =============================================================================

func TestTaskDetail_NoTask_ShowsPlaceholder(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(60, 20)

	view := stripANSI(td.View())

	if !strings.Contains(view, "No task selected") {
		t.Errorf("expected 'No task selected' placeholder, got:\n%s", view)
	}
}

func TestTaskDetail_NoTask_ShowsHeader(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(60, 20)

	view := td.View()

	if !strings.Contains(view, "Task Detail") {
		t.Errorf("expected 'Task Detail' header, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Task With All Fields
// =============================================================================

func TestTaskDetail_WithTask_ShowsTitle(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:             "abc12def",
		Title:          "Implement auth module",
		Status:         "pending",
		Priority:       "high",
		Path:           "projects/brain/task/abc12def.md",
		Classification: "ready",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "Implement auth module") {
		t.Errorf("expected task title in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsStatus(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:             "abc12def",
		Title:          "Test task",
		Status:         "pending",
		Classification: "ready",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "pending") {
		t.Errorf("expected status 'pending' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "ready") {
		t.Errorf("expected classification 'ready' in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsPriority(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:       "abc12def",
		Title:    "Test task",
		Priority: "high",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "high") {
		t.Errorf("expected priority 'high' in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsID(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Test task",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "abc12def") {
		t.Errorf("expected ID 'abc12def' in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsPath(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Test task",
		Path:  "projects/brain/task/abc12def.md",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "projects/brain/task/abc12def.md") {
		t.Errorf("expected path in view, got:\n%s", view)
	}
}

func TestTaskDetail_WithTask_ShowsContent(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:      "abc12def",
		Title:   "Test task",
		Content: "Implement the actual task body.\nInclude acceptance criteria.",
	}
	td.SetTask(task)

	view := stripANSI(td.View())

	for _, want := range []string{"Content:", "Implement the actual task body.", "Include acceptance criteria."} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in view, got:\n%s", want, view)
		}
	}
}

func TestTaskDetail_WithTask_ShowsContentAfterFrontmatter(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:                  "abc12def",
		Title:               "Test task",
		Content:             "Long task body should be last.",
		UserOriginalRequest: "Original request metadata",
	}
	td.SetTask(task)

	view := stripANSI(td.View())
	frontmatterIdx := strings.Index(view, "Frontmatter:")
	contentIdx := strings.Index(view, "Content:")
	if frontmatterIdx == -1 {
		t.Fatalf("expected Frontmatter section, got:\n%s", view)
	}
	if contentIdx == -1 {
		t.Fatalf("expected Content section, got:\n%s", view)
	}
	if contentIdx < frontmatterIdx {
		t.Fatalf("expected Content section after Frontmatter, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Dependencies
// =============================================================================

func TestTaskDetail_WithDependencies_ShowsDependsOn(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:        "abc12def",
		Title:     "Test task",
		DependsOn: []string{"dep1", "dep2"},
	}
	td.SetTask(task)

	view := td.View()

	// Check for dependency values (the "Dependencies:" header may have ANSI
	// codes splitting the word when lipgloss TrueColor profile is active)
	if !strings.Contains(view, "dep1") {
		t.Errorf("expected 'dep1' in dependencies, got:\n%s", view)
	}
	if !strings.Contains(view, "dep2") {
		t.Errorf("expected 'dep2' in dependencies, got:\n%s", view)
	}
}

func TestTaskDetail_NoDependencies_NoDepsSection(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Test task",
	}
	td.SetTask(task)

	view := td.View()

	if strings.Contains(view, "Dependencies") {
		t.Errorf("expected no 'Dependencies' section when no deps, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Blocked Task
// =============================================================================

func TestTaskDetail_BlockedTask_ShowsBlockedBy(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:              "abc12def",
		Title:           "Blocked task",
		Classification:  "blocked",
		BlockedBy:       []string{"blocker1", "blocker2"},
		BlockedByReason: "dependency failed",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "Blocked by") {
		t.Errorf("expected 'Blocked by' section, got:\n%s", view)
	}
	if !strings.Contains(view, "blocker1") {
		t.Errorf("expected 'blocker1' in blocked by, got:\n%s", view)
	}
	if !strings.Contains(view, "dependency failed") {
		t.Errorf("expected blocked reason in view, got:\n%s", view)
	}
}

func TestTaskDetail_WaitingTask_ShowsWaitingOn(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:             "abc12def",
		Title:          "Waiting task",
		Classification: "waiting",
		WaitingOn:      []string{"wait1", "wait2"},
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "Waiting on") {
		t.Errorf("expected 'Waiting on' section, got:\n%s", view)
	}
	if !strings.Contains(view, "wait1") {
		t.Errorf("expected 'wait1' in waiting on, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Cyclic Task
// =============================================================================

func TestTaskDetail_CyclicTask_ShowsCycleWarning(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:      "abc12def",
		Title:   "Cyclic task",
		InCycle: true,
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "↺") {
		t.Errorf("expected cycle indicator '↺' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "cycle") {
		t.Errorf("expected 'cycle' warning text in view, got:\n%s", view)
	}
}

func TestTaskDetail_NoCycle_NoCycleWarning(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:      "abc12def",
		Title:   "Normal task",
		InCycle: false,
	}
	td.SetTask(task)

	view := td.View()

	if strings.Contains(view, "↺") {
		t.Errorf("expected no cycle indicator when InCycle=false, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - SetSize
// =============================================================================

func TestTaskDetail_SetSize_UpdatesDimensions(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(100, 50)

	if td.width != 100 {
		t.Errorf("expected width 100, got %d", td.width)
	}
	if td.height != 50 {
		t.Errorf("expected height 50, got %d", td.height)
	}
}

// =============================================================================
// TaskDetail - Git Context
// =============================================================================

func TestTaskDetail_WithGitContext_ShowsBranch(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 30)

	task := &types.ResolvedTask{
		ID:        "abc12def",
		Title:     "Git task",
		GitBranch: "feature/auth",
		GitRemote: "origin",
	}
	td.SetTask(task)

	view := td.View()

	if !strings.Contains(view, "feature/auth") {
		t.Errorf("expected git branch in view, got:\n%s", view)
	}
}

// =============================================================================
// TaskDetail - Viewport Scrolling
// =============================================================================

// longTask creates a task with enough fields to generate many content lines.
func longTask() *types.ResolvedTask {
	return &types.ResolvedTask{
		ID:              "abc12def",
		Title:           "Task with lots of info",
		Status:          "pending",
		Priority:        "high",
		Path:            "projects/brain/task/abc12def.md",
		Classification:  "blocked",
		DependsOn:       []string{"dep1", "dep2", "dep3"},
		BlockedBy:       []string{"blocker1"},
		BlockedByReason: "dependency failed",
		WaitingOn:       []string{"wait1"},
		GitBranch:       "feature/test",
		GitRemote:       "origin",
		Workdir:         "~/projects/brain",
		ResolvedWorkdir: "/home/user/projects/brain",
		InCycle:         true,
	}
}

func TestTaskDetail_ViewportScrolling_NoScrollWhenContentFits(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 50) // Large viewport

	td.SetTask(longTask())
	view := td.View()

	if view == "" {
		t.Error("expected non-empty view")
	}
	// Should not show scroll indicators when content fits
	if strings.Contains(view, "▲") {
		t.Error("should not show up-arrow when content fits viewport")
	}
	if strings.Contains(view, "▼") {
		t.Error("should not show down-arrow when content fits viewport")
	}
}

func TestTaskDetail_ViewportScrolling_ShowsDownIndicatorWhenScrollable(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6) // Small viewport to force scrolling

	td.SetTask(longTask())
	view := td.View()

	if !strings.Contains(view, "▼") {
		t.Errorf("expected down-arrow indicator when content exceeds viewport, got:\n%s", view)
	}
}

func TestTaskDetail_ViewportScrolling_ScrollDownShowsUpIndicator(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	td.SetTask(longTask())
	td.View() // Initial render to set totalLines

	td.ScrollDown()
	view := td.View()

	if !strings.Contains(view, "▲") {
		t.Errorf("expected up-arrow indicator after scrolling down, got:\n%s", view)
	}
}

func TestTaskDetail_ViewportScrolling_ScrollToBottomNoDownIndicator(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	td.SetTask(longTask())
	td.View() // Initial render to set totalLines

	td.ScrollToBottom()
	view := td.View()

	if strings.Contains(view, "▼") {
		t.Errorf("should not show down-arrow at bottom, got:\n%s", view)
	}
}

func TestTaskDetail_ViewportScrolling_ScrollUpAtTopIsNoop(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	td.SetTask(longTask())
	td.View()

	offsetBefore := td.scrollOffset
	td.ScrollUp()

	if td.scrollOffset != offsetBefore {
		t.Errorf("scrollOffset should not change when already at top, got %d", td.scrollOffset)
	}
}

func TestTaskDetail_ViewportScrolling_ScrollDownClampsToMax(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	td.SetTask(longTask())
	td.View() // Initial render

	// Scroll way past the end
	for i := 0; i < 100; i++ {
		td.ScrollDown()
	}

	viewportHeight := td.height - 1 // header takes 1 line
	maxOffset := td.totalLines - viewportHeight
	if maxOffset < 0 {
		maxOffset = 0
	}

	if td.scrollOffset > maxOffset {
		t.Errorf("scrollOffset %d exceeds max %d", td.scrollOffset, maxOffset)
	}
}

func TestTaskDetail_ViewportScrolling_SetTaskResetsScroll(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	td.SetTask(longTask())
	td.View()
	td.ScrollDown()
	td.ScrollDown()

	if td.scrollOffset == 0 {
		t.Fatal("expected non-zero scrollOffset after scrolling")
	}

	// Setting a new task should reset scroll
	differentTask := longTask()
	differentTask.ID = "xyz99abc"
	td.SetTask(differentTask)
	if td.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0 after SetTask, got %d", td.scrollOffset)
	}
}

func TestTaskDetail_ViewportScrolling_PositionIndicatorShown(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	td.SetTask(longTask())
	view := td.View()

	// Should show position indicator like (1-5/20)
	if !strings.Contains(view, "/") || !strings.Contains(view, "(") {
		t.Errorf("expected position indicator in header when scrollable, got:\n%s", view)
	}
}

func TestTaskDetail_ViewportScrolling_ScrollToTopAfterScrollDown(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	td.SetTask(longTask())
	td.View()

	td.ScrollDown()
	td.ScrollDown()
	td.ScrollDown()

	if td.scrollOffset == 0 {
		t.Fatal("expected non-zero scrollOffset after scrolling")
	}

	td.ScrollToTop()
	if td.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0 after ScrollToTop, got %d", td.scrollOffset)
	}
}

func TestTaskDetail_ViewportScrolling_EmptyTaskNoScroll(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6)

	// No task set
	td.ScrollDown()
	td.ScrollUp()
	td.ScrollToTop()
	td.ScrollToBottom()

	// Should not panic and offset should stay 0
	if td.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0 with no task, got %d", td.scrollOffset)
	}

	view := td.View()
	if !strings.Contains(view, "No task selected") {
		t.Error("expected placeholder text for empty task")
	}
}

// =============================================================================
// TaskDetail - Feature Mode
// =============================================================================

// testFeature creates a ComputedFeature for testing.
func testFeature() *service.ComputedFeature {
	return &service.ComputedFeature{
		ID:                "auth-system",
		Project:           "brain-api",
		Priority:          "high",
		Status:            "in_progress",
		Classification:    "waiting",
		DependsOnFeatures: []string{"data-layer", "user-profiles"},
		BlockedByFeatures: []string{},
		WaitingOnFeatures: []string{"user-profiles"},
		InCycle:           false,
		TaskStats: service.FeatureTaskStats{
			Total:      5,
			Pending:    2,
			InProgress: 1,
			Completed:  2,
			Blocked:    0,
		},
		Tasks: []types.ResolvedTask{
			{ID: "t1", Title: "Set up auth middleware", Status: "completed"},
			{ID: "t2", Title: "Implement JWT signing", Status: "completed"},
			{ID: "t3", Title: "Login endpoint", Status: "in_progress"},
			{ID: "t4", Title: "Register endpoint", Status: "pending"},
			{ID: "t5", Title: "Password reset flow", Status: "pending"},
		},
	}
}

func TestTaskDetail_SetFeature_ShowsFeatureHeader(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	if !strings.Contains(view, "Feature: auth-system") {
		t.Errorf("expected 'Feature: auth-system' header, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ShowsStatus(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	if !strings.Contains(view, "in_progress") {
		t.Errorf("expected status 'in_progress' in view, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ShowsPriority(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	if !strings.Contains(view, "high") {
		t.Errorf("expected priority 'high' in view, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ShowsTaskStats(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	if !strings.Contains(view, "2/5 complete") {
		t.Errorf("expected task stats '2/5 complete' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "1 active") {
		t.Errorf("expected '1 active' in task stats, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ShowsDependencies(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	if !strings.Contains(view, "data-layer") {
		t.Errorf("expected dependency 'data-layer' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "user-profiles") {
		t.Errorf("expected dependency 'user-profiles' in view, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ShowsDependencyIcons(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	// data-layer is not in WaitingOn or BlockedBy, so it should show completed icon
	if !strings.Contains(view, "✓") {
		t.Errorf("expected completed icon '✓' for data-layer, got:\n%s", view)
	}
	// user-profiles is in WaitingOnFeatures, should show waiting icon
	if !strings.Contains(view, "◷") {
		t.Errorf("expected waiting icon '◷' for user-profiles, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ShowsTasks(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	if !strings.Contains(view, "Set up auth middleware") {
		t.Errorf("expected task 'Set up auth middleware' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Login endpoint") {
		t.Errorf("expected task 'Login endpoint' in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Password reset flow") {
		t.Errorf("expected task 'Password reset flow' in view, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ShowsTaskStatusIcons(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	// Completed tasks get ✓
	if !strings.Contains(view, "✓") {
		t.Errorf("expected completed icon '✓' in view, got:\n%s", view)
	}
	// In-progress tasks get ⚡
	if !strings.Contains(view, "⚡") {
		t.Errorf("expected in_progress icon '⚡' in view, got:\n%s", view)
	}
	// Pending tasks get ◌
	if !strings.Contains(view, "◌") {
		t.Errorf("expected pending icon '◌' in view, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ClearsTaskMode(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	// First set a task
	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Some task",
	}
	td.SetTask(task)
	if td.task == nil {
		t.Fatal("expected task to be set")
	}

	// Now set a feature - should clear task
	f := testFeature()
	td.SetFeature("auth-system", f)

	if td.task != nil {
		t.Error("expected task to be nil after SetFeature")
	}
	if !td.featureMode {
		t.Error("expected featureMode to be true after SetFeature")
	}
}

func TestTaskDetail_SetTask_ClearsFeatureMode(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	// First set a feature
	f := testFeature()
	td.SetFeature("auth-system", f)
	if !td.featureMode {
		t.Fatal("expected featureMode to be true")
	}

	// Now set a task - should clear feature mode
	task := &types.ResolvedTask{
		ID:    "abc12def",
		Title: "Some task",
	}
	td.SetTask(task)

	if td.featureMode {
		t.Error("expected featureMode to be false after SetTask")
	}
	if td.feature != nil {
		t.Error("expected feature to be nil after SetTask")
	}
}

func TestTaskDetail_SetFeature_ResetsScrollOnDifferentFeature(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6) // Small to force scrolling

	f := testFeature()
	td.SetFeature("auth-system", f)
	td.View() // Render to set totalLines
	td.ScrollDown()
	td.ScrollDown()

	if td.scrollOffset == 0 {
		t.Fatal("expected non-zero scrollOffset after scrolling")
	}

	// Setting a different feature should reset scroll
	f2 := testFeature()
	f2.ID = "other-feature"
	td.SetFeature("other-feature", f2)

	if td.scrollOffset != 0 {
		t.Errorf("expected scrollOffset 0 after SetFeature with different ID, got %d", td.scrollOffset)
	}
}

func TestTaskDetail_SetFeature_KeepsScrollOnSameFeature(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6) // Small to force scrolling

	f := testFeature()
	td.SetFeature("auth-system", f)
	td.View()
	td.ScrollDown()
	td.ScrollDown()

	offset := td.scrollOffset
	if offset == 0 {
		t.Fatal("expected non-zero scrollOffset after scrolling")
	}

	// Setting same feature should preserve scroll
	td.SetFeature("auth-system", f)

	if td.scrollOffset != offset {
		t.Errorf("expected scrollOffset %d after SetFeature with same ID, got %d", offset, td.scrollOffset)
	}
}

func TestTaskDetail_SetFeature_CycleWarning(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	f.InCycle = true
	td.SetFeature("auth-system", f)
	view := td.View()

	if !strings.Contains(view, "↺") {
		t.Errorf("expected cycle indicator '↺' in feature view, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_NoCycleWarningWhenFalse(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	f.InCycle = false
	td.SetFeature("auth-system", f)
	view := td.View()

	if strings.Contains(view, "↺") {
		t.Errorf("expected no cycle indicator when InCycle=false, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_Dependents(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 40)

	f := testFeature()
	f.WaitingOnFeatures = []string{"dashboard"}
	td.SetFeature("auth-system", f)
	view := td.View()

	// Check for dependent feature name and its annotation
	// (header "Dependents:" may have ANSI codes splitting letters with lipgloss underline)
	if !strings.Contains(view, "dashboard") {
		t.Errorf("expected 'dashboard' in dependents, got:\n%s", view)
	}
	if !strings.Contains(view, "waiting on this") {
		t.Errorf("expected 'waiting on this' annotation, got:\n%s", view)
	}
}

func TestTaskDetail_SetFeature_ScrollingWorks(t *testing.T) {
	td := NewTaskDetail()
	td.SetSize(80, 6) // Very small to force scrolling

	f := testFeature()
	td.SetFeature("auth-system", f)
	view := td.View()

	// Should show scroll indicators when content exceeds viewport
	if !strings.Contains(view, "▼") {
		t.Errorf("expected down-arrow indicator for feature view, got:\n%s", view)
	}

	td.ScrollDown()
	view = td.View()
	if !strings.Contains(view, "▲") {
		t.Errorf("expected up-arrow indicator after scrolling down in feature view, got:\n%s", view)
	}
}
