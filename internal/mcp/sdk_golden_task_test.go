package mcp_test

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestGolden_TaskTools covers task_tools.go: list/next/get/metadata/status,
// trigger, the monitor templates (generic and legacy aliases) and the
// relaunch path of resume_task_with_context. The live-inject path needs a
// runner bridge and is in the supervisor golden (dedicated server).
func TestGolden_TaskTools(t *testing.T) {
	api := dedicatedAPI(t)
	g := newGoldenAt(t, "task_tools", api)
	g.postProcess(canonicalCycles)
	g.postProcess(sortSectionBullets)
	p := g.project("tasks")
	idle := g.project("idle")
	empty := g.project("empty")

	alpha := entryFields(t, api, map[string]any{"type": "task", "title": "Alpha root", "content": "Alpha body\n\nSecond paragraph.", "project": p, "status": "pending", "priority": "high", "feature_id": "feat-a", "user_original_request": "do alpha\nplease", "agent": "tdd-dev", "model": "m/one", "git_branch": "feat-a", "merge_target_branch": "main", "merge_policy": "prompt_only"})
	g.bind("ALPHA", alpha)
	beta := entryFields(t, api, map[string]any{"type": "task", "title": "Beta child", "content": "Beta body", "project": p, "status": "pending", "priority": "medium", "feature_id": "feat-a", "depends_on": []string{alpha}})
	g.bind("BETA", beta)
	gamma := entryFields(t, api, map[string]any{"type": "task", "title": "Gamma blocked", "content": "Gamma body", "project": p, "status": "blocked", "priority": "low"})
	g.bind("GAMMA", gamma)
	delta := entryFields(t, api, map[string]any{"type": "task", "title": "Delta done", "content": "Delta body", "project": p, "status": "completed", "priority": "medium", "tags": []string{"x", "y"}})
	g.bind("DELTA", delta)
	zeta := entryFields(t, api, map[string]any{"type": "task", "title": "Zeta loop one", "content": "z1", "project": p, "status": "pending", "depends_on": []string{"Zeta loop two"}})
	g.bind("ZETA1", zeta)
	zeta2 := entryFields(t, api, map[string]any{"type": "task", "title": "Zeta loop two", "content": "z2", "project": p, "status": "pending", "depends_on": []string{zeta}})
	g.bind("ZETA2", zeta2)
	g.bind("IDLE_BLOCKED", entryFields(t, api, map[string]any{"type": "task", "title": "Idle blocked", "content": "b", "project": idle, "status": "blocked"}))
	g.bind("IDLE_DONE", entryFields(t, api, map[string]any{"type": "task", "title": "Idle done", "content": "d", "project": idle, "status": "completed"}))

	g.call("tasks all", "tasks", map[string]any{"project": p})
	g.call("tasks limit 2", "tasks", map[string]any{"project": p, "limit": 2})
	g.call("tasks status pending", "tasks", map[string]any{"project": p, "status": "pending"})
	g.call("tasks classification waiting", "tasks", map[string]any{"project": p, "classification": "waiting"})
	g.call("tasks classification blocked", "tasks", map[string]any{"project": p, "classification": "blocked"})
	g.call("tasks not pending", "tasks", map[string]any{"project": p, "classification": "not_pending"})
	g.call("tasks feature", "tasks", map[string]any{"project": p, "feature_id": "feat-a"})
	g.call("tasks no match", "tasks", map[string]any{"project": p, "feature_id": "nope"})
	g.call("tasks empty project", "tasks", map[string]any{"project": empty})

	g.call("next", "task_next", map[string]any{"project": p})
	g.call("next empty project", "task_next", map[string]any{"project": empty})
	g.call("next nothing ready", "task_next", map[string]any{"project": idle})

	g.call("get by id", "task_get", map[string]any{"project": p, "task_id": alpha})
	g.call("get by title", "task_get", map[string]any{"project": p, "taskId": "beta child"})
	g.call("get blocked", "task_get", map[string]any{"project": p, "task_id": gamma})
	g.call("get partial", "task_get", map[string]any{"project": p, "task_id": "zeta"})
	g.call("get unknown", "task_get", map[string]any{"project": p, "task_id": "nothing-like-it"})
	g.call("get missing id", "task_get", map[string]any{"project": p})

	g.call("metadata", "task_metadata", map[string]any{"project": p, "task_id": alpha})
	g.call("metadata dependent", "task_metadata", map[string]any{"project": p, "task_id": "Beta child"})
	g.call("metadata partial", "task_metadata", map[string]any{"project": p, "task_id": "loop"})
	g.call("metadata unknown", "task_metadata", map[string]any{"project": p, "task_id": "zzzzzzzz"})
	g.call("metadata missing id", "task_metadata", map[string]any{"project": p})

	g.call("status mixed", "tasks_status", map[string]any{"project": p, "task_ids": []string{alpha, delta, "zzzzzzzz"}})
	g.call("status completed already", "tasks_status", map[string]any{"project": p, "task_ids": []string{delta}, "wait_for": "completed", "timeout": 1000})
	g.call("status wait not found", "tasks_status", map[string]any{"project": p, "taskIds": []string{"zzzzzzzz"}, "waitFor": "any"})
	g.call("status missing ids", "tasks_status", map[string]any{"project": p})

	g.call("trigger unscheduled", "task_trigger", map[string]any{"project": p, "task_id": delta})
	g.call("trigger unknown", "task_trigger", map[string]any{"project": p, "task_id": "zzzzzzzz"})

	g.bindTaskID("MONITOR_ENABLE", g.call("monitor enable", "monitor_enable", map[string]any{"template_id": "blocked-inspector", "project": p, "feature_id": "feat-a", "schedule": "*/45 * * * *"}))
	g.call("monitor enable again", "monitor_enable", map[string]any{"template_id": "blocked-inspector", "project": p, "feature_id": "feat-a"})
	g.call("monitor enable unknown template", "monitor_enable", map[string]any{"template_id": "no-such-template", "project": p, "feature_id": "feat-a"})
	g.call("monitor enable missing args", "monitor_enable", map[string]any{"project": p})
	g.call("monitor disable", "monitor_disable", map[string]any{"template_id": "blocked-inspector", "project": p, "feature_id": "feat-a"})
	g.call("monitor disable again", "monitor_disable", map[string]any{"template_id": "blocked-inspector", "project": p, "feature_id": "feat-a"})
	g.call("monitor disable missing template", "monitor_disable", map[string]any{"project": p, "feature_id": "feat-a"})

	g.bindTaskID("FEATURE_REVIEW_ENABLE", g.call("feature review enable", "feature_review_enable", map[string]any{"project": p, "feature_id": "feat-a"}))
	g.call("feature review enable again", "feature_review_enable", map[string]any{"project": p, "feature_id": "feat-a"})
	g.call("feature review disable", "feature_review_disable", map[string]any{"project": p, "feature_id": "feat-a"})
	g.call("feature review disable again", "feature_review_disable", map[string]any{"project": p, "feature_id": "feat-a"})
	g.bindTaskID("FEATURE_REVIEW_ENABLE_EMPTY_FEATURE", g.call("feature review enable empty feature", "feature_review_enable", map[string]any{"project": p, "feature_id": "feat-none"}))

	g.bindTaskID("BLOCKED_INSPECTOR_ENABLE", g.call("blocked inspector enable", "blocked_inspector_enable", map[string]any{"project": p, "feature_id": "feat-b"}))
	g.call("blocked inspector enable again", "blocked_inspector_enable", map[string]any{"project": p, "feature_id": "feat-b", "schedule": "0 * * * *"})
	g.call("blocked inspector disable", "blocked_inspector_disable", map[string]any{"project": p, "feature_id": "feat-b"})
	g.call("blocked inspector disable again", "blocked_inspector_disable", map[string]any{"project": p, "feature_id": "feat-b"})

	g.bindTaskID("DREAM_ENABLE", g.call("dream enable", "dream_enable", map[string]any{"project": p, "schedule": "0 4 * * *"}))
	g.call("dream enable again", "dream_enable", map[string]any{"project": p})
	g.call("dream disable", "dream_disable", map[string]any{"project": p})
	g.call("dream disable again", "dream_disable", map[string]any{"project": p})

	g.call("resume missing task", "resume_task_with_context", map[string]any{"project": p, "injected_context": "ctx"})
	g.call("resume missing context", "resume_task_with_context", map[string]any{"project": p, "task_id": gamma})
	g.call("resume not abandoned", "resume_task_with_context", map[string]any{"project": p, "task_id": gamma, "injected_context": "look again"})
	g.call("resume forced", "resume_task_with_context", map[string]any{"project": p, "task_id": gamma, "injected_context": "look again", "force": true, "prefer_same_session": false, "executor_override": "pi"})
	g.call("resume forced again", "resume_task_with_context", map[string]any{"project": p, "task_id": gamma, "injected_context": "look again", "force": true})
	g.call("resume unknown task", "resume_task_with_context", map[string]any{"project": p, "task_id": "zzzzzzzz", "injected_context": "x", "force": true})
	g.call("tasks after resume", "tasks", map[string]any{"project": p, "status": "pending"})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead tasks", "tasks", map[string]any{"project": "p q"})
	g.callAt(dead, "dead next", "task_next", map[string]any{"project": "p"})
	g.callAt(dead, "dead get", "task_get", map[string]any{"project": "p", "task_id": "t"})
	g.callAt(dead, "dead metadata", "task_metadata", map[string]any{"project": "p", "task_id": "t"})
	g.callAt(dead, "dead status", "tasks_status", map[string]any{"project": "p", "task_ids": []string{"t"}})
	g.callAt(dead, "dead trigger", "task_trigger", map[string]any{"project": "p", "task_id": "t/1"})
	g.callAt(dead, "dead monitor enable", "monitor_enable", map[string]any{"template_id": "x", "project": "p", "feature_id": "f"})
	g.callAt(dead, "dead monitor disable", "monitor_disable", map[string]any{"template_id": "x", "project": "p", "feature_id": "f"})
	g.callAt(dead, "dead feature review enable", "feature_review_enable", map[string]any{"project": "p", "feature_id": "f"})
	g.callAt(dead, "dead feature review disable", "feature_review_disable", map[string]any{"project": "p", "feature_id": "f"})
	g.callAt(dead, "dead blocked inspector enable", "blocked_inspector_enable", map[string]any{"project": "p", "feature_id": "f"})
	g.callAt(dead, "dead blocked inspector disable", "blocked_inspector_disable", map[string]any{"project": "p", "feature_id": "f"})
	g.callAt(dead, "dead dream enable", "dream_enable", map[string]any{"project": "p"})
	g.callAt(dead, "dead dream disable", "dream_disable", map[string]any{"project": "p"})
	g.callAt(dead, "dead resume", "resume_task_with_context", map[string]any{"project": "p", "task_id": "t", "injected_context": "c"})
	g.check()
}

var monitorTaskIDRe = regexp.MustCompile(`\*\*Task ID:\*\* (\S+)`)

// bindTaskID records a call whose output names a generated task id and binds
// that id to a stable placeholder.
func (g *golden) bindTaskID(placeholder string, text string) {
	if m := monitorTaskIDRe.FindStringSubmatch(text); m != nil {
		g.bind(placeholder, m[1])
	}
}

var cycleLineRe = regexp.MustCompile(`(?m)^- Cycle: (.+)$`)

// canonicalCycles rotates each reported dependency cycle to start at its
// smallest member: the server finds cycles by map iteration, so the starting
// task varies between identical calls.
func canonicalCycles(s string) string {
	return cycleLineRe.ReplaceAllStringFunc(s, func(line string) string {
		members := strings.Split(strings.TrimPrefix(line, "- Cycle: "), " -> ")
		start := 0
		for i, m := range members {
			if m < members[start] {
				start = i
			}
		}
		return "- Cycle: " + strings.Join(append(members[start:], members[:start]...), " -> ")
	})
}

// sortSectionBullets orders the bullets (with their indented continuation
// lines) inside every "### " section. The task list is served in index order,
// which ties on equal timestamps; the client renders whatever order arrives,
// so the order itself is not what these transcripts pin.
func sortSectionBullets(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for i := 0; i < len(lines); {
		if !strings.HasPrefix(lines[i], "### ") {
			out = append(out, lines[i])
			i++
			continue
		}
		out = append(out, lines[i])
		i++
		var items []string
		for i < len(lines) && strings.HasPrefix(lines[i], "- ") {
			item := lines[i]
			i++
			for i < len(lines) && strings.HasPrefix(lines[i], "  ") {
				item += "\n" + lines[i]
				i++
			}
			items = append(items, item)
		}
		sort.Strings(items)
		out = append(out, items...)
	}
	return strings.Join(out, "\n")
}
