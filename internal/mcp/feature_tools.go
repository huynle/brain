package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterFeatureTools registers feature orchestration MCP tools on the server.
func RegisterFeatureTools(s *Server, client *APIClient) {
	registerBrainFeatures(s, client)
	registerBrainFeatureReady(s, client)
	registerBrainFeatureGet(s, client)
	registerBrainFeatureCheckout(s, client)
	registerBrainFeatureAssign(s, client)
	registerBrainFeatureClearAssignment(s, client)
}

func featureCommonProperties() map[string]Property {
	return map[string]Property{
		"project":    {Type: "string", Description: "Override auto-detected project"},
		"feature_id": {Type: "string", Description: "Feature ID to inspect or modify"},
	}
}

func registerBrainFeatures(s *Server, client *APIClient) {
	props := map[string]Property{
		"project":    {Type: "string", Description: "Override auto-detected project"},
		"ready_only": {Type: "boolean", Description: "List only features whose dependencies are ready"},
		"limit":      {Type: "number", Description: "Maximum features to include in the summary"},
	}
	s.RegisterTool(Tool{
		Name:        "features",
		Description: "List feature groups for a project, including readiness, task counts, and representative tasks.",
		InputSchema: InputSchema{Type: "object", Properties: props},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProject(args)
		path := "/tasks/" + url.PathEscape(project) + "/features"
		title := fmt.Sprintf("Features for project: %s", project)
		empty := "No features found"
		if BoolArg(args, "ready_only", false) {
			path += "/ready"
			title = fmt.Sprintf("Ready features for project: %s", project)
			empty = "No ready features found"
		}

		var resp types.FeatureListResponse
		if err := client.Request(ctx, http.MethodGet, path, nil, nil, &resp); err != nil {
			return "", err
		}
		return formatFeatureList(title, empty, resp.Features, IntArg(args, "limit", 50)), nil
	})
}

func registerBrainFeatureReady(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "feature_ready",
		Description: "List feature groups that are ready to run for a project.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string", Description: "Override auto-detected project"}, "limit": {Type: "number", Description: "Maximum features to include in the summary"}}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProject(args)
		var resp types.FeatureListResponse
		if err := client.Request(ctx, http.MethodGet, "/tasks/"+url.PathEscape(project)+"/features/ready", nil, nil, &resp); err != nil {
			return "", err
		}
		return formatFeatureList(fmt.Sprintf("Ready features for project: %s", project), "No ready features found", resp.Features, IntArg(args, "limit", 50)), nil
	})
}

func registerBrainFeatureGet(s *Server, client *APIClient) {
	props := featureCommonProperties()
	s.RegisterTool(Tool{
		Name:        "feature_get",
		Description: "Show one feature's readiness, task counts, and task dependency state.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"feature_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProject(args)
		featureID := StringArg(args, "feature_id", "")
		if featureID == "" {
			return "", fmt.Errorf("provide a 'feature_id'")
		}

		var resp types.FeatureResponse
		path := "/tasks/" + url.PathEscape(project) + "/features/" + url.PathEscape(featureID)
		if err := client.Request(ctx, http.MethodGet, path, nil, nil, &resp); err != nil {
			return "", err
		}
		return formatFeatureDetail(project, resp.Feature), nil
	})
}

func registerBrainFeatureCheckout(s *Server, client *APIClient) {
	props := featureCommonProperties()
	props["execution_branch"] = Property{Type: "string", Description: "Execution branch for checkout work"}
	props["merge_target_branch"] = Property{Type: "string", Description: "Branch the feature should merge into"}
	props["merge_policy"] = Property{Type: "string", Enum: []string{"prompt_only", "auto_pr", "auto_merge"}, Description: "Merge policy for checkout work"}
	props["merge_strategy"] = Property{Type: "string", Enum: []string{"squash", "merge", "rebase"}, Description: "Merge strategy for checkout work"}
	props["remote_branch_policy"] = Property{Type: "string", Enum: []string{"keep", "delete"}, Description: "Remote branch cleanup policy"}
	props["open_pr_before_merge"] = Property{Type: "boolean", Description: "Open a PR before merge"}
	props["execution_mode"] = Property{Type: "string", Enum: []string{"worktree", "current_branch"}, Description: "Execution mode for generated checkout work"}
	props["checkout_mode"] = Property{Type: "string", Enum: types.CheckoutModes, Description: "Feature checkout automation mode: 'ai' (default) runs the feature-checkout skill; 'simple' triggers a deterministic squash-merge automation."}
	s.RegisterTool(Tool{
		Name:        "feature_checkout",
		Description: "Create or reuse a feature checkout task for review and merge orchestration.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"feature_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProject(args)
		featureID := StringArg(args, "feature_id", "")
		if featureID == "" {
			return "", fmt.Errorf("provide a 'feature_id'")
		}

		req := types.FeatureCheckoutOptions{
			ExecutionBranch:    StringArg(args, "execution_branch", ""),
			MergeTargetBranch:  StringArg(args, "merge_target_branch", ""),
			MergePolicy:        StringArg(args, "merge_policy", ""),
			MergeStrategy:      StringArg(args, "merge_strategy", ""),
			RemoteBranchPolicy: StringArg(args, "remote_branch_policy", ""),
			OpenPRBeforeMerge:  BoolArg(args, "open_pr_before_merge", false),
			ExecutionMode:      StringArg(args, "execution_mode", ""),
			CheckoutMode:       StringArg(args, "checkout_mode", ""),
		}
		var resp types.CheckoutFeatureResult
		path := "/tasks/" + url.PathEscape(project) + "/features/" + url.PathEscape(featureID) + "/checkout"
		if err := client.Request(ctx, http.MethodPost, path, req, nil, &resp); err != nil {
			return "", err
		}
		return formatFeatureCheckout(project, featureID, resp), nil
	})
}

func registerBrainFeatureAssign(s *Server, client *APIClient) {
	props := featureCommonProperties()
	props["runner_id"] = Property{Type: "string", Description: "Runner ID to assign this feature to"}
	props["intent"] = Property{Type: "string", Description: "Human-readable reason for the assignment"}
	props["force"] = Property{Type: "boolean", Description: "Reassign even if another runner currently owns the feature"}
	s.RegisterTool(Tool{
		Name:        "feature_assign",
		Description: "Assign or reassign a feature to a runner.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"feature_id", "runner_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProject(args)
		featureID := StringArg(args, "feature_id", "")
		runnerID := StringArg(args, "runner_id", "")
		if featureID == "" {
			return "", fmt.Errorf("provide a 'feature_id'")
		}
		if runnerID == "" {
			return "", fmt.Errorf("provide a 'runner_id'")
		}

		req := types.FeatureAssignmentRequest{RunnerID: runnerID, Intent: StringArg(args, "intent", ""), Force: BoolArg(args, "force", false)}
		var resp types.FeatureAssignmentResponse
		path := "/tasks/" + url.PathEscape(project) + "/features/" + url.PathEscape(featureID) + "/assignment"
		if err := client.Request(ctx, http.MethodPut, path, req, nil, &resp); err != nil {
			return "", err
		}
		return formatFeatureAssignment(resp), nil
	})
}

func registerBrainFeatureClearAssignment(s *Server, client *APIClient) {
	props := featureCommonProperties()
	props["intent"] = Property{Type: "string", Description: "Human-readable reason for clearing the assignment"}
	s.RegisterTool(Tool{
		Name:        "feature_clear_assignment",
		Description: "Clear the current runner assignment for a feature.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"feature_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := ResolveProject(args)
		featureID := StringArg(args, "feature_id", "")
		if featureID == "" {
			return "", fmt.Errorf("provide a 'feature_id'")
		}

		intent := StringArg(args, "intent", "clear")
		req := types.ClearFeatureAssignmentRequest{Intent: intent}
		var resp types.FeatureAssignmentResponse
		path := "/tasks/" + url.PathEscape(project) + "/features/" + url.PathEscape(featureID) + "/assignment/clear"
		if err := client.Request(ctx, http.MethodPost, path, req, nil, &resp); err != nil {
			return "", err
		}
		return formatFeatureAssignment(resp), nil
	})
}

func formatFeatureList(title, empty string, features []types.Feature, limit int) string {
	if limit <= 0 {
		limit = 50
	}
	if len(features) == 0 {
		return fmt.Sprintf("## %s\n\n%s.", title, empty)
	}
	// Disclose the cut. The server returns the complete, unpaginated list, so
	// the information to say "showing 50 of 63" is already in hand — dropping
	// the rest silently made a truncated view look like the whole project.
	// The per-feature task list three lines below has always said "...and N
	// more tasks"; this applies the same courtesy one level up.
	omitted := 0
	if len(features) > limit {
		omitted = len(features) - limit
		features = features[:limit]
	}

	lines := []string{fmt.Sprintf("## %s", title), ""}
	if omitted > 0 {
		lines = append(lines,
			fmt.Sprintf("_Showing %d of %d features — %d omitted. Raise 'limit' to see the rest._",
				limit, limit+omitted, omitted),
			"")
	}
	for _, feature := range features {
		lines = append(lines, formatFeatureSummaryLines(feature)...)
		if len(feature.Tasks) > 0 {
			for i, task := range feature.Tasks {
				if i >= 3 {
					lines = append(lines, fmt.Sprintf("  - ...and %d more tasks", len(feature.Tasks)-i))
					break
				}
				lines = append(lines, fmt.Sprintf("  - %s (`%s`) - %s/%s", task.Title, task.ID, task.Status, task.Classification))
			}
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func formatFeatureDetail(project string, feature types.Feature) string {
	lines := []string{
		fmt.Sprintf("## Feature %s", feature.FeatureID),
		"",
		fmt.Sprintf("- Project: %s", project),
	}
	lines = append(lines, featureStateLines(feature)...)
	lines = append(lines, formatStatsLine(feature.Stats))
	if len(feature.Tasks) == 0 {
		lines = append(lines, "", "No tasks in this feature.")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, formatFeatureGitLines(feature.Tasks)...)
	lines = append(lines, "", "### Tasks")
	for _, task := range feature.Tasks {
		lines = append(lines, fmt.Sprintf("- %s (`%s`) - %s/%s", task.Title, task.ID, task.Status, task.Classification))
		if len(task.WaitingOn) > 0 {
			lines = append(lines, fmt.Sprintf("  waiting_on: %s", strings.Join(task.WaitingOn, ", ")))
		}
		if task.BlockedByReason != "" {
			lines = append(lines, fmt.Sprintf("  blocked_by: %s", task.BlockedByReason))
		}
	}
	return strings.Join(lines, "\n")
}

// formatFeatureGitLines renders where a feature's work lives in git and how
// it is meant to land.
//
// ResolvedTask carries nine git and merge fields, and none of them were
// shown anywhere. An agent asking the entirely ordinary question "what
// branch is this feature on, and what does it merge into?" got back only
// title/status/classification, despite the answer already being decoded and
// in hand.
//
// Values are folded across the feature's tasks because the model is one
// feature, one branch, one merge. Nothing enforces that, though, so when
// tasks disagree every distinct value is listed and marked — a feature
// whose tasks name three different merge targets is a checkout that will
// misbehave in a way nobody can see from the task list.
func formatFeatureGitLines(tasks []types.ResolvedTask) []string {
	if len(tasks) == 0 {
		return nil
	}

	fields := []struct {
		label string
		get   func(types.ResolvedTask) string
	}{
		{"branch", func(t types.ResolvedTask) string { return t.GitBranch }},
		{"merges into", func(t types.ResolvedTask) string { return t.MergeTargetBranch }},
		{"merge policy", func(t types.ResolvedTask) string { return t.MergePolicy }},
		{"merge strategy", func(t types.ResolvedTask) string { return t.MergeStrategy }},
		{"remote branch", func(t types.ResolvedTask) string { return t.RemoteBranchPolicy }},
		{"execution mode", func(t types.ResolvedTask) string { return t.ExecutionMode }},
		{"workdir", func(t types.ResolvedTask) string { return t.TargetWorkdir }},
		{"git remote", func(t types.ResolvedTask) string { return t.GitRemote }},
	}

	var body []string
	for _, f := range fields {
		var distinct []string
		seen := map[string]bool{}
		for _, t := range tasks {
			v := strings.TrimSpace(f.get(t))
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			distinct = append(distinct, v)
		}
		switch len(distinct) {
		case 0:
			// Nothing set on any task — say so only for the two fields
			// whose absence actually changes what happens at checkout.
			if f.label == "branch" || f.label == "merges into" {
				body = append(body, fmt.Sprintf("- %s: (unset)", f.label))
			}
		case 1:
			body = append(body, fmt.Sprintf("- %s: %s", f.label, distinct[0]))
		default:
			sort.Strings(distinct)
			body = append(body, fmt.Sprintf("- %s: ⚠ tasks disagree — %s", f.label, strings.Join(distinct, ", ")))
		}
	}

	if len(body) == 0 {
		return nil
	}
	return append([]string{"", "### Git & merge"}, body...)
}

// featureStateLines renders a feature's two INDEPENDENT states, which the
// previous single "- Status: ready" line conflated.
//
// types.Feature.Ready is `Classification == "ready"`, meaning the feature's
// FEATURE-LEVEL DEPENDENCIES are satisfied. It says nothing about whether any
// task inside can run. Rendering that as the bare word "ready" collided with the
// task-level classification of the same name — which does mean runnable — and
// the collision appeared in the same output, one line above a stats line reading
// "Ready: 0".
//
// Observed live on brain-api: a feature whose 13 tasks were all completed, one
// whose 15 tasks were all still draft, and one with genuinely runnable work all
// rendered "Status: ready". An agent picking work off that list cannot tell them
// apart. (feature_ready filters completed/archived out; the plain features list
// never did.)
//
// So dependency state is now named as dependency state, and the actionable
// question — is there runnable work here — is answered separately from the
// stats that were already on screen.
func featureStateLines(feature types.Feature) []string {
	deps := "waiting on other features"
	if feature.Ready {
		deps = "satisfied"
	}
	return []string{
		fmt.Sprintf("- Dependencies: %s", deps),
		fmt.Sprintf("- Work: %s", featureWorkState(feature.Stats)),
	}
}

// featureWorkState summarizes what an agent can actually do with the feature.
// Deliberately derived only from counts already shown, so the summary and the
// stats line can never disagree.
func featureWorkState(stats *types.TaskStats) string {
	if stats == nil || stats.Total == 0 {
		return "no tasks"
	}
	switch {
	case stats.Ready > 0:
		return fmt.Sprintf("%d task(s) ready to run", stats.Ready)
	case stats.Waiting > 0:
		return fmt.Sprintf("nothing runnable yet — %d waiting on dependencies", stats.Waiting)
	case stats.Blocked > 0 || stats.StatusBlocked > 0:
		return "nothing runnable — tasks are blocked"
	case stats.NotPending == stats.Total:
		// Everything is outside the pending lifecycle: finished, cancelled, or
		// still draft. "Finished" and "not started yet" are opposite situations
		// that both land here, so do not guess between them — say which bucket
		// and let the task list below disambiguate.
		return fmt.Sprintf("nothing runnable — all %d task(s) are outside the pending lifecycle (completed, draft, or cancelled)", stats.Total)
	default:
		return "nothing runnable"
	}
}

func formatFeatureSummaryLines(feature types.Feature) []string {
	lines := []string{fmt.Sprintf("### %s", feature.FeatureID)}
	lines = append(lines, featureStateLines(feature)...)
	return append(lines, formatStatsLine(feature.Stats))
}

func formatStatsLine(stats *types.TaskStats) string {
	if stats == nil {
		return "- Tasks: unknown"
	}
	return fmt.Sprintf("- Tasks: %d | Ready: %d | Waiting: %d | Blocked: %d | Not pending: %d", stats.Total, stats.Ready, stats.Waiting, stats.Blocked, stats.NotPending)
}

func formatFeatureCheckout(project, featureID string, result types.CheckoutFeatureResult) string {
	lines := []string{
		"## Feature checkout",
		"",
		fmt.Sprintf("- Project: %s", project),
		fmt.Sprintf("- Feature: %s", featureID),
		fmt.Sprintf("- created: %t", result.Created),
	}
	if result.GeneratedKey != "" {
		lines = append(lines, fmt.Sprintf("- Generated key: %s", result.GeneratedKey))
	}
	if result.Task != nil {
		lines = append(lines, "", "### Task", fmt.Sprintf("- Title: %s", result.Task.Title), fmt.Sprintf("- ID: %s", result.Task.ID), fmt.Sprintf("- Status: %s", result.Task.Status), fmt.Sprintf("- Path: %s", result.Task.Path))
	}
	return strings.Join(lines, "\n")
}

func formatFeatureAssignment(resp types.FeatureAssignmentResponse) string {
	lines := []string{"## Feature assignment", ""}
	if resp.ProjectID != "" {
		lines = append(lines, fmt.Sprintf("- Project: %s", resp.ProjectID))
	}
	if resp.FeatureID != "" {
		lines = append(lines, fmt.Sprintf("- Feature: %s", resp.FeatureID))
	}
	if resp.Status != "" {
		lines = append(lines, fmt.Sprintf("- Status: %s", resp.Status))
	}
	if resp.RunnerID != "" {
		lines = append(lines, fmt.Sprintf("- Runner: %s", resp.RunnerID))
	}
	if resp.PreviousRunner != "" {
		lines = append(lines, fmt.Sprintf("- Previous runner: %s", resp.PreviousRunner))
	}
	if resp.Source != "" {
		lines = append(lines, fmt.Sprintf("- Source: %s", resp.Source))
	}
	if resp.AssignedAt != "" {
		lines = append(lines, fmt.Sprintf("- Assigned at: %s", resp.AssignedAt))
	}
	if resp.UpdatedAt != "" {
		lines = append(lines, fmt.Sprintf("- Updated at: %s", resp.UpdatedAt))
	}
	return strings.Join(lines, "\n")
}
