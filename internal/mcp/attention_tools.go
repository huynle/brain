package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterAttentionTools registers the durable per-user attention inbox tools.
//
// These are distinct from reminders: an attention item is a first-class
// notification with a recipient, workflow links, and a lifecycle. Agents use
// these to get a specific user's attention (a blocked task, a waiting
// question, an arbitrary request) rather than scheduling future work.
func RegisterAttentionTools(s *Server, client *APIClient) {
	registerAttentionCreate(s, client)
	registerAttentionList(s, client)
	registerAttentionGet(s, client)
	registerAttentionResolve(s, client)
	registerAttentionSnooze(s, client)
}

func registerAttentionCreate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "attention_create",
		Description: "Raise a durable notification for a user — a blocked task, a waiting question, " +
			"a permission request, or any situation that needs a human's attention.\n\n" +
			"It appears in the user's Brain attention inbox and bell, survives restarts, and can be " +
			"delivered to the browser. Set a dedup_key so repeated producers do not pile up duplicates. " +
			"Link it to a task/feature/session so the user can act on it directly.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"recipient":   {Type: "string", Description: "Username to notify. Defaults to the calling principal when omitted."},
			"kind":        {Type: "string", Description: "What kind of attention this is (e.g. task_blocked, question, permission)."},
			"severity":    {Type: "string", Enum: []string{"info", "warning", "critical"}, Description: "How urgent it is. Defaults to info."},
			"title":       {Type: "string", Description: "Short headline shown in the bell and inbox."},
			"body":        {Type: "string", Description: "Optional longer markdown detail."},
			"project":     {Type: "string", Description: "Related project."},
			"task_id":     {Type: "string", Description: "Related task id, so the inbox can open/act on it."},
			"feature_id":  {Type: "string", Description: "Related feature id."},
			"session_id":  {Type: "string", Description: "Related OpenCode session id."},
			"runner_id":   {Type: "string", Description: "Related runner id."},
			"instance_id": {Type: "string", Description: "Related runner instance id."},
			"source_type": {Type: "string", Description: "Producer category (e.g. task, permission, checkpoint)."},
			"source_id":   {Type: "string", Description: "Producer's own id for correlation."},
			"dedup_key":   {Type: "string", Description: "Idempotency key per recipient — a repeat with the same key is a no-op."},
		}, Required: []string{"kind", "title"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		title := strings.TrimSpace(StringArg(args, "title", ""))
		kind := strings.TrimSpace(StringArg(args, "kind", ""))
		if title == "" || kind == "" {
			return "", fmt.Errorf("provide both 'kind' and 'title'")
		}
		req := types.CreateAttentionRequest{
			Recipient:  StringArg(args, "recipient", ""),
			Kind:       kind,
			Severity:   StringArg(args, "severity", ""),
			Title:      title,
			Body:       StringArg(args, "body", ""),
			Project:    StringArg(args, "project", ""),
			TaskID:     StringArg(args, "task_id", ""),
			FeatureID:  StringArg(args, "feature_id", ""),
			SessionID:  StringArg(args, "session_id", ""),
			RunnerID:   StringArg(args, "runner_id", ""),
			InstanceID: StringArg(args, "instance_id", ""),
			SourceType: StringArg(args, "source_type", ""),
			SourceID:   StringArg(args, "source_id", ""),
			DedupKey:   StringArg(args, "dedup_key", ""),
		}
		var out types.Attention
		if err := client.Request(ctx, http.MethodPost, "/attention", req, nil, &out); err != nil {
			return "", err
		}
		return formatAttention("Attention created", &out), nil
	})
}

func registerAttentionList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attention_list",
		Description: "List the calling user's attention inbox. Defaults to items that are not snoozed. Filter by state, project, kind, or severity.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"state":    {Type: "string", Enum: []string{"unread", "read", "snoozed", "resolved", "dismissed"}, Description: "Filter to one lifecycle state."},
			"project":  {Type: "string", Description: "Filter to one project."},
			"kind":     {Type: "string", Description: "Filter to one kind."},
			"severity": {Type: "string", Enum: []string{"info", "warning", "critical"}, Description: "Filter to one severity."},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		q := map[string]string{}
		for _, k := range []string{"state", "project", "kind", "severity"} {
			if v := StringArg(args, k, ""); v != "" {
				q[k] = v
			}
		}
		var out struct {
			Attention []types.Attention `json:"attention"`
			Count     int               `json:"count"`
		}
		if err := client.Request(ctx, http.MethodGet, "/attention", nil, q, &out); err != nil {
			return "", err
		}
		if out.Count == 0 {
			return "No attention items.", nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%d attention item(s):\n", out.Count)
		for i := range out.Attention {
			a := &out.Attention[i]
			fmt.Fprintf(&b, "- [%s/%s] %s (%s)", a.Severity, a.State, a.Title, a.ID)
			if a.Project != "" {
				fmt.Fprintf(&b, " project=%s", a.Project)
			}
			if a.TaskID != "" {
				fmt.Fprintf(&b, " task=%s", a.TaskID)
			}
			b.WriteString("\n")
		}
		return b.String(), nil
	})
}

func registerAttentionGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attention_get",
		Description: "Fetch one attention item from the calling user's inbox by id.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"id": {Type: "string", Description: "Attention item id."},
		}, Required: []string{"id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "id", ""))
		if id == "" {
			return "", fmt.Errorf("provide an 'id'")
		}
		var out types.Attention
		if err := client.Request(ctx, http.MethodGet, "/attention/"+url.PathEscape(id), nil, nil, &out); err != nil {
			return "", err
		}
		return formatAttention("Attention", &out), nil
	})
}

func registerAttentionResolve(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attention_resolve",
		Description: "Resolve (close) an attention item once its underlying situation is handled.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"id": {Type: "string", Description: "Attention item id."},
		}, Required: []string{"id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "id", ""))
		if id == "" {
			return "", fmt.Errorf("provide an 'id'")
		}
		var out types.Attention
		if err := client.Request(ctx, http.MethodPost, "/attention/"+url.PathEscape(id)+"/resolve", nil, nil, &out); err != nil {
			return "", err
		}
		return formatAttention("Attention resolved", &out), nil
	})
}

func registerAttentionSnooze(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attention_snooze",
		Description: "Snooze an attention item, hiding it from the default inbox until a later time.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"id":            {Type: "string", Description: "Attention item id."},
			"snoozed_until": {Type: "string", Description: "RFC3339 instant with an offset to re-surface the item."},
		}, Required: []string{"id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := strings.TrimSpace(StringArg(args, "id", ""))
		if id == "" {
			return "", fmt.Errorf("provide an 'id'")
		}
		body := map[string]string{"snoozed_until": StringArg(args, "snoozed_until", "")}
		var out types.Attention
		if err := client.Request(ctx, http.MethodPost, "/attention/"+url.PathEscape(id)+"/snooze", body, nil, &out); err != nil {
			return "", err
		}
		return formatAttention("Attention snoozed", &out), nil
	})
}

func formatAttention(prefix string, a *types.Attention) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", prefix, a.Title)
	fmt.Fprintf(&b, "ID: %s\nRecipient: %s\nKind: %s\nSeverity: %s\nState: %s\n", a.ID, a.Recipient, a.Kind, a.Severity, a.State)
	if a.Project != "" {
		fmt.Fprintf(&b, "Project: %s\n", a.Project)
	}
	if a.TaskID != "" {
		fmt.Fprintf(&b, "Task: %s\n", a.TaskID)
	}
	if a.Body != "" {
		fmt.Fprintf(&b, "\n%s\n", a.Body)
	}
	return b.String()
}
