package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterWebhookTools registers all webhook management tools on the server.
func RegisterWebhookTools(s *Server, client *APIClient) {
	registerBrainWebhookCreate(s, client)
	registerBrainWebhookList(s, client)
	registerBrainWebhookGet(s, client)
	registerBrainWebhookUpdate(s, client)
	registerBrainWebhookTest(s, client)
	registerBrainWebhookDeliveries(s, client)
	registerBrainWebhookDelete(s, client)
	registerBrainWebhookToggle(s, client)
}

// =============================================================================
// webhook_create
// =============================================================================

func registerBrainWebhookCreate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_create",
		Description: `Register a new webhook to receive event notifications.

Creates a webhook that will receive HTTP POST callbacks when matching events occur.
Events use a namespaced taxonomy (e.g., "task.completed", "entry.created").
Supports glob patterns like "task.*" to match all task events.

Example:
  webhook_create({ name: "deploy-hook", url: "https://example.com/hook", events: ["task.completed"] })`,
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
			return "", fmt.Errorf("url is required")
		}

		events := StringSliceArg(args, "events")
		if len(events) == 0 {
			return "", fmt.Errorf("events must be a non-empty array of event type strings")
		}

		// Validate URL format
		if _, err := url.ParseRequestURI(webhookURL); err != nil {
			return "", fmt.Errorf("invalid URL %q: %w", webhookURL, err)
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
				return "", errors.New("webhook already exists with that URL. Use webhook_list to see existing webhooks")
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
// webhook_list
// =============================================================================

func registerBrainWebhookList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_list",
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
			return "No webhooks registered. Use webhook_create to add one.", nil
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
// webhook_get
// =============================================================================

func registerBrainWebhookGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_get",
		Description: `Inspect a webhook configuration by ID.

Returns the webhook URL, subscribed events, enabled status, filters, and timestamps.
Use webhook_list to find webhook IDs.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"id": {Type: "string", Description: "Webhook ID to inspect"},
			},
			Required: []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := StringArg(args, "id", "")
		if id == "" {
			return "", fmt.Errorf("id is required")
		}

		var resp types.WebhookResponse
		err := client.Request(ctx, "GET", "/webhooks/"+url.PathEscape(id), nil, nil, &resp)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
				return "", fmt.Errorf("webhook not found: %s. Use webhook_list to see existing webhooks", id)
			}
			return "", err
		}

		return formatWebhookConfig("Webhook configuration", resp), nil
	})
}

// =============================================================================
// webhook_update
// =============================================================================

func registerBrainWebhookUpdate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_update",
		Description: `Update a webhook configuration by ID.

Only provided fields are changed. Supports updating name, URL, events, filter,
secret, and enabled status. Use webhook_get to inspect the result.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"id":      {Type: "string", Description: "Webhook ID to update"},
				"name":    {Type: "string", Description: "New human-readable name"},
				"url":     {Type: "string", Description: "New webhook POST callback URL"},
				"events":  {Type: "array", Items: &Property{Type: "string"}, Description: "Replacement event subscription list"},
				"filter":  {Type: "object", Description: "Replacement key-value filter"},
				"secret":  {Type: "string", Description: "New HMAC secret; empty string clears it"},
				"enabled": {Type: "boolean", Description: "Set true to enable, false to disable"},
			},
			Required: []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := StringArg(args, "id", "")
		if id == "" {
			return "", fmt.Errorf("id is required")
		}

		body := map[string]any{}
		if name, ok := args["name"].(string); ok {
			body["name"] = name
		}
		if webhookURL, ok := args["url"].(string); ok {
			if webhookURL == "" {
				return "", fmt.Errorf("url must not be empty")
			}
			if _, err := url.ParseRequestURI(webhookURL); err != nil {
				return "", fmt.Errorf("invalid URL %q: %w", webhookURL, err)
			}
			body["url"] = webhookURL
		}
		if _, ok := args["events"]; ok {
			events := StringSliceArg(args, "events")
			if len(events) == 0 {
				return "", fmt.Errorf("events must be a non-empty array of event type strings")
			}
			body["events"] = events
		}
		if filterRaw, ok := args["filter"]; ok && filterRaw != nil {
			if filterMap, ok := filterRaw.(map[string]any); ok {
				filter := make(map[string]string, len(filterMap))
				for k, v := range filterMap {
					if s, ok := v.(string); ok {
						filter[k] = s
					}
				}
				body["filter"] = filter
			}
		}
		if secret, ok := args["secret"].(string); ok {
			body["secret"] = secret
		}
		if enabled, ok := args["enabled"].(bool); ok {
			body["enabled"] = enabled
		}
		if len(body) == 0 {
			return "", fmt.Errorf("provide at least one field to update")
		}

		var resp types.WebhookResponse
		err := client.Request(ctx, "PATCH", "/webhooks/"+url.PathEscape(id), body, nil, &resp)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
				return "", fmt.Errorf("webhook not found: %s. Use webhook_list to see existing webhooks", id)
			}
			if strings.Contains(errMsg, "already exists") || strings.Contains(errMsg, "409") {
				return "", errors.New("webhook already exists with that URL. Use webhook_list to see existing webhooks")
			}
			return "", err
		}

		return formatWebhookConfig("Webhook updated successfully", resp), nil
	})
}

// =============================================================================
// webhook_test
// =============================================================================

func registerBrainWebhookTest(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_test",
		Description: `Send a synthetic test event to a webhook by ID.

The API delivers a webhook.test event synchronously and returns the delivery
result, including success, status code, latency, and error details.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"id": {Type: "string", Description: "Webhook ID to test"},
			},
			Required: []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := StringArg(args, "id", "")
		if id == "" {
			return "", fmt.Errorf("id is required")
		}

		var resp types.WebhookDeliveryResponse
		err := client.Request(ctx, "POST", "/webhooks/"+url.PathEscape(id)+"/test", nil, nil, &resp)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
				return "", fmt.Errorf("webhook not found: %s. Use webhook_list to see existing webhooks", id)
			}
			return "", err
		}

		return formatWebhookDelivery("Webhook test delivery result", resp), nil
	})
}

// =============================================================================
// webhook_deliveries
// =============================================================================

func registerBrainWebhookDeliveries(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_deliveries",
		Description: `List recent delivery attempts for a webhook.

Returns delivery IDs, event types, status codes, success state, latency, errors,
and timestamps. Use limit to control how many delivery records are returned.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"id":    {Type: "string", Description: "Webhook ID whose deliveries should be listed"},
				"limit": {Type: "number", Description: "Maximum deliveries to return (default: 50)"},
			},
			Required: []string{"id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		id := StringArg(args, "id", "")
		if id == "" {
			return "", fmt.Errorf("id is required")
		}

		queryParams := map[string]string{}
		if limit := IntArg(args, "limit", 0); limit > 0 {
			queryParams["limit"] = fmt.Sprintf("%d", limit)
		}

		var resp struct {
			Deliveries []types.WebhookDeliveryResponse `json:"deliveries"`
		}
		err := client.Request(ctx, "GET", "/webhooks/"+url.PathEscape(id)+"/deliveries", nil, queryParams, &resp)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
				return "", fmt.Errorf("webhook not found: %s. Use webhook_list to see existing webhooks", id)
			}
			return "", err
		}
		if len(resp.Deliveries) == 0 {
			return fmt.Sprintf("No deliveries recorded for webhook %s.", id), nil
		}

		lines := []string{
			fmt.Sprintf("## Webhook Deliveries for %s (%d)", id, len(resp.Deliveries)),
			"",
		}
		for _, delivery := range resp.Deliveries {
			result := "failure"
			if delivery.Success {
				result = "success"
			}
			lines = append(lines, fmt.Sprintf("- **%s** - %s", delivery.ID, result))
			lines = append(lines, fmt.Sprintf("  Webhook ID: %s", delivery.WebhookID))
			lines = append(lines, fmt.Sprintf("  Event Type: %s", delivery.EventType))
			lines = append(lines, fmt.Sprintf("  Status Code: %s", webhookStatusCode(delivery.StatusCode)))
			lines = append(lines, fmt.Sprintf("  Latency: %s", webhookLatency(delivery.LatencyMs)))
			if delivery.Error != "" {
				lines = append(lines, fmt.Sprintf("  Error: %s", delivery.Error))
			}
			if delivery.CreatedAt != "" {
				lines = append(lines, fmt.Sprintf("  Created: %s", delivery.CreatedAt))
			}
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// webhook_delete
// =============================================================================

func registerBrainWebhookDelete(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_delete",
		Description: `Remove a webhook by ID.

Permanently deletes the webhook registration. Delivery history is also removed.
Use webhook_list to find webhook IDs.`,
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
			return "", fmt.Errorf("id is required")
		}

		err := client.Request(ctx, "DELETE", "/webhooks/"+url.PathEscape(id), nil, nil, nil)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "404") {
				return "", fmt.Errorf("webhook not found: %s. Use webhook_list to see existing webhooks", id)
			}
			return "", err
		}

		return fmt.Sprintf("Webhook %s deleted successfully.", id), nil
	})
}

// =============================================================================
// webhook_toggle
// =============================================================================

func registerBrainWebhookToggle(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "webhook_toggle",
		Description: `Enable or disable a webhook without deleting it.

Toggling a webhook off stops delivery attempts while preserving the configuration.
Use webhook_list to find webhook IDs.`,
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
			return "", fmt.Errorf("id is required")
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
				return "", fmt.Errorf("webhook not found: %s. Use webhook_list to see existing webhooks", id)
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

func formatWebhookConfig(title string, wh types.WebhookResponse) string {
	status := "enabled"
	if !wh.Enabled {
		status = "disabled"
	}
	lines := []string{
		fmt.Sprintf("## %s", title),
		"",
		fmt.Sprintf("- **ID:** %s", wh.ID),
		fmt.Sprintf("- **Name:** %s", wh.Name),
		fmt.Sprintf("- **URL:** %s", wh.URL),
		fmt.Sprintf("- **Status:** %s", status),
		fmt.Sprintf("- **Events:** %s", strings.Join(wh.Events, ", ")),
	}
	if len(wh.Filter) > 0 {
		lines = append(lines, fmt.Sprintf("- **Filter:** %s", formatStringMap(wh.Filter)))
	}
	if wh.CreatedAt != "" {
		lines = append(lines, fmt.Sprintf("- **Created:** %s", wh.CreatedAt))
	}
	if wh.UpdatedAt != "" {
		lines = append(lines, fmt.Sprintf("- **Updated:** %s", wh.UpdatedAt))
	}
	return strings.Join(lines, "\n")
}

func formatWebhookDelivery(title string, delivery types.WebhookDeliveryResponse) string {
	result := "failure"
	if delivery.Success {
		result = "success"
	}
	lines := []string{
		fmt.Sprintf("## %s", title),
		"",
		fmt.Sprintf("- **Delivery ID:** %s", delivery.ID),
		fmt.Sprintf("- **Webhook ID:** %s", delivery.WebhookID),
		fmt.Sprintf("- **Event Type:** %s", delivery.EventType),
		fmt.Sprintf("- **Result:** %s", result),
		fmt.Sprintf("- **Status Code:** %s", webhookStatusCode(delivery.StatusCode)),
		fmt.Sprintf("- **Latency:** %s", webhookLatency(delivery.LatencyMs)),
	}
	if delivery.Error != "" {
		lines = append(lines, fmt.Sprintf("- **Error:** %s", delivery.Error))
	}
	if delivery.CreatedAt != "" {
		lines = append(lines, fmt.Sprintf("- **Created:** %s", delivery.CreatedAt))
	}
	return strings.Join(lines, "\n")
}

func formatStringMap(values map[string]string) string {
	parts := make([]string, 0, len(values))
	for k, v := range values {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func webhookStatusCode(statusCode *int) string {
	if statusCode == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d", *statusCode)
}

func webhookLatency(latencyMs *int) string {
	if latencyMs == nil {
		return "n/a"
	}
	return fmt.Sprintf("%dms", *latencyMs)
}

// webhookToJSON marshals a webhook response to indented JSON for tool output.
func webhookToJSON(wh types.WebhookResponse) string {
	data, err := json.MarshalIndent(wh, "", "  ")
	if err != nil {
		return fmt.Sprintf("Error marshaling webhook: %v", err)
	}
	return string(data)
}
