package mcp_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestGolden_SchedulerTools records scheduler_status and a non-empty
// task_placement_reasons on a dedicated server (scheduler state is
// server-wide): one ready task and no runner, so the real scheduler records a
// no_candidate rejection on its own tick.
func TestGolden_SchedulerTools(t *testing.T) {
	api := dedicatedAPI(t)
	g := newGoldenAt(t, "scheduler_tools", api)
	g.scrubRe(`- Total ticks: \d+`, "- Total ticks: <N>")
	g.scrubRe(`- Created at: \d+`, "- Created at: <UNIX>")
	// A scheduler tick (5s) can land between reads and add an identical
	// rejection row; pin the row's content, not how many ticks wrote it.
	g.scrubRe(`Rejections recorded: [1-9]\d*`, "Rejections recorded: <N>")
	g.postProcess(collapseRepeatedDecisions)

	body, _ := json.Marshal(map[string]any{"type": "task", "title": "Unplaceable", "content": "x", "project": "sched", "status": "pending"})
	resp, err := http.Post(api+"/api/v1/entries", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.ID == "" {
		t.Fatal("seed task failed")
	}
	g.bind("TASK_ID", created.ID)

	// Wait (bounded) for the scheduler to tick past the task at least twice:
	// a rejection row exists and the last tick considered the project.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var reasons struct {
			Total int `json:"total"`
		}
		var status struct {
			Results map[string]json.RawMessage `json:"last_project_results"`
		}
		getJSON(t, api+"/api/v1/tasks/sched/"+created.ID+"/placement-reasons", &reasons)
		getJSON(t, api+"/api/v1/scheduler/status", &status)
		if reasons.Total > 0 && status.Results["sched"] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduler never rejected the seeded task (reasons=%d)", reasons.Total)
		}
		time.Sleep(100 * time.Millisecond)
	}
	g.call("placement reasons", "task_placement_reasons", map[string]any{"project": "sched", "task_id": created.ID})
	g.call("placement reasons unknown task", "task_placement_reasons", map[string]any{"project": "sched", "task_id": "zzzzzzzz"})
	g.call("placement reasons missing task", "task_placement_reasons", map[string]any{"project": "sched"})
	g.call("dispatch lease none", "task_dispatch_lease", map[string]any{"project": "sched", "task_id": created.ID})
	g.call("scheduler status", "scheduler_status", nil)
	g.check()
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_ = json.NewDecoder(resp.Body).Decode(out)
}

var decisionBlockRe = regexp.MustCompile(`### Decision\n(?:- .*\n)+\n`)

// collapseRepeatedDecisions folds identical consecutive "### Decision" blocks.
func collapseRepeatedDecisions(s string) string {
	for _, b := range decisionBlockRe.FindAllString(s, -1) {
		for strings.Contains(s, b+b) {
			s = strings.Replace(s, b+b, b, 1)
		}
	}
	return s
}
