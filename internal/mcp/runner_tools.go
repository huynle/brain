package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterRunnerTools registers read-only runner visibility MCP tools.
func RegisterRunnerTools(s *Server, client *APIClient) {
	registerBrainRunnerStatus(s, client)
	registerBrainRunners(s, client)
	registerBrainRunnerGet(s, client)
	registerBrainRunnerInstances(s, client)
	registerBrainRunnerInstancesAll(s, client)
}

func registerBrainRunnerStatus(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_status",
		Description: "Show runner service pause/running status.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		var resp types.RunnerStatusResponse
		if err := client.Request(ctx, http.MethodGet, "/tasks/runner/status", nil, nil, &resp); err != nil {
			return "", err
		}
		return formatRunnerStatus(resp), nil
	})
}

func registerBrainRunners(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runners",
		Description: "List registered runners with state, projects, capabilities, counts, and timestamps.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"status":   {Type: "string", Enum: []string{"online", "stale", "offline"}, Description: "Optional client-side status filter"},
			"executor": {Type: "string", Description: "Optional client-side executor filter, e.g. opencode or pi"},
			"project":  {Type: "string", Description: "Optional client-side project filter"},
			"limit":    {Type: "number", Description: "Maximum runners to show"},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		var resp types.RunnerListResponse
		if err := client.Request(ctx, http.MethodGet, "/runners", nil, nil, &resp); err != nil {
			return "", err
		}
		resp.Runners = filterRunners(resp.Runners, args)
		if limit := IntArg(args, "limit", 0); limit > 0 && len(resp.Runners) > limit {
			resp.Runners = resp.Runners[:limit]
		}
		resp.Total = len(resp.Runners)
		return formatRunnerList(resp), nil
	})
}

func registerBrainRunnerGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_get",
		Description: "Get one registered runner by ID.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id": {Type: "string", Description: "Runner ID"},
		}, Required: []string{"runner_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		runnerID := StringArgAlias(args, "", "runner_id", "runnerId")
		if runnerID == "" {
			return "", fmt.Errorf("runner_id is required")
		}
		var resp types.RunnerInfo
		if err := client.Request(ctx, http.MethodGet, "/runners/"+url.PathEscape(runnerID), nil, nil, &resp); err != nil {
			return "", err
		}
		return formatRunner(resp), nil
	})
}

func registerBrainRunnerInstances(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_instances",
		Description: "List executor instances for one runner.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id": {Type: "string", Description: "Runner ID"},
			"status":    {Type: "string", Enum: []string{"starting", "idle", "busy", "exited"}, Description: "Optional client-side status filter"},
			"kind":      {Type: "string", Enum: []string{"task", "adhoc"}, Description: "Optional client-side kind filter"},
			"project":   {Type: "string", Description: "Optional client-side project_id filter"},
		}, Required: []string{"runner_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		runnerID := StringArgAlias(args, "", "runner_id", "runnerId")
		if runnerID == "" {
			return "", fmt.Errorf("runner_id is required")
		}
		var resp types.InstanceListResponse
		if err := client.Request(ctx, http.MethodGet, "/runners/"+url.PathEscape(runnerID)+"/instances", nil, nil, &resp); err != nil {
			return "", err
		}
		resp.Instances = filterInstances(resp.Instances, args)
		resp.Total = len(resp.Instances)
		return formatInstanceList("Runner: "+runnerID, resp), nil
	})
}

func registerBrainRunnerInstancesAll(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_instances_all",
		Description: "List executor instances across all runners.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id": {Type: "string", Description: "Optional client-side runner filter"},
			"status":    {Type: "string", Enum: []string{"starting", "idle", "busy", "exited"}, Description: "Optional client-side status filter"},
			"kind":      {Type: "string", Enum: []string{"task", "adhoc"}, Description: "Optional client-side kind filter"},
			"project":   {Type: "string", Description: "Optional client-side project_id filter"},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		var resp types.InstanceListResponse
		if err := client.Request(ctx, http.MethodGet, "/instances", nil, nil, &resp); err != nil {
			return "", err
		}
		resp.Instances = filterInstances(resp.Instances, args)
		resp.Total = len(resp.Instances)
		return formatInstanceList("All runners", resp), nil
	})
}

func filterRunners(runners []types.RunnerInfo, args map[string]any) []types.RunnerInfo {
	status := StringArg(args, "status", "")
	executor := StringArg(args, "executor", "")
	project := StringArg(args, "project", "")
	if status == "" && executor == "" && project == "" {
		return runners
	}
	filtered := make([]types.RunnerInfo, 0, len(runners))
	for _, runner := range runners {
		if status != "" && string(runner.Status) != status {
			continue
		}
		if executor != "" && !containsString(runner.Executors, executor) {
			continue
		}
		if project != "" && !containsString(runner.Projects, project) {
			continue
		}
		filtered = append(filtered, runner)
	}
	return filtered
}

func filterInstances(instances []types.OpencodeInstance, args map[string]any) []types.OpencodeInstance {
	runnerID := StringArgAlias(args, "", "runner_id", "runnerId")
	status := StringArg(args, "status", "")
	kind := StringArg(args, "kind", "")
	project := StringArg(args, "project", "")
	if runnerID == "" && status == "" && kind == "" && project == "" {
		return instances
	}
	filtered := make([]types.OpencodeInstance, 0, len(instances))
	for _, instance := range instances {
		if runnerID != "" && instance.RunnerID != runnerID {
			continue
		}
		if status != "" && instance.Status != status {
			continue
		}
		if kind != "" && instance.Kind != kind {
			continue
		}
		if project != "" && instance.ProjectID != project {
			continue
		}
		filtered = append(filtered, instance)
	}
	return filtered
}

func formatRunnerStatus(status types.RunnerStatusResponse) string {
	var b strings.Builder
	b.WriteString("## Runner Status\n\n")
	fmt.Fprintf(&b, "- Running: %t\n", status.Running)
	fmt.Fprintf(&b, "- Paused: %t\n", status.Paused)
	fmt.Fprintf(&b, "- Paused projects: %s\n", formatStringList(status.PausedProjects))
	fmt.Fprintf(&b, "- Automations paused: %t\n", status.AutomationsPaused)
	fmt.Fprintf(&b, "- Automation paused projects: %s\n", formatStringList(status.AutomationPausedProjects))
	return b.String()
}

func formatRunnerList(resp types.RunnerListResponse) string {
	var b strings.Builder
	b.WriteString("## Runners\n\n")
	fmt.Fprintf(&b, "Total: %d\n\n", resp.Total)
	if len(resp.Runners) == 0 {
		b.WriteString("No runners found.\n")
		return b.String()
	}
	for _, runner := range resp.Runners {
		writeRunnerSummary(&b, runner)
		b.WriteString("- Instances: use runner_instances with this runner_id\n\n")
	}
	return b.String()
}

func formatRunner(runner types.RunnerInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Runner %s\n\n", runner.RunnerID)
	writeRunnerSummary(&b, runner)
	return b.String()
}

func writeRunnerSummary(b *strings.Builder, runner types.RunnerInfo) {
	fmt.Fprintf(b, "### %s\n", runner.RunnerID)
	fmt.Fprintf(b, "- Hostname: %s\n", runner.Hostname)
	fmt.Fprintf(b, "- State: %s\n", runner.Status)
	fmt.Fprintf(b, "- Projects: %s\n", formatStringList(runner.Projects))
	fmt.Fprintf(b, "- Capabilities: %s\n", formatStringList(runner.Capabilities))
	fmt.Fprintf(b, "- Executors: %s\n", formatStringList(runner.Executors))
	fmt.Fprintf(b, "- Max parallel: %d\n", runner.MaxParallel)
	fmt.Fprintf(b, "- Active tasks: %d\n", runner.ActiveTasks)
	fmt.Fprintf(b, "- Draining: %t\n", runner.Draining)
	fmt.Fprintf(b, "- Dispatch push: %t\n", runner.DispatchPush)
	if runner.RegisteredAt != "" {
		fmt.Fprintf(b, "- Registered: %s\n", runner.RegisteredAt)
	}
	if runner.LastHeartbeat != "" {
		fmt.Fprintf(b, "- Last heartbeat: %s\n", runner.LastHeartbeat)
	}
	if runner.Version != "" {
		fmt.Fprintf(b, "- Version: %s\n", runner.Version)
	}
}

func formatInstanceList(scope string, resp types.InstanceListResponse) string {
	var b strings.Builder
	b.WriteString("## Runner Instances\n\n")
	fmt.Fprintf(&b, "%s\n", scope)
	fmt.Fprintf(&b, "Total: %d\n\n", resp.Total)
	if len(resp.Instances) == 0 {
		b.WriteString("No instances found.\n")
		return b.String()
	}
	for _, instance := range resp.Instances {
		fmt.Fprintf(&b, "### %s\n", instance.InstanceID)
		fmt.Fprintf(&b, "- Runner: %s\n", instance.RunnerID)
		fmt.Fprintf(&b, "- Kind: %s\n", instance.Kind)
		fmt.Fprintf(&b, "- Status: %s\n", instance.Status)
		fmt.Fprintf(&b, "- Project: %s\n", valueOrNone(instance.ProjectID))
		fmt.Fprintf(&b, "- Task: %s\n", valueOrNone(instance.TaskID))
		fmt.Fprintf(&b, "- Feature: %s\n", valueOrNone(instance.FeatureID))
		fmt.Fprintf(&b, "- Title: %s\n", valueOrNone(instance.Title))
		fmt.Fprintf(&b, "- Executor: %s\n", valueOrNone(instance.Executor))
		fmt.Fprintf(&b, "- Agent: %s\n", valueOrNone(instance.Agent))
		fmt.Fprintf(&b, "- Model: %s\n", valueOrNone(instance.Model))
		if instance.Port != 0 {
			fmt.Fprintf(&b, "- Port: %d\n", instance.Port)
		}
		if instance.PID != 0 {
			fmt.Fprintf(&b, "- PID: %d\n", instance.PID)
		}
		if instance.StartedAt != 0 {
			fmt.Fprintf(&b, "- Started: %d\n", instance.StartedAt)
		}
		if instance.LastSeen != 0 {
			fmt.Fprintf(&b, "- Last seen: %d\n", instance.LastSeen)
		}
		fmt.Fprintf(&b, "- Pending permissions: %d\n", instance.PendingPermissions)
		fmt.Fprintf(&b, "- Bridge connected: %t\n\n", instance.BridgeConnected)
	}
	return b.String()
}

func formatStringList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func valueOrNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}
