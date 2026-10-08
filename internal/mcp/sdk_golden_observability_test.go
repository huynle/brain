package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

// seedTask creates a task through the real REST API and returns its id.
func seedTask(t *testing.T, apiURL, project, title string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"type": "task", "title": title, "content": "seed", "project": project, "status": "draft"})
	resp, err := http.Post(apiURL+"/api/v1/entries", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.ID == "" {
		t.Fatalf("seed task: status=%d err=%v", resp.StatusCode, err)
	}
	return out.ID
}

func sortMetadataBlocks(s string) string {
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		if lines[i] != "- Metadata:" {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.HasPrefix(lines[j], "  - ") {
			j++
		}
		sort.Strings(lines[i+1 : j])
		i = j - 1
	}
	return strings.Join(lines, "\n")
}

func TestGolden_ObservabilityTools(t *testing.T) {
	g := newGolden(t, "observability_tools")
	project := g.project("golden-obs")
	taskID := seedTask(t, realAPI(t), project, "Observed task")
	g.bind("TASK_ID", taskID)

	g.call("logs", "task_logs", map[string]any{"project": project, "task_id": taskID, "limit": 10})
	g.call("logs missing task", "task_logs", map[string]any{"project": project})
	g.call("logs unknown task", "task_logs", map[string]any{"project": project, "task_id": "zzzzzzzz"})
	g.call("dispatch lease none", "task_dispatch_lease", map[string]any{"project": project, "task_id": taskID})
	g.call("dispatch lease missing task", "task_dispatch_lease", map[string]any{"project": project})
	g.call("placement reasons", "task_placement_reasons", map[string]any{"project": project, "task_id": taskID})
	g.scrubRe(`Searched \d+ buffered`, "Searched <N> buffered")
	g.call("events invalid type", "events_recent", map[string]any{"type": "automation.run"})
	g.call("events invalid source", "events_recent", map[string]any{"project_id": project, "source": "martians", "limit": 5})
	g.scrubRe(`evt_[0-9a-f]+`, "<EVENT_ID>")
	// formatRecentEvents ranges over the metadata map (random order, before
	// and after the SDK move); compare the set, not the order.
	g.postProcess(sortMetadataBlocks)
	g.call("events for seeded project", "events_recent", map[string]any{"project_id": project, "type": "*", "limit": 5})
	g.call("automation runs none", "automation_runs", map[string]any{"project": project, "status": "queued", "limit": 5})
	g.call("automation run unknown", "automation_run_get", map[string]any{"run_id": "nonexistent"})
	g.call("automation run missing arg", "automation_run_get", map[string]any{})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead api logs", "task_logs", map[string]any{"project": "p", "task_id": "t1", "limit": 3})
	g.callAt(dead, "dead api lease", "task_dispatch_lease", map[string]any{"project": "p", "task_id": "t1"})
	g.callAt(dead, "dead api placement", "task_placement_reasons", map[string]any{"project": "p", "task_id": "t1"})
	g.callAt(dead, "dead api events", "events_recent", map[string]any{"project_id": "p", "type": "task.*", "limit": 4})
	g.callAt(dead, "dead api runs", "automation_runs", map[string]any{"project": "p", "automation_id": "a1"})
	g.callAt(dead, "dead api run get", "automation_run_get", map[string]any{"run_id": "r1"})
	g.callAt(dead, "dead api scheduler", "scheduler_status", map[string]any{})
	g.check()
}
