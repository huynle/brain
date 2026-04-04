package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterWebhookTools registers all 4 webhook management tools on the server.
func RegisterWebhookTools(s *Server, client *APIClient) {
	registerBrainWebhookCreate(s, client)
	registerBrainWebhookList(s, client)
	registerBrainWebhookDelete(s, client)
	registerBrainWebhookToggle(s, client)
}

// =============================================================================
// brain_webhook_create
// =============================================================================

func registerBrainWebhookCreate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_webhook_create",
		Description: `Register a new webhook to receive event notifications.

Creates a webhook that will receive HTTP POST callbacks when matching events occur.
Events use a namespaced taxonomy (e.g., "task.completed", "entry.created").
Supports glob patterns like "task.*" to match all task events.

Example:
  brain_webhook_create({ name: "deploy-hook", url: "https://example.com/hook", events: ["task.completed"] })`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"name":    {Type: "string", Description: "Human-readable name for the webhook"},
				"url":     {Type: "string", Description: "URL to receive webhook POST callbacks"},
				"events":  {Type: "array", Items: &Property{Type: "string"}, Description: "Event types to subscribe to (e.g., [\"task.completed\", \"entry.*\"])"},
				"filter":  {Type: "object", Description: "Optional key-value filter (e.g., {\"project\": \"my-project\"})"},
				"secret":  {Type: "string", Description: "Optional HMAC secret for payload signing (X-Hook-Signature header)"},
				"enabled": {Type: "boolean", Description: "Whether the webhook is active (default: true)"},
			},
			Required: []string{"url", "events"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		webhookURL := StringArg(args, "url", "")
		if webhookURL == "" {
			return "Error: url is required", nil
		}

		events := StringSliceArg(args, "events")
		if len(events) == 0 {
			return "Error: events must be a non-empty array of event type strings", nil
		}

		// Validate URL format
		if _, err := url.ParseRequestURI(webhookURL); err != nil {
			return fmt.Sprintf("Error: invalid URL %q: %v", webhookURL, err), nil
		}

		req := types.CreateWebhookRequest{
			Name:   StringArg(args, "name", ""),
			URL:    webhookURL,
			Events: events,
			Secret: StringArg(args, "secret", ""),
		}

		// Parse filter (map[string]string)
		if filterRaw, ok := args["filter"]; ok && filterRaw != nil {
			if filterMap, ok := filterRaw.(map[string]any); ok {
				req.Filter = make(map[string]string, len(filterMap))
				for k, v := range filterMap {
					if s, ok := v.(string); ok {
						req.Filter[k] = s
					}
				}
			}
		}

		// Parse enabled
		if enabledRaw, ok := args["enabled"]; ok {
			if enabled, ok := enabledRaw.(bool); ok {
				req.Enabled = &enabled
			}
		}

		var resp types.WebhookResponse
		err := client.Request(ctx, "POST", "/webhooks", req, nil, &resp)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "already exists") || strings.Contains(errMsg, "409") {
				return fmt.Sprintf("Webhook already exists with that URL. Use brain_webhook_list to see existing webhooks."), nil
			}
			return "", err
		}

		lines := []string{
			"Webhook created successfully:",
			"",
			fmt.Sprintf("- **ID:** %s", resp.ID),
			fmt.Sprintf("- **Name:** %s", resp.Name),
			fmt.Sprintf("- **URL:** %s", resp.URL),
			fmt.Sprintf("- **Events:** %s", strings.Join(resp.Events, ", ")),
			fmt.Sprintf("- **Enabled:** %v", resp.Enabled),
		}

		if len(resp.Filter) > 0 {
			filterParts := make([]string, 0, len(resp.Filter))
			for k, v := range resp.Filter {
				filterParts = append(filterParts, fmt.Sprintf("%s=%s", k, v))
			}
			lines = append(lines, fmt.Sprintf("- **Filter:** %s", strings.Join(filterParts, ", ")))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_webhook_list
// =============================================================================

func registerBrainWebhookList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_webhook_list",
		Description: `List all registered webhooks.

Returns all webhooks with their IDs, URLs, subscribed events, and status.
Use enabled_only to filter to active webhooks.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"enabled_only": {Type: "boolean", Description: "Only return enabled webhooks (default: false)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		queryParams := map[string]string{}
		if BoolArg(args, "enabled_only", false) {
			queryParams["enabled"] = "true"
		}

		var resp struct {
			Webhooks []types.WebhookResponse `json:"webhooks"`
		}
		if err := client.Request(ctx, "GET", "/webhooks", nil, queryParams, &resp); err != nil {
			return "", err
		}

		if len(resp.Webhooks) == 0 {
			return "No webhooks registered. Use brain_webhook_create to add one.", nil
		}

		lines := []string{
			fmt.Sprintf("## Webhooks (%d)", len(resp.Webhooks)),
			"",
		}

		for _, wh := range resp.Webhooks {
			enabledStr := "enabled"
			if !wh.Enabled {
				enabledStr = "disabled"
			}
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", wh.Name, wh.ID, enabledStr))
			lines = append(lines, fmt.Sprintf("  URL: %s", wh.URL))
			lines = append(lines, fmt.Sprintf("  Events: %s", strings.Join(wh.Events, ", ")))
			if len(wh.Filter) > 0 {
				filterParts := make([]string, 0, len(wh.Filter))
				for k, v := range wh.Filter {
					filterParts = append(filterParts, fmt.Sprintf("%s=%s", k, v))
				}
				lines = append(lines, fmt.Sprintf("  Filter: %s", strings.Join(filterParts, ", ")))
			}
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_webhook_delete
// =============================================================================

func registerBrainWebhookDelete(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_webhook_delete",
		Description: `Remove a webhook by ID.

Permanently deletes the webhook registration. Delivery history is also removed.
Use brain_webhook_list to find webhook IDs.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"id": {Type: "string", Description: "Webhook ID to delete"},
			},
			Required: []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := StringArg(args, "id", "")
		if id == "" {
			return "Error: id is required", nil
		}

		err := client.Request(ctx, "DELETE", "/webhooks/"+url.PathEscape(id), nil, nil, nil)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
				return fmt.Sprintf("Webhook not found: %s. Use brain_webhook_list to see existing webhooks.", id), nil
			}
			return "", err
		}

		return fmt.Sprintf("Webhook %s deleted successfully.", id), nil
	})
}

// =============================================================================
// brain_webhook_toggle
// =============================================================================

func registerBrainWebhookToggle(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_webhook_toggle",
		Description: `Enable or disable a webhook without deleting it.

Toggling a webhook off stops delivery attempts while preserving the configuration.
Use brain_webhook_list to find webhook IDs.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"id":      {Type: "string", Description: "Webhook ID to toggle"},
				"enabled": {Type: "boolean", Description: "Set to true to enable, false to disable"},
			},
			Required: []string{"id", "enabled"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := StringArg(args, "id", "")
		if id == "" {
			return "Error: id is required", nil
		}

		enabled := BoolArg(args, "enabled", true)

		body := map[string]any{
			"enabled": enabled,
		}

		var resp types.WebhookResponse
		err := client.Request(ctx, "PATCH", "/webhooks/"+url.PathEscape(id), body, nil, &resp)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
				return fmt.Sprintf("Webhook not found: %s. Use brain_webhook_list to see existing webhooks.", id), nil
			}
			return "", err
		}

		status := "enabled"
		if !resp.Enabled {
			status = "disabled"
		}

		return fmt.Sprintf("Webhook %s (%s) is now %s.", resp.ID, resp.Name, status), nil
	})
}

// =============================================================================
// Webhook helper for JSON output
// =============================================================================

// webhookToJSON marshals a webhook response to indented JSON for tool output.
func webhookToJSON(wh types.WebhookResponse) string {
	data, err := json.MarshalIndent(wh, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error marshaling webhook: %v", err)
	}
	return string(data)
}
