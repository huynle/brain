package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/cmd/brain/assets"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// =============================================================================
// Migrate automations: dream monitors → per-project bindings
// =============================================================================

const testDreamParentID = "parent01"

// dreamPatch captures one PATCH /entries/<path> call.
type dreamPatch struct {
	Path    string
	Updates map[string]interface{}
}

// dreamAPI is an httptest fake for `brain migrate automations`. It serves the
// monitor tasks, the global Dream Consolidation parents, and the bindings per
// parent, and records every create and update.
type dreamAPI struct {
	mu sync.Mutex

	monitors []types.BrainEntry
	parents  []types.BrainEntry
	bindings map[string][]types.BrainEntry // keyed by parent ID

	// failCreateFor makes POST return 500 for a binding in this project.
	failCreateFor string

	creates    []types.CreateEntryRequest
	patches    []dreamPatch
	bindingSeq int
}

func (d *dreamAPI) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/entries":
			q := r.URL.Query()
			var entries []types.BrainEntry
			switch {
			case q.Get("type") == "task" && q.Get("tags") == "monitor":
				entries = d.monitors
			case q.Get("type") == "automation" && q.Get("global") == "true":
				entries = d.parents
			case q.Get("type") == "automation" && strings.HasPrefix(q.Get("tags"), "extends:"):
				entries = d.bindings[strings.TrimPrefix(q.Get("tags"), "extends:")]
			}
			json.NewEncoder(w).Encode(types.ListEntriesResponse{Entries: entries, Total: len(entries)})
			return

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/entries":
			var req types.CreateEntryRequest
			json.NewDecoder(r.Body).Decode(&req)
			d.creates = append(d.creates, req)
			if req.Extends != "" && req.Project == d.failCreateFor {
				http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
				return
			}
			id := "newauto" + req.Title
			if req.Extends != "" {
				d.bindingSeq++
				id = "newbind" + strconv.Itoa(d.bindingSeq)
			}
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(types.CreateEntryResponse{
				ID:   id,
				Path: "projects/" + req.Project + "/automation/x.md",
				Type: req.Type,
			})
			return

		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/entries/"):
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			d.patches = append(d.patches, dreamPatch{
				Path:    strings.TrimPrefix(r.URL.Path, "/api/v1/entries/"),
				Updates: body,
			})
			json.NewEncoder(w).Encode(types.BrainEntry{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func (d *dreamAPI) createdBindings() []types.CreateEntryRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []types.CreateEntryRequest
	for _, c := range d.creates {
		if c.Extends != "" {
			out = append(out, c)
		}
	}
	return out
}

// findDreamPatch returns the first patch to path, or nil.
func findDreamPatch(patches []dreamPatch, path string) *dreamPatch {
	for i := range patches {
		if patches[i].Path == path {
			return &patches[i]
		}
	}
	return nil
}

func (d *dreamAPI) patchList() []dreamPatch {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]dreamPatch, len(d.patches))
	copy(out, d.patches)
	return out
}

// dreamMonitor returns a dream monitor task scoped to one project, as
// service.BuildMonitorTag writes it.
func dreamMonitor(id, project string, enabled bool) types.BrainEntry {
	se := enabled
	return types.BrainEntry{
		ID:              id,
		Path:            "projects/" + project + "/task/" + id + ".md",
		Title:           "Monitor: Dream Consolidation (project " + project + ")",
		Type:            "task",
		Status:          "active",
		ProjectID:       project,
		Tags:            []string{"scheduled", "dream", "consolidation", "monitor", "monitor:dream:project:" + project},
		Schedule:        "0 4 * * *",
		ScheduleEnabled: &se,
		Timezone:        "America/Denver",
		Agent:           "dream-agent",
		Model:           "anthropic/claude-sonnet-4-20250514",
	}
}

// dreamParent returns the global Dream Consolidation automation.
func dreamParent(id string) types.BrainEntry {
	return types.BrainEntry{
		ID:     id,
		Path:   "global/automation/dream-" + id + ".md",
		Title:  "Dream Consolidation",
		Type:   "automation",
		Status: "active",
		Tags:   []string{"automation", "dream", "consolidation"},
	}
}

// newDreamMigrateCommand builds a MigrateCommand against the fake API, with a
// temporary brain dir so the file-deploy step never touches the developer's.
// Stdin is scripted and never a terminal, so no test can block on a prompt.
func newDreamMigrateCommand(apiURL, brainDir string, flags *MigrateFlags, out *bytes.Buffer) *MigrateCommand {
	cfg := testAutomationConfig(apiURL)
	cfg.Server.BrainDir = brainDir
	return &MigrateCommand{
		Subcommand:      "automations",
		Config:          cfg,
		Flags:           flags,
		Out:             out,
		apiClient:       runner.NewAPIClient(cfg.Runner),
		In:              strings.NewReader(""),
		StdinIsTerminal: func() bool { return false },
	}
}

// -----------------------------------------------------------------------------
// Increment 1: an enabled project dream monitor becomes a binding of the global
// parent that keeps its schedule, timezone, agent and model; the monitor's
// schedule is then disabled with a note that points at the binding.
// -----------------------------------------------------------------------------

func TestMigrateAutomations_DreamMonitorBecomesBinding(t *testing.T) {
	api := &dreamAPI{
		monitors: []types.BrainEntry{dreamMonitor("mon00001", "alpha", true)},
		parents:  []types.BrainEntry{dreamParent(testDreamParentID)},
	}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newDreamMigrateCommand(server.URL, t.TempDir(), &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}

	bindings := api.createdBindings()
	if len(bindings) != 1 {
		t.Fatalf("binding creates = %d, want 1\noutput:\n%s", len(bindings), out.String())
	}
	b := bindings[0]
	if b.Type != "automation" {
		t.Errorf("Type = %q, want automation", b.Type)
	}
	if b.Extends != testDreamParentID {
		t.Errorf("Extends = %q, want %q", b.Extends, testDreamParentID)
	}
	if b.Project != "alpha" {
		t.Errorf("Project = %q, want alpha", b.Project)
	}
	if b.Status != "active" {
		t.Errorf("Status = %q, want active", b.Status)
	}
	if b.Global == nil || *b.Global {
		t.Errorf("Global = %v, want explicit false (a binding belongs to one project)", b.Global)
	}
	if b.Trigger == nil || b.Trigger.Schedule != "0 4 * * *" || b.Trigger.Timezone != "America/Denver" {
		t.Errorf("Trigger = %#v, want schedule 0 4 * * * and timezone America/Denver", b.Trigger)
	}
	if b.Trigger != nil && b.Trigger.Type != "" {
		t.Errorf("Trigger.Type = %q, want empty: a binding cannot set trigger.type", b.Trigger.Type)
	}
	if b.Agent != "dream-agent" || b.Model != "anthropic/claude-sonnet-4-20250514" {
		t.Errorf("Agent/Model = %q/%q, want the task's dream-agent and model", b.Agent, b.Model)
	}

	disabled := findDreamPatch(api.patchList(), "projects/alpha/task/mon00001.md")
	if disabled == nil {
		t.Fatalf("monitor task was not patched\npatches: %#v", api.patchList())
	}
	if v, ok := disabled.Updates["schedule_enabled"].(bool); !ok || v {
		t.Errorf("schedule_enabled = %v, want false", disabled.Updates["schedule_enabled"])
	}
	note, _ := disabled.Updates["append"].(string)
	if !strings.Contains(note, "newbind1") {
		t.Errorf("append note = %q, want it to point at the binding newbind1", note)
	}
}

// -----------------------------------------------------------------------------
// Increment 2: dry run, rerun, parent checks and failed-create safety.
// Characterization: these pin behaviour that the first increment implemented.
// -----------------------------------------------------------------------------

func TestMigrateAutomations_DryRunWritesNothing(t *testing.T) {
	api := &dreamAPI{
		monitors: []types.BrainEntry{dreamMonitor("mon00001", "alpha", true)},
		parents:  []types.BrainEntry{dreamParent(testDreamParentID)},
	}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	brainDir := t.TempDir()
	var out bytes.Buffer
	cmd := newDreamMigrateCommand(server.URL, brainDir, &MigrateFlags{DryRun: true}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}

	if n := len(api.creates); n != 0 {
		t.Errorf("dry run made %d POST /entries calls, want 0", n)
	}
	if n := len(api.patchList()); n != 0 {
		t.Errorf("dry run made %d PATCH calls, want 0: %#v", n, api.patchList())
	}
	if _, err := os.Stat(filepath.Join(brainDir, "global", "automation")); !os.IsNotExist(err) {
		t.Errorf("dry run created the automation directory (stat err: %v)", err)
	}
	if !strings.Contains(out.String(), "DRY RUN: Would create binding in project alpha") {
		t.Errorf("dry run output does not describe the planned binding:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "DRY RUN: Would disable monitor") {
		t.Errorf("dry run output does not describe the planned disable:\n%s", out.String())
	}
}

func TestMigrateAutomations_RerunReusesExistingBinding(t *testing.T) {
	existing := types.BrainEntry{
		ID:        "bind0001",
		Path:      "projects/alpha/automation/bind0001.md",
		Type:      "automation",
		Status:    "active",
		ProjectID: "alpha",
		Extends:   testDreamParentID,
	}
	api := &dreamAPI{
		monitors: []types.BrainEntry{dreamMonitor("mon00001", "alpha", true)},
		parents:  []types.BrainEntry{dreamParent(testDreamParentID)},
		bindings: map[string][]types.BrainEntry{testDreamParentID: {existing}},
	}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newDreamMigrateCommand(server.URL, t.TempDir(), &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}

	if n := len(api.createdBindings()); n != 0 {
		t.Errorf("rerun created %d bindings, want 0 (project already has one)", n)
	}
	p := findDreamPatch(api.patchList(), "projects/alpha/task/mon00001.md")
	if p == nil {
		t.Fatalf("rerun did not disable the monitor; it must still be disabled\npatches: %#v", api.patchList())
	}
	if note, _ := p.Updates["append"].(string); !strings.Contains(note, "bind0001") {
		t.Errorf("note = %q, want it to point at the existing binding bind0001", note)
	}
}

func TestMigrateAutomations_RerunAfterDisableIsNoop(t *testing.T) {
	existing := types.BrainEntry{
		ID:        "bind0001",
		Path:      "projects/alpha/automation/bind0001.md",
		Type:      "automation",
		Status:    "active",
		ProjectID: "alpha",
		Extends:   testDreamParentID,
	}
	api := &dreamAPI{
		monitors: []types.BrainEntry{dreamMonitor("mon00001", "alpha", false)},
		parents:  []types.BrainEntry{dreamParent(testDreamParentID)},
		bindings: map[string][]types.BrainEntry{testDreamParentID: {existing}},
	}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newDreamMigrateCommand(server.URL, t.TempDir(), &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if n := len(api.createdBindings()); n != 0 {
		t.Errorf("second run made %d binding POSTs, want 0", n)
	}
	if n := len(api.patchList()); n != 0 {
		t.Errorf("second run made %d PATCH calls, want 0: %#v", n, api.patchList())
	}
}

func TestMigrateAutomations_AmbiguousParentAbortsBeforeAnyWrite(t *testing.T) {
	api := &dreamAPI{
		monitors: []types.BrainEntry{dreamMonitor("mon00001", "alpha", true)},
		parents:  []types.BrainEntry{dreamParent("parent01"), dreamParent("parent02")},
	}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newDreamMigrateCommand(server.URL, t.TempDir(), &MigrateFlags{}, &out)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "found 2 global") {
		t.Fatalf("Execute error = %v, want an abort naming the 2 duplicate parents", err)
	}
	if n := len(api.createdBindings()); n != 0 {
		t.Errorf("ambiguous parent still created %d bindings", n)
	}
	if n := len(api.patchList()); n != 0 {
		t.Errorf("ambiguous parent still wrote %d PATCH calls; the dream monitor must stay enabled: %#v", n, api.patchList())
	}
}

func TestMigrateAutomations_MissingParentAbortsBeforeAnyWrite(t *testing.T) {
	api := &dreamAPI{
		monitors: []types.BrainEntry{dreamMonitor("mon00001", "alpha", true)},
	}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newDreamMigrateCommand(server.URL, t.TempDir(), &MigrateFlags{}, &out)
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no global") {
		t.Fatalf("Execute error = %v, want an abort naming the missing parent", err)
	}
	if n := len(api.createdBindings()); n != 0 {
		t.Errorf("missing parent still created %d bindings", n)
	}
	if n := len(api.patchList()); n != 0 {
		t.Errorf("missing parent still wrote %d PATCH calls: %#v", n, api.patchList())
	}
}

func TestMigrateAutomations_FailedBindingLeavesMonitorEnabled(t *testing.T) {
	api := &dreamAPI{
		monitors:      []types.BrainEntry{dreamMonitor("mon00001", "alpha", true)},
		parents:       []types.BrainEntry{dreamParent(testDreamParentID)},
		failCreateFor: "alpha",
	}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	var out bytes.Buffer
	cmd := newDreamMigrateCommand(server.URL, t.TempDir(), &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if n := len(api.createdBindings()); n != 1 {
		t.Fatalf("binding attempts = %d, want 1", n)
	}
	for _, p := range api.patchList() {
		if p.Path == "projects/alpha/task/mon00001.md" {
			t.Errorf("monitor was patched (%#v) although its binding failed; dreaming for alpha would stop", p.Updates)
		}
	}
}

// -----------------------------------------------------------------------------
// Increment 3: the stagger offer. It needs the stdin and --yes fields, so the
// tests fail to build until those exist.
// -----------------------------------------------------------------------------

// dreamInstalledNoStagger is an installed dream automation file from before
// stagger existed: its trigger has no stagger line.
const dreamInstalledNoStagger = `---
type: automation
title: "Dream Consolidation"
status: active
tags:
  - automation
  - dream
trigger:
  type: cron
  schedule: "0 3 * * *"
  filter:
    project: "*"
  cooldown: 24h
  max_concurrent: 1
action:
  type: prompt
  execution_mode: current_branch
  direct_prompt: |
    Consolidate the project.
enabled: true
max_runs: 0
---

## Dream Consolidation
`

// installDreamFile writes content as the installed dream automation under brainDir.
func installDreamFile(t *testing.T, brainDir, content string) string {
	t.Helper()
	dir := filepath.Join(brainDir, "global", "automation")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "dream-consolidation.md")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write installed copy: %v", err)
	}
	return path
}

// dreamStaggerOf returns the stagger of the dream file at path.
func dreamStaggerOf(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc, err := frontmatter.Parse(string(raw))
	if err != nil {
		t.Fatalf("parse %s after the migration: %v\n%s", path, err, raw)
	}
	if doc.Frontmatter.Trigger == nil {
		t.Fatalf("trigger block lost in %s", path)
	}
	return doc.Frontmatter.Trigger.Stagger
}

// staggerCommand is a migrate command whose stdin is scripted.
func staggerCommand(apiURL, brainDir, stdin string, tty bool, flags *MigrateFlags, out *bytes.Buffer) *MigrateCommand {
	cmd := newDreamMigrateCommand(apiURL, brainDir, flags, out)
	cmd.In = strings.NewReader(stdin)
	cmd.StdinIsTerminal = func() bool { return tty }
	return cmd
}

func TestDreamStagger_YesAtTTYAppliesToFileAndLiveEntry(t *testing.T) {
	api := &dreamAPI{parents: []types.BrainEntry{dreamParent(testDreamParentID)}}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	brainDir := t.TempDir()
	path := installDreamFile(t, brainDir, dreamInstalledNoStagger)

	var out bytes.Buffer
	cmd := staggerCommand(server.URL, brainDir, "y\n", true, &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}

	if got := dreamStaggerOf(t, path); got != "2h" {
		t.Errorf("installed copy stagger = %q, want 2h", got)
	}
	parent := dreamParent(testDreamParentID)
	p := findDreamPatch(api.patchList(), parent.Path)
	if p == nil {
		t.Fatalf("live Dream Consolidation entry was not updated\npatches: %#v", api.patchList())
	}
	trig, _ := p.Updates["trigger"].(map[string]interface{})
	if trig["stagger"] != "2h" {
		t.Errorf("live trigger = %#v, want stagger 2h", p.Updates["trigger"])
	}
}

func TestDreamStagger_NoAnswerLeavesInstalledCopyAlone(t *testing.T) {
	api := &dreamAPI{parents: []types.BrainEntry{dreamParent(testDreamParentID)}}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	brainDir := t.TempDir()
	path := installDreamFile(t, brainDir, dreamInstalledNoStagger)

	var out bytes.Buffer
	cmd := staggerCommand(server.URL, brainDir, "n\n", true, &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if got := dreamStaggerOf(t, path); got != "" {
		t.Errorf("declined offer still wrote stagger %q", got)
	}
	if n := len(api.patchList()); n != 0 {
		t.Errorf("declined offer made %d PATCH calls: %#v", n, api.patchList())
	}
}

func TestDreamStagger_NoTTYWithoutYesRefuses(t *testing.T) {
	api := &dreamAPI{parents: []types.BrainEntry{dreamParent(testDreamParentID)}}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	brainDir := t.TempDir()
	path := installDreamFile(t, brainDir, dreamInstalledNoStagger)

	var out bytes.Buffer
	// Even a piped "y" must not apply: only a terminal or --yes confirms.
	cmd := staggerCommand(server.URL, brainDir, "y\n", false, &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if got := dreamStaggerOf(t, path); got != "" {
		t.Errorf("no-TTY run wrote stagger %q without --yes", got)
	}
	if n := len(api.patchList()); n != 0 {
		t.Errorf("no-TTY run made %d PATCH calls without --yes", n)
	}
	if !strings.Contains(out.String(), "--yes") {
		t.Errorf("refusal does not tell the user to pass --yes:\n%s", out.String())
	}
}

func TestDreamStagger_YesAppliesWithoutTTY(t *testing.T) {
	api := &dreamAPI{parents: []types.BrainEntry{dreamParent(testDreamParentID)}}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	brainDir := t.TempDir()
	path := installDreamFile(t, brainDir, dreamInstalledNoStagger)

	var out bytes.Buffer
	cmd := staggerCommand(server.URL, brainDir, "", false, &MigrateFlags{Yes: true}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if got := dreamStaggerOf(t, path); got != "2h" {
		t.Errorf("--yes without a TTY: stagger = %q, want 2h", got)
	}
}

func TestDreamStagger_DryRunWritesNothingEvenWithYes(t *testing.T) {
	api := &dreamAPI{parents: []types.BrainEntry{dreamParent(testDreamParentID)}}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	brainDir := t.TempDir()
	path := installDreamFile(t, brainDir, dreamInstalledNoStagger)

	var out bytes.Buffer
	cmd := staggerCommand(server.URL, brainDir, "", true, &MigrateFlags{DryRun: true, Yes: true}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if got := dreamStaggerOf(t, path); got != "" {
		t.Errorf("dry run wrote stagger %q", got)
	}
	if n := len(api.patchList()); n != 0 {
		t.Errorf("dry run made %d PATCH calls", n)
	}
}

func TestDreamStagger_AlreadyStaggeredIsNotOffered(t *testing.T) {
	api := &dreamAPI{parents: []types.BrainEntry{dreamParent(testDreamParentID)}}
	server := httptest.NewServer(api.handler())
	defer server.Close()

	brainDir := t.TempDir()
	staggered := strings.Replace(dreamInstalledNoStagger, "  cooldown: 24h", "  stagger: 1h\n  cooldown: 24h", 1)
	path := installDreamFile(t, brainDir, staggered)

	var out bytes.Buffer
	cmd := staggerCommand(server.URL, brainDir, "y\n", true, &MigrateFlags{}, &out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v\noutput:\n%s", err, out.String())
	}
	if got := dreamStaggerOf(t, path); got != "1h" {
		t.Errorf("existing stagger changed to %q, want 1h untouched", got)
	}
	if strings.Contains(out.String(), "Apply stagger") {
		t.Errorf("already-staggered copy was offered again:\n%s", out.String())
	}
}

// The shipped template ships with stagger, so a fresh install is staggered.
func TestDreamAsset_ShipsStagger(t *testing.T) {
	raw, err := assets.GetAutomation("dream-consolidation.md")
	if err != nil {
		t.Fatalf("GetAutomation: %v", err)
	}
	doc, err := frontmatter.Parse(string(raw))
	if err != nil {
		t.Fatalf("parse shipped asset: %v", err)
	}
	if doc.Frontmatter.Trigger == nil || doc.Frontmatter.Trigger.Stagger != "2h" {
		t.Fatalf("shipped dream trigger stagger = %v, want 2h", doc.Frontmatter.Trigger)
	}
}
