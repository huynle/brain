package mcp_test

import "testing"

func TestGolden_GoalTools(t *testing.T) {
	g := newGolden(t, "goal_tools")
	project := g.project("golden-goals")

	g.call("create missing project", "goal_create", map[string]any{"title": "x"})
	g.call("create missing title", "goal_create", map[string]any{"project": project})
	bad := g.call("create invalid trigger", "goal_create", map[string]any{"project": project, "title": "bad", "goal_id": g.project("goal-bad"), "trigger_source": "nope"})
	g.bind("BAD_ENTRY_ID", field(t, bad, `entry_id: (\S+)`))

	created := g.call("create", "goal_create", map[string]any{
		"project": project, "title": "Ship the thing", "content": "body", "goal_id": g.project("goal-one"),
		"criteria": "tests pass", "validation": "go test", "feature_id": "golden-goal-feature",
		"trigger_source": "both", "steering_enabled": false, "steering_cooldown_minutes": 5,
		"direct_prompt": "do it", "complete_statuses": []any{"completed"},
	})
	goalID := field(t, created, `Goal created: (\S+)`)
	g.bind("GOAL_ID", goalID)
	g.bind("GOAL_ENTRY_ID", field(t, created, `entry_id: (\S+)`))

	second := g.call("create nested config", "goal_create", map[string]any{
		"project": project, "title": "Second goal", "goal_id": g.project("goal-two"),
		"config": map[string]any{"criteria": "nested", "steering": map[string]any{"enabled": true, "cooldown_minutes": float64(30)}},
		"action": map[string]any{"type": "prompt", "direct_prompt": "nested prompt"},
	})
	secondID := field(t, second, `Goal created: (\S+)`)
	g.bind("GOAL2_ID", secondID)
	g.bind("GOAL2_ENTRY_ID", field(t, second, `entry_id: (\S+)`))

	g.call("list project", "goal_list", map[string]any{"project": project})
	g.call("list feature", "goal_list", map[string]any{"project": project, "feature_id": "golden-goal-feature"})
	g.call("list empty", "goal_list", map[string]any{"project": g.project("golden-goals-empty")})

	g.call("update missing id", "goal_update", map[string]any{"title": "x"})
	g.call("update", "goal_update", map[string]any{"goal_id": goalID, "title": "Renamed goal", "feature_id": "", "criteria": ""})
	g.call("update unknown", "goal_update", map[string]any{"goal_id": "nonexistent-goal", "title": "x"})
	g.call("update invalid status", "goal_update", map[string]any{"goal_id": goalID, "status": "exploded"})

	g.call("pause", "goal_pause", map[string]any{"goal_id": goalID})
	g.call("resume", "goal_resume", map[string]any{"goal_id": goalID})
	g.call("pause unknown", "goal_pause", map[string]any{"goal_id": "nonexistent-goal"})

	run := g.call("run", "goal_run", map[string]any{"goal_id": secondID})
	if m := fieldOptional(run, `Generated task: (\S+)`); m != "" {
		g.bind("GENERATED_TASK", m)
	}
	g.call("run unknown", "goal_run", map[string]any{"goal_id": "nonexistent-goal"})
	g.call("progress", "goal_progress", map[string]any{"goal_id": secondID})
	g.call("progress unknown", "goal_progress", map[string]any{"goal_id": "nonexistent-goal"})
	g.call("audit", "goal_audit", map[string]any{"goal_id": secondID, "limit": 5})
	g.call("audit none", "goal_audit", map[string]any{"goal_id": goalID})
	g.call("audit missing id", "goal_audit", map[string]any{})

	g.call("archive", "goal_archive", map[string]any{"goal_id": goalID})
	g.call("list all", "goal_list", map[string]any{"project": project, "status": "all"})
	g.call("delete", "goal_delete", map[string]any{"goal_id": goalID})
	g.call("delete again", "goal_delete", map[string]any{"goal_id": goalID})

	dead := deadAPIMCP(t)
	g.callAt(dead, "dead api list", "goal_list", map[string]any{"project": "p", "status": "all"})
	g.callAt(dead, "dead api run", "goal_run", map[string]any{"goal_id": "g1"})
	g.callAt(dead, "dead api audit", "goal_audit", map[string]any{"goal_id": "g1"})
	g.check()
}
