package runner

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// readPromptFileForTest reads a prompt file written by Spawn.
func readPromptFileForTest(t *testing.T, path string) string {
	t.Helper()
	if path == "" {
		t.Fatalf("empty prompt file path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	return string(b)
}

// stubStartHeadlessServer replaces the serve-startup hook so tests get a
// deterministic port without spawning a real `opencode serve`. Returns a
// restore func.
func stubStartHeadlessServer(port int) func() {
	prev := startHeadlessServerFn
	startHeadlessServerFn = func(_ *OpenCodeExecutor, _, _, _ string) (int, map[string]struct{}, Process, error) {
		return port, map[string]struct{}{}, noopProcess{}, nil
	}
	return func() { startHeadlessServerFn = prev }
}

// stubCreateOpencodeSession replaces the session-creation hook. Returns a
// restore func.
func stubCreateOpencodeSession(fn func(port int, title string) (string, error)) func() {
	prev := createOpencodeSessionFn
	createOpencodeSessionFn = fn
	return func() { createOpencodeSessionFn = prev }
}

// noopProcess is a Process that is immediately "done" for serve-lifetime
// goroutines in tests.
type noopProcess struct{}

func (noopProcess) Pid() int                 { return 424242 }
func (noopProcess) Kill(sig os.Signal) error { return nil }
func (noopProcess) Exited() bool             { return true }
func (noopProcess) ExitCode() int            { return 0 }

// resumeTestTask returns a task with realistic content for resume-prompt tests.
func resumeTestTask() *types.ResolvedTask {
	t := testResolvedTask("resume123")
	t.Content = "---\ntitle: Resume test\nstatus: in_progress\n---\n\n# Resume test\n\nAuthoritative brain task body that must always survive."
	return t
}

const (
	testInjected = "SUPERVISOR SAYS: pivot to the retry path and stop touching config.go"
)

// =============================================================================
// buildRehydratePrompt
// =============================================================================

func TestBuildRehydratePrompt_IncludesHeaderPreambleAndContent(t *testing.T) {
	task := resumeTestTask()
	prompt := buildRehydratePrompt(task, "some prior transcript", testInjected, defaultRehydrateBudgetBytes)

	if !strings.Contains(prompt, "Task Assignment") {
		t.Errorf("rehydrate prompt should contain task assignment header, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, task.ID) {
		t.Errorf("rehydrate prompt should contain task ID %q", task.ID)
	}
	if !strings.Contains(prompt, task.Path) {
		t.Errorf("rehydrate prompt should contain brain path %q", task.Path)
	}
	// resume=true header marker
	if !strings.Contains(prompt, "Resume:") {
		t.Errorf("rehydrate prompt should mark the header as a resume, got:\n%s", prompt)
	}
	// brain-runner-queue guidance in the preamble
	if !strings.Contains(prompt, "brain-runner-queue") {
		t.Errorf("rehydrate prompt should instruct loading brain-runner-queue skill")
	}
	if !strings.Contains(prompt, "brain_recall") {
		t.Errorf("rehydrate prompt should instruct brain_recall on the task path")
	}
	// authoritative task content is included verbatim (never truncated)
	if !strings.Contains(prompt, "Authoritative brain task body that must always survive.") {
		t.Errorf("rehydrate prompt must include the full task content, got:\n%s", prompt)
	}
}

func TestBuildRehydratePrompt_InjectedContextLastAndDelimited(t *testing.T) {
	task := resumeTestTask()
	prompt := buildRehydratePrompt(task, "prior transcript", testInjected, defaultRehydrateBudgetBytes)

	if !strings.Contains(prompt, testInjected) {
		t.Fatalf("rehydrate prompt must contain injected context verbatim")
	}
	if !strings.Contains(prompt, "--- Supervisor injected context ---") {
		t.Errorf("rehydrate prompt should delimit injected context with a clear marker")
	}
	if !strings.Contains(prompt, "--- end supervisor injected context ---") {
		t.Errorf("rehydrate prompt should close the injected context block")
	}
	// injected context must be LAST — freshest instruction wins
	idxInjected := strings.Index(prompt, testInjected)
	idxContent := strings.Index(prompt, "Authoritative brain task body that must always survive.")
	idxTranscript := strings.Index(prompt, "prior transcript")
	if idxInjected < idxContent {
		t.Errorf("injected context must appear after task content (idx %d vs %d)", idxInjected, idxContent)
	}
	if idxInjected < idxTranscript {
		t.Errorf("injected context must appear after prior transcript (idx %d vs %d)", idxInjected, idxTranscript)
	}
}

func TestBuildRehydratePrompt_TruncatesTranscriptButPreservesAuthoritative(t *testing.T) {
	task := resumeTestTask()
	// A big transcript that clearly exceeds any reasonable per-section budget.
	head := strings.Repeat("HEADLINE_MARKER ", 4000) // ~64KB
	tail := strings.Repeat("TAILLINE_MARKER ", 4000) // ~64KB
	transcript := head + "MIDDLE_SHOULD_BE_ELIDED " + tail

	budget := 8192
	prompt := buildRehydratePrompt(task, transcript, testInjected, budget)

	// The transcript section must be bounded. Locate the transcript block and
	// assert its size respects the budget. The transcript section runs from its
	// header to the START of the next section (task content, which is
	// authoritative and unbounded), so bound the span there — NOT at the
	// injected block, which sits after the task content.
	sectionHeader := "## Prior session transcript (bounded)"
	start := strings.Index(prompt, sectionHeader)
	if start < 0 {
		t.Fatalf("expected a bounded transcript section header, got:\n%s", prompt[:min(len(prompt), 500)])
	}
	end := strings.Index(prompt, "Task content from Brain API:")
	if end < 0 {
		end = strings.Index(prompt, "--- Supervisor injected context ---")
	}
	if end < 0 {
		end = len(prompt)
	}
	transcriptSection := prompt[start:end]
	if len(transcriptSection) > budget {
		t.Errorf("transcript section size %d exceeds budget %d", len(transcriptSection), budget)
	}

	// Elision marker must be present.
	if !strings.Contains(transcriptSection, "transcript truncated") {
		t.Errorf("expected an elision marker in the truncated transcript, got section:\n%s", transcriptSection[:min(len(transcriptSection), 300)])
	}
	// Head and tail should both survive (head+tail truncation).
	if !strings.Contains(transcriptSection, "HEADLINE_MARKER") {
		t.Errorf("truncated transcript should keep the head")
	}
	if !strings.Contains(transcriptSection, "TAILLINE_MARKER") {
		t.Errorf("truncated transcript should keep the tail")
	}
	// The middle must be gone.
	if strings.Contains(transcriptSection, "MIDDLE_SHOULD_BE_ELIDED") {
		t.Errorf("truncated transcript should elide the middle")
	}

	// Authoritative material must be intact regardless of budget pressure.
	if !strings.Contains(prompt, "Authoritative brain task body that must always survive.") {
		t.Errorf("task content must never be truncated")
	}
	if !strings.Contains(prompt, testInjected) {
		t.Errorf("injected context must never be truncated")
	}
}

func TestBuildRehydratePrompt_NoTranscriptOmitsSection(t *testing.T) {
	task := resumeTestTask()
	prompt := buildRehydratePrompt(task, "", testInjected, defaultRehydrateBudgetBytes)
	if strings.Contains(prompt, "## Prior session transcript") {
		t.Errorf("no transcript should mean no transcript section, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, testInjected) {
		t.Errorf("injected context should still be present without a transcript")
	}
}

func TestBuildRehydratePrompt_NoInjectedOmitsBlock(t *testing.T) {
	task := resumeTestTask()
	prompt := buildRehydratePrompt(task, "prior transcript", "", defaultRehydrateBudgetBytes)
	if strings.Contains(prompt, "Supervisor injected context") {
		t.Errorf("no injected context should mean no injected block, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Authoritative brain task body that must always survive.") {
		t.Errorf("task content should still be present")
	}
}

func TestBuildRehydratePrompt_ZeroBudgetUsesDefault(t *testing.T) {
	task := resumeTestTask()
	// A transcript larger than the default budget so we can prove the default
	// budget is applied (transcript gets truncated) when budgetBytes <= 0.
	transcript := strings.Repeat("X", defaultRehydrateBudgetBytes*2)
	prompt := buildRehydratePrompt(task, transcript, testInjected, 0)

	start := strings.Index(prompt, "## Prior session transcript (bounded)")
	if start < 0 {
		t.Fatalf("expected transcript section")
	}
	end := strings.Index(prompt, "Task content from Brain API:")
	if end < 0 {
		end = strings.Index(prompt, "--- Supervisor injected context ---")
	}
	if end < 0 {
		end = len(prompt)
	}
	if len(prompt[start:end]) > defaultRehydrateBudgetBytes {
		t.Errorf("with zero budget, transcript should be bounded by default budget %d, got %d", defaultRehydrateBudgetBytes, len(prompt[start:end]))
	}
}

// =============================================================================
// buildSameSessionResumePrompt
// =============================================================================

func TestBuildSameSessionResumePrompt_ContainsInjectedNotFullContent(t *testing.T) {
	task := resumeTestTask()
	prompt := buildSameSessionResumePrompt(task, testInjected)

	if !strings.Contains(prompt, testInjected) {
		t.Errorf("same-session prompt must contain injected context")
	}
	if !strings.Contains(prompt, "--- Supervisor injected context ---") {
		t.Errorf("same-session prompt should delimit injected context")
	}
	// The full task body must NOT be dumped again — the session already has it.
	if strings.Contains(prompt, "Authoritative brain task body that must always survive.") {
		t.Errorf("same-session prompt should NOT re-dump the full task content, got:\n%s", prompt)
	}
	// A continue preamble must be present.
	if !strings.Contains(prompt, "continuing") {
		t.Errorf("same-session prompt should contain a continue preamble, got:\n%s", prompt)
	}
	// Header still pins the task identity.
	if !strings.Contains(prompt, task.ID) {
		t.Errorf("same-session prompt should still pin task ID %q", task.ID)
	}
}

// =============================================================================
// selectResumePrompt
// =============================================================================

func TestSelectResumePrompt_SameSessionAllowed(t *testing.T) {
	task := resumeTestTask()
	opts := SpawnOptions{
		ResumeMode:      ResumeModeSameSession,
		ResumeSessionID: "ses_stored",
		InjectedContext: testInjected,
		PriorTranscript: "prior transcript that should NOT appear",
	}
	got := selectResumePrompt(task, opts, true)
	want := buildSameSessionResumePrompt(task, testInjected)
	if got != want {
		t.Errorf("same-session (allowed) should return the same-session prompt.\n got:\n%s\nwant:\n%s", got, want)
	}
	// prior transcript should NOT leak into a same-session prompt
	if strings.Contains(got, "prior transcript that should NOT appear") {
		t.Errorf("same-session prompt should not contain the prior transcript")
	}
}

func TestSelectResumePrompt_SameSessionNotAllowedCoercesToRehydrate(t *testing.T) {
	task := resumeTestTask()
	opts := SpawnOptions{
		ResumeMode:      ResumeModeSameSession,
		ResumeSessionID: "ses_stored",
		InjectedContext: testInjected,
		PriorTranscript: "prior transcript body",
	}
	got := selectResumePrompt(task, opts, false) // Pi: same-session not allowed
	want := buildRehydratePrompt(task, opts.PriorTranscript, opts.InjectedContext, defaultRehydrateBudgetBytes)
	if got != want {
		t.Errorf("same-session on a non-capable executor should coerce to rehydrate")
	}
}

func TestSelectResumePrompt_Rehydrate(t *testing.T) {
	task := resumeTestTask()
	opts := SpawnOptions{
		ResumeMode:      ResumeModeRehydrate,
		InjectedContext: testInjected,
		PriorTranscript: "prior transcript body",
	}
	got := selectResumePrompt(task, opts, true)
	want := buildRehydratePrompt(task, opts.PriorTranscript, opts.InjectedContext, defaultRehydrateBudgetBytes)
	if got != want {
		t.Errorf("rehydrate mode should return the rehydrate prompt")
	}
}

func TestSelectResumePrompt_EmptyFallsBackToLegacy(t *testing.T) {
	task := resumeTestTask()
	opts := SpawnOptions{
		ResumeMode: "",
		IsResume:   true,
	}
	got := selectResumePrompt(task, opts, true)
	want := CommonBuildPrompt(task, true)
	if got != want {
		t.Errorf("empty resume mode should fall back to legacy CommonBuildPrompt")
	}

	// And with IsResume=false too.
	opts2 := SpawnOptions{ResumeMode: "", IsResume: false}
	got2 := selectResumePrompt(task, opts2, true)
	want2 := CommonBuildPrompt(task, false)
	if got2 != want2 {
		t.Errorf("empty resume mode (non-resume) should fall back to legacy CommonBuildPrompt(false)")
	}
}

// =============================================================================
// OpenCode Spawn same-session wiring
// =============================================================================

// TestSpawn_SameSession_UsesStoredSessionNoCreate verifies that a same-session
// resume pins the STORED session id onto the run command and does NOT create a
// fresh session via createOpencodeSession.
func TestSpawn_SameSession_UsesStoredSessionNoCreate(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir
	// Control enabled so the serve+attach (session-pinning) path is exercised.

	// Stub the serve startup so we get a deterministic port without a real
	// process, and stub createOpencodeSession so we can detect invocation.
	restoreServe := stubStartHeadlessServer(4242)
	defer restoreServe()

	created := false
	restoreCreate := stubCreateOpencodeSession(func(port int, title string) (string, error) {
		created = true
		return "ses_freshly_created", nil
	})
	defer restoreCreate()

	var gotArgs []string
	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		gotArgs = args
		return exec.Command("/bin/echo", "spawned")
	}

	task := resumeTestTask()
	res, err := e.Spawn(context.Background(), task, "proj", SpawnOptions{
		Mode:            ExecutionModeHeadless,
		Workdir:         t.TempDir(),
		ResumeMode:      ResumeModeSameSession,
		ResumeSessionID: "ses_stored",
		InjectedContext: testInjected,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if created {
		t.Errorf("same-session resume must NOT create a fresh session")
	}
	if res.SessionID != "ses_stored" {
		t.Errorf("SpawnResult.SessionID = %q, want ses_stored", res.SessionID)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "--session ses_stored") {
		t.Errorf("run args should pin the STORED session, got: %s", joined)
	}
	if strings.Contains(joined, "ses_freshly_created") {
		t.Errorf("run args must not reference a freshly-created session")
	}
}

// TestSpawn_Rehydrate_CreatesFreshSession verifies rehydrate mode creates a
// fresh session (createOpencodeSession invoked) and uses the rehydrate prompt.
func TestSpawn_Rehydrate_CreatesFreshSession(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	restoreServe := stubStartHeadlessServer(4243)
	defer restoreServe()

	created := false
	restoreCreate := stubCreateOpencodeSession(func(port int, title string) (string, error) {
		created = true
		return "ses_fresh", nil
	})
	defer restoreCreate()

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "spawned")
	}

	task := resumeTestTask()
	res, err := e.Spawn(context.Background(), task, "proj", SpawnOptions{
		Mode:            ExecutionModeHeadless,
		Workdir:         t.TempDir(),
		ResumeMode:      ResumeModeRehydrate,
		ResumeSessionID: "ses_ignored_in_rehydrate",
		InjectedContext: testInjected,
		PriorTranscript: "prior transcript body",
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if !created {
		t.Errorf("rehydrate resume must create a fresh session")
	}
	if res.SessionID != "ses_fresh" {
		t.Errorf("SpawnResult.SessionID = %q, want ses_fresh", res.SessionID)
	}

	// The prompt file should contain the rehydrate assembly, not the stored id.
	prompt := readPromptFileForTest(t, res.PromptFile)
	if !strings.Contains(prompt, testInjected) {
		t.Errorf("rehydrate prompt file should contain injected context")
	}
	if !strings.Contains(prompt, "Authoritative brain task body that must always survive.") {
		t.Errorf("rehydrate prompt file should contain full task content")
	}
}

// TestSpawn_LegacyEmptyResumeMode_Unchanged verifies the legacy path still
// creates a fresh session and uses the legacy CommonBuildPrompt.
func TestSpawn_LegacyEmptyResumeMode_Unchanged(t *testing.T) {
	stateDir := t.TempDir()
	cfg := testExecutorConfig()
	cfg.StateDir = stateDir

	restoreServe := stubStartHeadlessServer(4244)
	defer restoreServe()

	created := false
	restoreCreate := stubCreateOpencodeSession(func(port int, title string) (string, error) {
		created = true
		return "ses_legacy", nil
	})
	defer restoreCreate()

	e := NewExecutor(cfg)
	e.CommandFactory = func(name string, args ...string) *exec.Cmd {
		return exec.Command("/bin/echo", "spawned")
	}

	task := resumeTestTask()
	res, err := e.Spawn(context.Background(), task, "proj", SpawnOptions{
		Mode:    ExecutionModeHeadless,
		Workdir: t.TempDir(),
		// ResumeMode empty → legacy.
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	if !created {
		t.Errorf("legacy path must create a fresh session")
	}
	if res.SessionID != "ses_legacy" {
		t.Errorf("SpawnResult.SessionID = %q, want ses_legacy", res.SessionID)
	}
	prompt := readPromptFileForTest(t, res.PromptFile)
	want := CommonBuildPrompt(task, false)
	if prompt != want {
		t.Errorf("legacy prompt file should equal CommonBuildPrompt(task,false)")
	}
}
