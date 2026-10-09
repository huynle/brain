package commands

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
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
func newDreamMigrateCommand(apiURL, brainDir string, flags *MigrateFlags, out *bytes.Buffer) *MigrateCommand {
	cfg := testAutomationConfig(apiURL)
	cfg.Server.BrainDir = brainDir
	return &MigrateCommand{
		Subcommand: "automations",
		Config:     cfg,
		Flags:      flags,
		Out:        out,
		apiClient:  runner.NewAPIClient(cfg.Runner),
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
