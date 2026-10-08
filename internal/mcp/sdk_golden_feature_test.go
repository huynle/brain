package mcp_test

import (
	"regexp"
	"testing"
	"time"
)

// TestGolden_FeatureAndAssignmentTools covers feature_tools.go and
// task_assignment_tools.go on a dedicated server: runner registration and
// assignments are server-wide state.
func TestGolden_FeatureAndAssignmentTools(t *testing.T) {
	api := dedicatedAPI(t)
	g := newGoldenAt(t, "feature_assignment_tools", api)
	g.scrubRe(`- Assigned at: .*`, "- Assigned at: <TIME>")
	g.scrubRe(`- Updated at: .*`, "- Updated at: <TIME>")
	p := "feat-proj"

	g.call("features empty", "features", map[string]any{"project": p})
	g.call("feature ready empty", "feature_ready", map[string]any{"project": p})

	one := entryFields(t, api, map[string]any{"type": "task", "title": "Build API", "content": "one", "project": p, "status": "pending", "priority": "high", "feature_id": "feat-x", "git_branch": "feat-x", "merge_target_branch": "main", "executor": "opencode"})
	g.bind("ONE", one)
	two := entryFields(t, api, map[string]any{"type": "task", "title": "Wire UI", "content": "two", "project": p, "status": "pending", "feature_id": "feat-x", "depends_on": []string{one}, "git_branch": "feat-x", "merge_target_branch": "develop"})
	g.bind("TWO", two)
	g.bind("THREE", entryFields(t, api, map[string]any{"type": "task", "title": "Docs", "content": "three", "project": p, "status": "completed", "feature_id": "feat-y"}))
	g.bind("FOUR", entryFields(t, api, map[string]any{"type": "task", "title": "Later", "content": "four", "project": p, "status": "pending", "feature_id": "feat-z", "feature_depends_on": []string{"feat-x"}}))
	solo := entryFields(t, api, map[string]any{"type": "task", "title": "Standalone", "content": "solo", "project": p, "status": "pending", "executor": "pi", "requires_capability": []string{"gpu"}})
	g.bind("SOLO", solo)

	apiDo(t, "POST", api+"/api/v1/runners/register", map[string]any{
		"runner_id": "runner-gpu", "hostname": "host-a", "projects": []string{p},
		"executors": []string{"opencode", "pi"}, "capabilities": []string{"gpu"}, "max_parallel": 2,
	})
	time.Sleep(5 * time.Millisecond)
	apiDo(t, "POST", api+"/api/v1/runners/register", map[string]any{
		"runner_id": "runner-plain", "hostname": "host-b", "executors": []string{"opencode"}, "max_parallel": 1,
	})

	g.call("features", "features", map[string]any{"project": p})
	g.call("features limit 1", "features", map[string]any{"project": p, "limit": 1})
	g.call("features ready only", "features", map[string]any{"project": p, "ready_only": true})
	g.call("feature ready", "feature_ready", map[string]any{"project": p, "limit": 5})
	g.call("feature get", "feature_get", map[string]any{"project": p, "feature_id": "feat-x"})
	g.call("feature get done", "feature_get", map[string]any{"project": p, "feature_id": "feat-y"})
	g.call("feature get unknown", "feature_get", map[string]any{"project": p, "feature_id": "feat-none"})
	g.call("feature get missing id", "feature_get", map[string]any{"project": p})

	g.call("feature candidates", "feature_runner_candidates", map[string]any{"project": p, "feature_id": "feat-x"})
	g.call("feature candidates rejected", "feature_runner_candidates", map[string]any{"project": p, "feature_id": "feat-x", "include_rejected": true})
	g.call("feature candidates unknown", "feature_runner_candidates", map[string]any{"project": p, "feature_id": "feat-none"})
	g.call("feature candidates missing id", "feature_runner_candidates", map[string]any{"project": p})

	g.call("feature assign missing runner", "feature_assign", map[string]any{"project": p, "feature_id": "feat-x"})
	g.call("feature assign missing feature", "feature_assign", map[string]any{"project": p, "runner_id": "runner-gpu"})
	g.call("feature assign unknown runner", "feature_assign", map[string]any{"project": p, "feature_id": "feat-x", "runner_id": "nope"})
	g.call("feature assign", "feature_assign", map[string]any{"project": p, "feature_id": "feat-x", "runner_id": "runner-plain", "force": true})
	g.call("feature assign again", "feature_assign", map[string]any{"project": p, "feature_id": "feat-x", "runner_id": "runner-gpu", "force": true})
	g.call("feature reassign", "feature_assign", map[string]any{"project": p, "feature_id": "feat-x", "runner_id": "runner-gpu", "intent": "reassign", "force": true})
	g.call("feature clear", "feature_clear_assignment", map[string]any{"project": p, "feature_id": "feat-x", "intent": "done here"})
	g.call("feature clear default intent", "feature_clear_assignment", map[string]any{"project": p, "feature_id": "feat-x"})
	g.call("feature clear missing id", "feature_clear_assignment", map[string]any{"project": p})

	g.call("checkout missing id", "feature_checkout", map[string]any{"project": p})
	g.bindCheckoutTask("CHECKOUT_CHECKOUT_BAD_POLICY", g.call("checkout bad policy", "feature_checkout", map[string]any{"project": p, "feature_id": "feat-x", "merge_policy": "yolo"}))
	g.bindCheckoutTask("CHECKOUT_CHECKOUT", g.call("checkout", "feature_checkout", map[string]any{"project": p, "feature_id": "feat-x", "execution_branch": "feat-x", "merge_target_branch": "main", "merge_policy": "prompt_only", "merge_strategy": "squash", "remote_branch_policy": "keep", "open_pr_before_merge": true, "execution_mode": "worktree", "checkout_mode": "simple"}))
	g.bindCheckoutTask("CHECKOUT_CHECKOUT_AGAIN", g.call("checkout again", "feature_checkout", map[string]any{"project": p, "feature_id": "feat-x", "merge_policy": "prompt_only"}))
	g.bindCheckoutTask("CHECKOUT_CHECKOUT_UNKNOWN_FEATURE", g.call("checkout unknown feature", "feature_checkout", map[string]any{"project": p, "feature_id": "feat-none"}))

	g.call("candidates task", "runner_candidates", map[string]any{"project": p, "task_id": solo})
	g.call("candidates task rejected", "runner_candidates", map[string]any{"project": p, "task_id": solo, "include_rejected": true})
	g.call("candidates unknown task", "runner_candidates", map[string]any{"project": p, "task_id": "zzzzzzzz"})
	g.call("candidates proposed", "runner_candidates", map[string]any{"project": p, "executor": "pi", "requires_capability": []string{"gpu"}, "include_rejected": true})
	g.call("candidates proposed feature", "runner_candidates", map[string]any{"project": p, "feature_id": "feat-x", "execution_mode": "worktree", "machine_affinity": "none"})
	g.call("candidates proposed invalid", "runner_candidates", map[string]any{"project": p, "machine_affinity": "everywhere"})
	g.call("candidates proposed remote", "runner_candidates", map[string]any{"project": p, "git_remote": "git@github.com:o/r.git", "include_rejected": true})

	g.call("assign missing", "task_assign", map[string]any{"project": p, "task_id": solo})
	g.call("assign incompatible", "task_assign", map[string]any{"project": p, "task_id": solo, "runner_id": "runner-plain"})
	g.call("assign", "task_assign", map[string]any{"project": p, "task_id": solo, "runner_id": "runner-gpu", "force": true})
	g.call("assign again", "task_assign", map[string]any{"project": p, "task_id": solo, "runner_id": "runner-gpu", "force": true})
	g.call("reassign", "task_assign", map[string]any{"project": p, "task_id": solo, "runner_id": "runner-gpu", "intent": "reassign", "force": true})
	g.call("assign feature task", "task_assign", map[string]any{"project": p, "task_id": one, "runner_id": "runner-gpu", "force": true})
	g.call("assign unknown task", "task_assign", map[string]any{"project": p, "task_id": "zzzzzzzz", "runner_id": "runner-gpu"})
	g.call("clear", "task_clear_assignment", map[string]any{"project": p, "task_id": solo})
	g.call("clear again", "task_clear_assignment", map[string]any{"project": p, "task_id": solo})
	g.call("clear missing", "task_clear_assignment", map[string]any{"project": p})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead features", "features", map[string]any{"project": "p q"})
	g.callAt(dead, "dead features ready only", "features", map[string]any{"project": "p", "ready_only": true})
	g.callAt(dead, "dead feature ready", "feature_ready", map[string]any{"project": "p"})
	g.callAt(dead, "dead feature get", "feature_get", map[string]any{"project": "p", "feature_id": "f/1"})
	g.callAt(dead, "dead feature candidates", "feature_runner_candidates", map[string]any{"project": "p", "feature_id": "f"})
	g.callAt(dead, "dead feature assign", "feature_assign", map[string]any{"project": "p", "feature_id": "f", "runner_id": "r"})
	g.callAt(dead, "dead feature clear", "feature_clear_assignment", map[string]any{"project": "p", "feature_id": "f"})
	g.callAt(dead, "dead checkout", "feature_checkout", map[string]any{"project": "p", "feature_id": "f"})
	g.callAt(dead, "dead candidates task", "runner_candidates", map[string]any{"project": "p", "task_id": "t 1"})
	g.callAt(dead, "dead candidates proposed", "runner_candidates", map[string]any{"project": "p"})
	g.callAt(dead, "dead assign", "task_assign", map[string]any{"project": "p", "task_id": "t", "runner_id": "r"})
	g.callAt(dead, "dead clear", "task_clear_assignment", map[string]any{"project": "p", "task_id": "t"})
	g.check()
}

var checkoutTaskIDRe = regexp.MustCompile(`(?m)^- ID: (\S+)$`)

// bindCheckoutTask binds the generated checkout task id named in a call's
// output to a stable placeholder.
func (g *golden) bindCheckoutTask(placeholder, text string) {
	if m := checkoutTaskIDRe.FindStringSubmatch(text); m != nil {
		g.bind(placeholder, m[1])
	}
}
