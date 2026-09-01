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

// RegisterProjectTools registers project context and placement MCP tools on the server.
func RegisterProjectTools(s *Server, client *APIClient) {
	registerBrainContextGet(s, client)
	registerBrainContextResolve(s, client)
	registerBrainProjectPlacementGet(s, client)
	registerBrainProjectPlacementPut(s, client)
}

func registerBrainContextGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "context_get",
		Description: `Show the ambient execution context this MCP server resolved at startup.

Tools that take an optional 'project' parameter fall back to the project shown here when it is omitted. Use this to check which project a save/list/attachment call will target by default, and which identity (client/host) is stamped on MCP-created tasks.`,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		execCtx := GetCachedContext()
		lines := []string{
			"## MCP Execution Context",
			"",
			"### Project (default for tools when 'project' is omitted)",
		}
		// An empty ProjectID means detection failed, not that the project is
		// named "". Say so, because the alternative — guessing from the
		// working directory basename — is how seven Hindsight entries ended
		// up in a project called "app".
		if execCtx.ProjectID == "" {
			lines = append(lines,
				"- Project: ⚠ COULD NOT DETERMINE",
				"  This process is not running inside a recognised git repository under your",
				"  home directory, so there is no safe default project. Pass 'project'",
				"  explicitly on every tool call; omitting it will not fall back to a guess.")
		} else {
			lines = append(lines, fmt.Sprintf("- Project: %s", execCtx.ProjectID))
		}
		lines = append(lines, fmt.Sprintf("- Workdir: %s", execCtx.Workdir))
		if execCtx.GitRemote != "" {
			lines = append(lines, fmt.Sprintf("- Git remote: %s", execCtx.GitRemote))
		}
		if execCtx.GitBranch != "" {
			lines = append(lines, fmt.Sprintf("- Git branch: %s", execCtx.GitBranch))
		}
		lines = append(lines,
			"",
			"### Identity (stamped on MCP-created tasks)",
			fmt.Sprintf("- Client ID: %s", execCtx.ClientID),
			fmt.Sprintf("- Host ID: %s", execCtx.HostID),
			fmt.Sprintf("- Hostname: %s", execCtx.Hostname),
			fmt.Sprintf("- OS/Arch: %s/%s", execCtx.OS, execCtx.Arch),
		)
		// Say plainly whether stamping is actually happening. The identity
		// above is this process's, and over the HTTP transport this process
		// is the Brain API — not the caller — so nothing is stamped.
		if s.ambientContextDescribesCaller() {
			if execCtx.AbsPath != "" {
				lines = append(lines, fmt.Sprintf("- Origin path: %s", execCtx.AbsPath))
			}
			lines = append(lines,
				"- Tasks created here are stamped origin_machine_id/origin_client_id/origin_path,",
				"  and default to machine_affinity=preferred (a runner on this machine wins,",
				"  but the task still runs elsewhere if none is available). Pass",
				"  machine_affinity='local' to require this machine.")
		} else {
			lines = append(lines,
				"- ⚠ Origin stamping is DISABLED for this session: this MCP server runs inside",
				"  the Brain API, so the identity above describes the API host rather than you.",
				"  Tasks created here carry no origin, and machine_affinity has nothing to",
				"  resolve against. Use the stdio MCP server for machine-affine tasks.")
		}
		if execCtx.Username != "" {
			lines = append(lines, fmt.Sprintf("- Username: %s", execCtx.Username))
		}
		lines = append(lines,
			"",
			"### API",
			fmt.Sprintf("- Base URL: %s", client.baseURL),
		)
		return strings.Join(lines, "\n"), nil
	})
}

func registerBrainContextResolve(s *Server, client *APIClient) {
	props := map[string]Property{
		"client_id":         {Type: "string", Description: "Unique Brain client ID"},
		"host_id":           {Type: "string", Description: "Stable host identifier for the client"},
		"kind":              {Type: "string", Description: "Client kind, such as opencode or runner"},
		"hostname":          {Type: "string", Description: "Hostname reported by the client"},
		"os":                {Type: "string", Description: "Operating system"},
		"arch":              {Type: "string", Description: "CPU architecture"},
		"username":          {Type: "string", Description: "Username running the client"},
		"home_dir":          {Type: "string", Description: "Client home directory"},
		"labels":            {Type: "object", Description: "Client labels used for placement or identity"},
		"capabilities":      {Type: "array", Items: &Property{Type: "string"}, Description: "Client capabilities"},
		"path":              {Type: "string", Description: "Observed workspace path"},
		"git_root":          {Type: "string", Description: "Workspace git root"},
		"git_common_dir":    {Type: "string", Description: "Workspace git common directory"},
		"git_worktree_main": {Type: "string", Description: "Main worktree path"},
		"git_branch":        {Type: "string", Description: "Current git branch"},
		"git_remote":        {Type: "string", Description: "Git remote URL"},
		"folder_name":       {Type: "string", Description: "Workspace folder name"},
	}
	s.RegisterTool(Tool{
		Name:        "context_resolve",
		Description: "Resolve the Brain project for a client/workspace observation.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"client_id", "host_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		clientID := StringArg(args, "client_id", "")
		hostID := StringArg(args, "host_id", "")
		if clientID == "" {
			return "", fmt.Errorf("provide a 'client_id'")
		}
		if hostID == "" {
			return "", fmt.Errorf("provide a 'host_id'")
		}

		req := types.ResolveClientContextRequest{
			Client: types.BrainClientInfo{
				ClientID:     clientID,
				Kind:         StringArg(args, "kind", ""),
				HostID:       hostID,
				Hostname:     StringArg(args, "hostname", ""),
				OS:           StringArg(args, "os", ""),
				Arch:         StringArg(args, "arch", ""),
				Username:     StringArg(args, "username", ""),
				HomeDir:      StringArg(args, "home_dir", ""),
				Labels:       projectStringMapArg(args, "labels"),
				Capabilities: StringSliceArg(args, "capabilities"),
			},
			Workspace: types.WorkspaceObservation{
				Path:            StringArg(args, "path", ""),
				GitRoot:         StringArg(args, "git_root", ""),
				GitCommonDir:    StringArg(args, "git_common_dir", ""),
				GitWorktreeMain: StringArg(args, "git_worktree_main", ""),
				GitBranch:       StringArg(args, "git_branch", ""),
				GitRemote:       StringArg(args, "git_remote", ""),
				FolderName:      StringArg(args, "folder_name", ""),
			},
		}
		var resp types.ResolveClientContextResponse
		if err := client.Request(ctx, http.MethodPost, "/context/resolve", req, nil, &resp); err != nil {
			return "", err
		}
		return formatContextResolution(req, resp), nil
	})
}

func registerBrainProjectPlacementGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "project_placement_get",
		Description: "Get Brain scheduling placement configuration for a project.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{"project": {Type: "string", Description: "Project ID"}}, Required: []string{"project"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := StringArg(args, "project", "")
		if project == "" {
			return "", fmt.Errorf("provide a 'project'")
		}
		var resp types.ProjectPlacement
		if err := client.Request(ctx, http.MethodGet, "/projects/"+url.PathEscape(project)+"/placement", nil, nil, &resp); err != nil {
			return "", err
		}
		return formatProjectPlacement("Project placement", resp), nil
	})
}

func registerBrainProjectPlacementPut(s *Server, client *APIClient) {
	props := projectPlacementProperties()
	s.RegisterTool(Tool{
		Name:        "project_placement_put",
		Description: "Create or update Brain scheduling placement configuration for a project.",
		InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"project"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := StringArg(args, "project", "")
		if project == "" {
			return "", fmt.Errorf("provide a 'project'")
		}
		req := types.ProjectPlacement{
			Affinity:             StringArg(args, "affinity", ""),
			PreferredMachines:    StringSliceArg(args, "preferred_machines"),
			AllowedMachines:      StringSliceArg(args, "allowed_machines"),
			WorkspacePolicy:      StringArg(args, "workspace_policy", ""),
			RequiredLabels:       projectStringMapArg(args, "required_labels"),
			RequiredCapabilities: StringSliceArg(args, "required_capabilities"),
			Resources:            anyMapArg(args, "resources"),
		}
		var resp types.ProjectPlacement
		if err := client.Request(ctx, http.MethodPut, "/projects/"+url.PathEscape(project)+"/placement", req, nil, &resp); err != nil {
			return "", err
		}
		return formatProjectPlacement("Project placement updated", resp), nil
	})
}

func projectPlacementProperties() map[string]Property {
	return map[string]Property{
		"project":               {Type: "string", Description: "Project ID"},
		"affinity":              {Type: "string", Enum: []string{"strict", "soft", "none"}, Description: "Machine affinity policy"},
		"preferred_machines":    {Type: "array", Items: &Property{Type: "string"}, Description: "Preferred machine IDs"},
		"allowed_machines":      {Type: "array", Items: &Property{Type: "string"}, Description: "Allowed machine IDs"},
		"workspace_policy":      {Type: "string", Enum: []string{"worktree", "current_branch"}, Description: "Workspace execution policy"},
		"required_labels":       {Type: "object", Description: "Required client labels"},
		"required_capabilities": {Type: "array", Items: &Property{Type: "string"}, Description: "Required client capabilities"},
		"resources":             {Type: "object", Description: "Resource requirements"},
	}
}

func formatContextResolution(req types.ResolveClientContextRequest, resp types.ResolveClientContextResponse) string {
	lines := []string{
		"## Context resolution",
		"",
		fmt.Sprintf("- Project: %s", resp.ProjectID),
		fmt.Sprintf("- Confidence: %s", resp.Confidence),
		fmt.Sprintf("- Source: %s", resp.Source),
		"",
		"### Client",
		fmt.Sprintf("- Client: %s", req.Client.ClientID),
		fmt.Sprintf("- Host: %s", req.Client.HostID),
	}
	if req.Client.Kind != "" {
		lines = append(lines, fmt.Sprintf("- Kind: %s", req.Client.Kind))
	}
	if req.Client.Hostname != "" {
		lines = append(lines, fmt.Sprintf("- Hostname: %s", req.Client.Hostname))
	}
	if len(req.Client.Labels) > 0 {
		lines = append(lines, fmt.Sprintf("- Labels: %s", formatStringMapInline(req.Client.Labels)))
	}
	if len(req.Client.Capabilities) > 0 {
		lines = append(lines, fmt.Sprintf("- Capabilities: %s", strings.Join(req.Client.Capabilities, ", ")))
	}
	lines = append(lines, "", "### Workspace")
	if req.Workspace.Path != "" {
		lines = append(lines, fmt.Sprintf("- Workspace: %s", req.Workspace.Path))
	}
	if req.Workspace.GitBranch != "" {
		lines = append(lines, fmt.Sprintf("- Git branch: %s", req.Workspace.GitBranch))
	}
	if req.Workspace.GitRemote != "" {
		lines = append(lines, fmt.Sprintf("- Git remote: %s", req.Workspace.GitRemote))
	}
	if req.Workspace.FolderName != "" {
		lines = append(lines, fmt.Sprintf("- Folder: %s", req.Workspace.FolderName))
	}
	if resp.Dream != nil {
		lines = append(lines, "", "### Dream context")
		if resp.Dream.Title != "" {
			lines = append(lines, fmt.Sprintf("- Dream: %s", resp.Dream.Title))
		}
		if resp.Dream.Path != "" {
			lines = append(lines, fmt.Sprintf("- Path: %s", resp.Dream.Path))
		}
	}
	return strings.Join(lines, "\n")
}

func formatProjectPlacement(title string, placement types.ProjectPlacement) string {
	lines := []string{
		"## " + title,
		"",
		fmt.Sprintf("- Project: %s", placement.ProjectID),
		fmt.Sprintf("- Affinity: %s", placement.Affinity),
	}
	if len(placement.PreferredMachines) > 0 {
		lines = append(lines, fmt.Sprintf("- Preferred machines: %s", strings.Join(placement.PreferredMachines, ", ")))
	}
	if len(placement.AllowedMachines) > 0 {
		lines = append(lines, fmt.Sprintf("- Allowed machines: %s", strings.Join(placement.AllowedMachines, ", ")))
	}
	if placement.WorkspacePolicy != "" {
		lines = append(lines, fmt.Sprintf("- Workspace policy: %s", placement.WorkspacePolicy))
	}
	if len(placement.RequiredLabels) > 0 {
		lines = append(lines, fmt.Sprintf("- Required labels: %s", formatStringMapInline(placement.RequiredLabels)))
	}
	if len(placement.RequiredCapabilities) > 0 {
		lines = append(lines, fmt.Sprintf("- Required capabilities: %s", strings.Join(placement.RequiredCapabilities, ", ")))
	}
	if len(placement.Resources) > 0 {
		lines = append(lines, "", "### Resources")
		for _, key := range sortedAnyMapKeys(placement.Resources) {
			lines = append(lines, fmt.Sprintf("- %s: %v", key, placement.Resources[key]))
		}
	}
	return strings.Join(lines, "\n")
}

func projectStringMapArg(args map[string]any, key string) map[string]string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	result := map[string]string{}
	switch m := value.(type) {
	case map[string]string:
		return m
	case map[string]any:
		for k, v := range m {
			if s, ok := v.(string); ok {
				result[k] = s
			} else if v != nil {
				result[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func anyMapArg(args map[string]any, key string) map[string]any {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

func formatStringMapInline(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, m[key]))
	}
	return strings.Join(parts, ", ")
}

func sortedAnyMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
