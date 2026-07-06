package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Migrate Goals Subcommand Tests
// =============================================================================

// legacyGoalPlan returns a representative legacy V1 goal plan entry.
func legacyGoalPlan() types.BrainEntry {
	return types.BrainEntry{
		ID:            "plan0001",
		Path:          "projects/demo/plan/plan0001.md",
		Title:         "Ship dark mode",
		Type:          "plan",
		Status:        "active",
		ProjectID:     "demo",
		Tags:          []string{"goal", "goal:v1", "goal:plan"},
		GeneratedKey:  "goal:ship-dark-mode:plan",
		GeneratedBy:   "brain-goal",
		TargetWorkdir: "/work/demo",
		Agent:         "tdd-dev",
		Model:         "anthropic/claude-sonnet-4-20250514",
		Content: "# Goal: Ship dark mode\n\n## Acceptance Criteria\n\n" +
			"- Toggle persists\n- Respects OS preference\n\n## Validation Commands\n\n" +
			"- `npm test`\n\n## Execution Metadata\n\n- agent: tdd-dev\n" +
			"- model: anthropic/claude-sonnet-4-20250514\n- target_workdir: /work/demo\n" +
			"- goal_session_mode: continue\n",
	}
}

// legacyGoalReconciler returns the paired reconciler task for legacyGoalPlan.
func legacyGoalReconciler() types.BrainEntry {
	return types.BrainEntry{
		ID:           "task0001",
		Path:         "projects/demo/task/task0001.md",
		Title:        "Goal Reconcile: Ship dark mode",
		Type:         "task",
		Status:       "pending",
		ProjectID:    "demo",
		Tags:         []string{"goal", "goal:v1", "goal:reconciler"},
		GeneratedKey: "goal:ship-dark-mode:reconcile",
		GeneratedBy:  "brain-goal",
		DirectPrompt: "Reconcile the dark mode goal.",
		Agent:        "tdd-dev",
	}
}

// patchRecord captures a single PATCH (UpdateEntry) request.
type patchRecord struct {
	Path   string
	Status string
	Append string
}

// goalsServer is a configurable httptest handler for the migrate goals flow.
// It routes GET /entries by the type+tags query, records POST /entries
// (CreateEntry) and PATCH /entries/... (UpdateEntry) calls, and is safe for
// concurrent access since httptest handlers run in goroutines.
type goalsServer struct {
	mu sync.Mutex

	plans       []types.BrainEntry // returned for type=plan&tags=goal:plan
	reconcilers []types.BrainEntry // returned for type=task&tags=goal:reconciler
	automations []types.BrainEntry // returned for type=automation&tags=goal

	posts   int
	patches []patchRecord
}

func (g *goalsServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/api/v1/entries"):
			q := r.URL.Query()
			typ := q.Get("type")
			tags := q.Get("tags")

			g.mu.Lock()
			defer g.mu.Unlock()

			var entries []types.BrainEntry
			switch {
			case typ == "plan" && tags == goalPlanTag:
				entries = g.plans
			case typ == "task" && tags == goalReconcilerTag:
				entries = g.reconcilers
			case typ == "automation" && tags == "goal":
				entries = g.automations
			}
			resp := types.ListEntriesResponse{Entries: entries, Total: len(entries)}
			json.NewEncoder(w).Encode(resp)
			return

		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/api/v1/entries"):
			g.mu.Lock()
			g.posts++
			g.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			resp := types.CreateEntryResponse{
				ID:   "newauto1",
				Path: "global/automation/newauto1.md",
				Type: "automation",
			}
			json.NewEncoder(w).Encode(resp)
			return

		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/api/v1/entries/"):
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			status, _ := body["status"].(string)
			appendStr, _ := body["append"].(string)

			g.mu.Lock()
			g.patches = append(g.patches, patchRecord{
				Path:   r.URL.Path,
				Status: status,
				Append: appendStr,
			})
			g.mu.Unlock()

			json.NewEncoder(w).Encode(types.BrainEntry{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

// postCount returns the number of recorded POST /entries calls.
func (g *goalsServer) postCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.posts
}

// patchRecords returns a copy of the recorded PATCH calls.
func (g *goalsServer) patchRecords() []patchRecord {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]patchRecord, len(g.patches))
	copy(out, g.patches)
	return out
}

// newTestMigrateCommand builds a MigrateCommand wired to the given API URL.
func newTestMigrateCommand(apiURL, subcommand string, flags *MigrateFlags, out *bytes.Buffer) *MigrateCommand {
	cfg := testAutomationConfig(apiURL)
	return &MigrateCommand{
		Subcommand: subcommand,
		Config:     cfg,
		Flags:      flags,
		Out:        out,
		apiClient:  runner.NewAPIClient(cfg.Runner),
	}
}

// -----------------------------------------------------------------------------
// 1. Success: plan + reconciler converted, plan archived, reconciler cancelled.
// -----------------------------------------------------------------------------

func TestMigrateGoals_Success(t *testing.T) {
	gs := &goalsServer{
		plans:       []types.BrainEntry{legacyGoalPlan()},
		reconcilers: []types.BrainEntry{legacyGoalReconciler()},
	}
	server := httptest.NewServer(gs.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestMigrateCommand(server.URL, "goals", &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"Created goal automation",
		"Archived legacy plan",
		"Cancelled legacy reconciler",
		"Migration complete!",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}

	if got := gs.postCount(); got != 1 {
		t.Errorf("expected exactly 1 POST /entries, got %d", got)
	}

	patches := gs.patchRecords()
	if len(patches) != 2 {
		t.Fatalf("expected 2 PATCH calls (plan + reconciler), got %d: %+v", len(patches), patches)
	}

	var sawArchived, sawCancelled bool
	for _, p := range patches {
		switch p.Status {
		case "archived":
			sawArchived = true
			if p.Append == "" {
				t.Errorf("plan archive PATCH should have a non-empty append: %+v", p)
			}
			if !strings.Contains(p.Path, "plan0001") {
				t.Errorf("archived PATCH should target the plan path, got %q", p.Path)
			}
		case "cancelled":
			sawCancelled = true
			if p.Append == "" {
				t.Errorf("reconciler cancel PATCH should have a non-empty append: %+v", p)
			}
			if !strings.Contains(p.Path, "task0001") {
				t.Errorf("cancelled PATCH should target the reconciler path, got %q", p.Path)
			}
		default:
			t.Errorf("unexpected PATCH status %q: %+v", p.Status, p)
		}
	}
	if !sawArchived {
		t.Error("expected a PATCH with status=archived for the plan")
	}
	if !sawCancelled {
		t.Error("expected a PATCH with status=cancelled for the reconciler")
	}
}

// -----------------------------------------------------------------------------
// 2. Dry-run: prints intent, writes nothing.
// -----------------------------------------------------------------------------

func TestMigrateGoals_DryRun(t *testing.T) {
	gs := &goalsServer{
		plans:       []types.BrainEntry{legacyGoalPlan()},
		reconcilers: []types.BrainEntry{legacyGoalReconciler()},
	}
	server := httptest.NewServer(gs.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestMigrateCommand(server.URL, "goals", &MigrateFlags{DryRun: true}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	for _, want := range []string{
		"DRY RUN: Would create goal automation",
		"DRY RUN: Would archive legacy plan",
		"DRY RUN Summary:",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}

	if got := gs.postCount(); got != 0 {
		t.Errorf("dry-run must not POST; got %d POST calls", got)
	}
	if patches := gs.patchRecords(); len(patches) != 0 {
		t.Errorf("dry-run must not PATCH; got %d PATCH calls: %+v", len(patches), patches)
	}
}

// -----------------------------------------------------------------------------
// 3. Dedup: existing goal automation skips create unless --force.
// -----------------------------------------------------------------------------

func TestMigrateGoals_DedupSkip(t *testing.T) {
	existing := types.BrainEntry{
		ID:        "existauto",
		Path:      "global/automation/existauto.md",
		Title:     "Goal: Ship dark mode",
		Type:      "automation",
		Status:    "active",
		ProjectID: "demo",
		Tags:      []string{"goal", "goal:ship-dark-mode"},
	}

	gs := &goalsServer{
		plans:       []types.BrainEntry{legacyGoalPlan()},
		reconcilers: []types.BrainEntry{legacyGoalReconciler()},
		automations: []types.BrainEntry{existing},
	}
	server := httptest.NewServer(gs.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestMigrateCommand(server.URL, "goals", &MigrateFlags{Force: false}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Skipped") {
		t.Errorf("output should report a skip:\n%s", output)
	}
	if !strings.Contains(output, "already exists") {
		t.Errorf("output should explain the goal automation already exists:\n%s", output)
	}
	if got := gs.postCount(); got != 0 {
		t.Errorf("dedup skip must not POST; got %d POST calls", got)
	}
}

func TestMigrateGoals_DedupForceRecreates(t *testing.T) {
	existing := types.BrainEntry{
		ID:        "existauto",
		Path:      "global/automation/existauto.md",
		Title:     "Goal: Ship dark mode",
		Type:      "automation",
		Status:    "active",
		ProjectID: "demo",
		Tags:      []string{"goal", "goal:ship-dark-mode"},
	}

	gs := &goalsServer{
		plans:       []types.BrainEntry{legacyGoalPlan()},
		reconcilers: []types.BrainEntry{legacyGoalReconciler()},
		automations: []types.BrainEntry{existing},
	}
	server := httptest.NewServer(gs.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestMigrateCommand(server.URL, "goals", &MigrateFlags{Force: true}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if got := gs.postCount(); got != 1 {
		t.Errorf("--force should recreate the goal automation (expected 1 POST), got %d", got)
	}
	if !strings.Contains(out.String(), "Created goal automation") {
		t.Errorf("--force output should report a creation:\n%s", out.String())
	}
}

// -----------------------------------------------------------------------------
// 4. No legacy goals found.
// -----------------------------------------------------------------------------

func TestMigrateGoals_NoLegacyGoals(t *testing.T) {
	gs := &goalsServer{} // empty plans, reconcilers, automations
	server := httptest.NewServer(gs.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestMigrateCommand(server.URL, "goals", &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if !strings.Contains(out.String(), "No legacy goals found.") {
		t.Errorf("output should report no legacy goals:\n%s", out.String())
	}
	if got := gs.postCount(); got != 0 {
		t.Errorf("empty migration must not POST; got %d", got)
	}
	if patches := gs.patchRecords(); len(patches) != 0 {
		t.Errorf("empty migration must not PATCH; got %d", len(patches))
	}
}

// -----------------------------------------------------------------------------
// 5. API-down: graceful degradation, returns nil.
// -----------------------------------------------------------------------------

func TestMigrateGoals_APIDown(t *testing.T) {
	// 127.0.0.1:1 is effectively unreachable (privileged/closed port).
	var out bytes.Buffer
	cmd := newTestMigrateCommand("http://127.0.0.1:1", "goals", &MigrateFlags{}, &out)

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("API-down should not return an error, got: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "API unavailable") {
		t.Errorf("output should mention API unavailable:\n%s", output)
	}
	if !strings.Contains(output, "Skipping goal migration") {
		t.Errorf("output should mention skipping goal migration:\n%s", output)
	}
}

// -----------------------------------------------------------------------------
// 6. Idempotency: a plan already archived is converted (Step 2) but Step 3
//    skips re-archiving it, issuing no PATCH for that plan.
// -----------------------------------------------------------------------------

func TestMigrateGoals_IdempotentAlreadyArchived(t *testing.T) {
	plan := legacyGoalPlan()
	plan.Status = "archived"

	gs := &goalsServer{
		plans: []types.BrainEntry{plan},
		// No reconciler, no existing automations (so Step 2 converts it).
	}
	server := httptest.NewServer(gs.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newTestMigrateCommand(server.URL, "goals", &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "Already archived") {
		t.Errorf("output should report the plan is already archived:\n%s", output)
	}

	// The plan is converted (Step 2 POST) but never re-archived (no PATCH).
	if got := gs.postCount(); got != 1 {
		t.Errorf("expected 1 POST (plan still converted), got %d", got)
	}
	for _, p := range gs.patchRecords() {
		if strings.Contains(p.Path, "plan0001") {
			t.Errorf("already-archived plan must not be PATCHed, but saw: %+v", p)
		}
	}
}

// -----------------------------------------------------------------------------
// 7. Sanity: unknown subcommand still errors.
// -----------------------------------------------------------------------------

func TestMigrateGoals_UnknownSubcommand(t *testing.T) {
	var out bytes.Buffer
	cmd := newTestMigrateCommand("http://localhost:9999", "bogus", &MigrateFlags{}, &out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown migrate subcommand")
	}
	if !strings.Contains(err.Error(), "unknown migrate subcommand") {
		t.Errorf("unexpected error: %v", err)
	}
}
