package runner

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

type resourceHealthState struct {
	mu       sync.Mutex
	warnings map[string]bool
}

// Pressure starts at 80% and clears below 70%, without changing kill policy.
func pressureState(previous bool, rss, limit int64) bool {
	if limit <= 0 {
		return false
	}
	threshold := 0.8
	if previous {
		threshold = 0.7
	}
	return float64(rss)/float64(limit) >= threshold
}
func (tr *TaskRunner) publishResourceSample(info ProcessInfo, table map[int]procSample, tree []int, limit int64, unavailable string) {
	task := info.Task
	sample := &types.ResourceSample{InstanceID: task.InstanceID, SessionID: task.SessionID, Executor: task.ExecutorType, SampledAt: time.Now().UTC(), LimitBytes: limit, UnavailableReason: unavailable}
	if !task.LastActivity.IsZero() {
		at := task.LastActivity
		sample.LastActivity = &at
	}
	if unavailable == "" {
		rss := treeRSS(table, tree)
		count := len(tree)
		sample.RSSBytes = &rss
		sample.ProcessCount = &count
		names := map[string]bool{}
		for _, pid := range tree {
			if command := table[pid].Command; command != "" {
				names[command] = true
			}
		}
		commands := []string{}
		for command := range names {
			commands = append(commands, command)
		}
		sort.Strings(commands)
		if len(commands) > 8 {
			commands = commands[:8]
		}
		if len(commands) > 0 {
			summary := strings.Join(commands, ", ")
			sample.CommandSummary = &summary
		}
		if rss > limit {
			sample.TerminationReason = "memory_limit_exceeded"
		}
	}
	key := task.ProjectID + ":" + task.ID + ":" + task.InstanceID
	tr.resourceHealth.mu.Lock()
	if tr.resourceHealth.warnings == nil {
		tr.resourceHealth.warnings = map[string]bool{}
	}
	previous := tr.resourceHealth.warnings[key]
	warning := previous
	if sample.RSSBytes != nil {
		warning = pressureState(previous, *sample.RSSBytes, limit)
	}
	if warning {
		tr.resourceHealth.warnings[key] = true
	} else {
		delete(tr.resourceHealth.warnings, key)
	}
	tr.resourceHealth.mu.Unlock()
	sample.Warning = warning
	event := RunnerEvent{Type: EventTaskResourceSample, TaskID: task.ID, ProjectID: task.ProjectID, FeatureID: task.FeatureID, SessionID: task.SessionID, Resource: sample}
	tr.emitEvent(event)
	if warning != previous {
		event.Type = EventTaskResourcePressure
		tr.emitEvent(event)
	}
}
