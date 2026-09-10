package runner

import (
	"encoding/json"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestResourcePressureHysteresisAndUnavailable(t *testing.T) {
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	events := []RunnerEvent{}
	tr.OnEvent(func(e RunnerEvent) { events = append(events, e) })
	info := ProcessInfo{Task: RunningTask{ID: "t", ProjectID: "p", InstanceID: "i", ExecutorType: "pi"}}
	for _, rss := range []int64{79, 81, 85, 75, 69} {
		tr.publishResourceSample(info, map[int]procSample{1: {PID: 1, RSSBytes: rss}}, []int{1}, 100, "")
	}
	warnings := 0
	for _, e := range events {
		if e.Type == EventTaskResourcePressure {
			warnings++
		}
	}
	if warnings != 2 {
		t.Fatalf("expected warning and recovery only, got %d", warnings)
	}
	tr.publishResourceSample(info, nil, nil, 100, "sampling_failed")
	e := events[len(events)-1].ToEvent()
	var sample types.ResourceSample
	if err := json.Unmarshal([]byte(e.Metadata["resource"]), &sample); err != nil {
		t.Fatal(err)
	}
	if sample.RSSBytes != nil || sample.ProcessCount != nil || sample.LastActivity != nil || sample.CommandSummary != nil || sample.Executor != "pi" {
		t.Fatal(sample)
	}
	if e.ProjectID != "p" || e.TaskID != "t" {
		t.Fatal(e)
	}
}

func TestResourceCommandsExcludeArgumentsAndPaths(t *testing.T) {
	table, err := parseProcessTable([]byte("12 1 100 /usr/local/bin/node\n13 12 200 /bin/sh\n"))
	if err != nil {
		t.Fatal(err)
	}
	tr := newTestRunner(newMockClient(), newMockExecutor(), newMockProcessMgr(), newMockStateMgr())
	var last RunnerEvent
	tr.OnEvent(func(e RunnerEvent) { last = e })
	tr.publishResourceSample(ProcessInfo{Task: RunningTask{ID: "t"}}, table, []int{12, 13}, 1<<20, "")
	if last.Resource.CommandSummary == nil || *last.Resource.CommandSummary != "node, sh" {
		t.Fatal(last.Resource)
	}
}
