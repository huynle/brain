package storage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// CreateWebhook
// ---------------------------------------------------------------------------

func TestCreateWebhook_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		Name:    "test-hook",
		URL:     "https://example.com/hook",
		Events:  []string{"task.completed", "task.failed"},
		Filter:  map[string]interface{}{"project": "brain-api"},
		Secret:  "s3cret",
		Enabled: true,
	}

	err := s.CreateWebhook(ctx, wh)
	if err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	// ID should be auto-generated
	if wh.ID == "" {
		t.Fatal("expected auto-generated ID, got empty")
	}
	if wh.CreatedAt == "" {
		t.Fatal("expected auto-set CreatedAt, got empty")
	}
	if wh.UpdatedAt == "" {
		t.Fatal("expected auto-set UpdatedAt, got empty")
	}
}

func TestCreateWebhook_CustomID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "custom-id-123",
		Name:    "custom-hook",
		URL:     "https://example.com/hook",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}

	err := s.CreateWebhook(ctx, wh)
	if err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}
	if wh.ID != "custom-id-123" {
		t.Errorf("ID = %q, want %q", wh.ID, "custom-id-123")
	}
}

func TestCreateWebhook_DuplicateID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh1 := &Webhook{
		ID:      "dup-id",
		Name:    "hook1",
		URL:     "https://example.com/hook1",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	wh2 := &Webhook{
		ID:      "dup-id",
		Name:    "hook2",
		URL:     "https://example.com/hook2",
		Events:  []string{"task.failed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}

	if err := s.CreateWebhook(ctx, wh1); err != nil {
		t.Fatalf("CreateWebhook (1) failed: %v", err)
	}
	err := s.CreateWebhook(ctx, wh2)
	if err == nil {
		t.Fatal("expected error for duplicate ID, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetWebhook
// ---------------------------------------------------------------------------

func TestGetWebhook_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "get-test",
		Name:    "test-hook",
		URL:     "https://example.com/hook",
		Events:  []string{"task.completed", "task.failed"},
		Filter:  map[string]interface{}{"project": "brain-api"},
		Secret:  "s3cret",
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	got, err := s.GetWebhook(ctx, "get-test")
	if err != nil {
		t.Fatalf("GetWebhook failed: %v", err)
	}

	if got.ID != "get-test" {
		t.Errorf("ID = %q, want %q", got.ID, "get-test")
	}
	if got.Name != "test-hook" {
		t.Errorf("Name = %q, want %q", got.Name, "test-hook")
	}
	if got.URL != "https://example.com/hook" {
		t.Errorf("URL = %q, want %q", got.URL, "https://example.com/hook")
	}
	if len(got.Events) != 2 {
		t.Errorf("Events len = %d, want 2", len(got.Events))
	}
	if got.Events[0] != "task.completed" {
		t.Errorf("Events[0] = %q, want %q", got.Events[0], "task.completed")
	}
	if got.Filter["project"] != "brain-api" {
		t.Errorf("Filter[project] = %v, want %q", got.Filter["project"], "brain-api")
	}
	if got.Secret != "s3cret" {
		t.Errorf("Secret = %q, want %q", got.Secret, "s3cret")
	}
	if !got.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestGetWebhook_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetWebhook(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent webhook, got nil")
	}
	if got != nil {
		t.Errorf("expected nil webhook, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// ListWebhooks
// ---------------------------------------------------------------------------

func TestListWebhooks_All(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	for i, name := range []string{"hook1", "hook2", "hook3"} {
		enabled := i != 2 // hook3 is disabled
		wh := &Webhook{
			Name:    name,
			URL:     "https://example.com/" + name,
			Events:  []string{"task.completed"},
			Filter:  map[string]interface{}{},
			Enabled: enabled,
		}
		if err := s.CreateWebhook(ctx, wh); err != nil {
			t.Fatalf("CreateWebhook(%s) failed: %v", name, err)
		}
	}

	webhooks, err := s.ListWebhooks(ctx)
	if err != nil {
		t.Fatalf("ListWebhooks failed: %v", err)
	}
	if len(webhooks) != 3 {
		t.Fatalf("got %d webhooks, want 3", len(webhooks))
	}
}

func TestListWebhooks_EnabledOnly(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh1 := &Webhook{
		Name: "enabled-hook", URL: "https://example.com/1",
		Events: []string{"task.completed"}, Filter: map[string]interface{}{}, Enabled: true,
	}
	wh2 := &Webhook{
		Name: "disabled-hook", URL: "https://example.com/2",
		Events: []string{"task.completed"}, Filter: map[string]interface{}{}, Enabled: false,
	}
	if err := s.CreateWebhook(ctx, wh1); err != nil {
		t.Fatalf("CreateWebhook(1) failed: %v", err)
	}
	if err := s.CreateWebhook(ctx, wh2); err != nil {
		t.Fatalf("CreateWebhook(2) failed: %v", err)
	}

	webhooks, err := s.ListWebhooks(ctx, true)
	if err != nil {
		t.Fatalf("ListWebhooks(enabledOnly) failed: %v", err)
	}
	if len(webhooks) != 1 {
		t.Fatalf("got %d webhooks, want 1 (enabled only)", len(webhooks))
	}
	if webhooks[0].Name != "enabled-hook" {
		t.Errorf("Name = %q, want %q", webhooks[0].Name, "enabled-hook")
	}
}

func TestListWebhooks_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	webhooks, err := s.ListWebhooks(ctx)
	if err != nil {
		t.Fatalf("ListWebhooks failed: %v", err)
	}
	if webhooks == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(webhooks) != 0 {
		t.Errorf("got %d webhooks, want 0", len(webhooks))
	}
}

// ---------------------------------------------------------------------------
// UpdateWebhook
// ---------------------------------------------------------------------------

func TestUpdateWebhook_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "update-test",
		Name:    "original-name",
		URL:     "https://example.com/original",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Secret:  "old-secret",
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	// Update fields
	wh.Name = "updated-name"
	wh.URL = "https://example.com/updated"
	wh.Events = []string{"task.completed", "task.failed"}
	wh.Filter = map[string]interface{}{"project": "new-project"}
	wh.Secret = "new-secret"
	wh.Enabled = false

	if err := s.UpdateWebhook(ctx, wh); err != nil {
		t.Fatalf("UpdateWebhook failed: %v", err)
	}

	// Verify
	got, err := s.GetWebhook(ctx, "update-test")
	if err != nil {
		t.Fatalf("GetWebhook after update failed: %v", err)
	}
	if got.Name != "updated-name" {
		t.Errorf("Name = %q, want %q", got.Name, "updated-name")
	}
	if got.URL != "https://example.com/updated" {
		t.Errorf("URL = %q, want %q", got.URL, "https://example.com/updated")
	}
	if len(got.Events) != 2 {
		t.Errorf("Events len = %d, want 2", len(got.Events))
	}
	if got.Filter["project"] != "new-project" {
		t.Errorf("Filter[project] = %v, want %q", got.Filter["project"], "new-project")
	}
	if got.Secret != "new-secret" {
		t.Errorf("Secret = %q, want %q", got.Secret, "new-secret")
	}
	if got.Enabled {
		t.Error("Enabled should be false after update")
	}
	if got.UpdatedAt == "" {
		t.Error("UpdatedAt should not be empty after update")
	}
}

func TestUpdateWebhook_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:     "nonexistent",
		Name:   "x",
		URL:    "https://example.com",
		Events: []string{},
		Filter: map[string]interface{}{},
	}
	err := s.UpdateWebhook(ctx, wh)
	if err == nil {
		t.Fatal("expected error for nonexistent webhook, got nil")
	}
}

// ---------------------------------------------------------------------------
// DeleteWebhook
// ---------------------------------------------------------------------------

func TestDeleteWebhook_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "delete-me",
		Name:    "doomed-hook",
		URL:     "https://example.com/delete",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	err := s.DeleteWebhook(ctx, "delete-me")
	if err != nil {
		t.Fatalf("DeleteWebhook failed: %v", err)
	}

	// Should be gone
	_, err = s.GetWebhook(ctx, "delete-me")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestDeleteWebhook_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.DeleteWebhook(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent webhook, got nil")
	}
}

func TestDeleteWebhook_CascadesDeliveries(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "cascade-test",
		Name:    "cascade-hook",
		URL:     "https://example.com/cascade",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	// Create deliveries
	statusCode := 200
	latencyMs := 50
	d := &WebhookDelivery{
		WebhookID:  "cascade-test",
		EventType:  "task.completed",
		StatusCode: &statusCode,
		Success:    true,
		LatencyMs:  &latencyMs,
	}
	if err := s.CreateDelivery(ctx, d); err != nil {
		t.Fatalf("CreateDelivery failed: %v", err)
	}

	// Delete webhook should cascade to deliveries
	if err := s.DeleteWebhook(ctx, "cascade-test"); err != nil {
		t.Fatalf("DeleteWebhook failed: %v", err)
	}

	// Deliveries should be gone
	deliveries, err := s.ListDeliveries(ctx, "cascade-test", 10)
	if err != nil {
		t.Fatalf("ListDeliveries after cascade failed: %v", err)
	}
	if len(deliveries) != 0 {
		t.Errorf("got %d deliveries after cascade, want 0", len(deliveries))
	}
}

// ---------------------------------------------------------------------------
// CreateDelivery
// ---------------------------------------------------------------------------

func TestCreateDelivery_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create parent webhook first
	wh := &Webhook{
		ID:      "delivery-parent",
		Name:    "parent-hook",
		URL:     "https://example.com/parent",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	statusCode := 200
	latencyMs := 42
	d := &WebhookDelivery{
		WebhookID:  "delivery-parent",
		EventType:  "task.completed",
		StatusCode: &statusCode,
		Success:    true,
		LatencyMs:  &latencyMs,
	}

	err := s.CreateDelivery(ctx, d)
	if err != nil {
		t.Fatalf("CreateDelivery failed: %v", err)
	}

	if d.ID == "" {
		t.Fatal("expected auto-generated ID, got empty")
	}
	if d.CreatedAt == "" {
		t.Fatal("expected auto-set CreatedAt, got empty")
	}
}

func TestCreateDelivery_WithError(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "error-parent",
		Name:    "error-hook",
		URL:     "https://example.com/error",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	d := &WebhookDelivery{
		WebhookID: "error-parent",
		EventType: "task.completed",
		Success:   false,
		Error:     "connection timeout",
	}

	err := s.CreateDelivery(ctx, d)
	if err != nil {
		t.Fatalf("CreateDelivery failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ListDeliveries
// ---------------------------------------------------------------------------

func TestListDeliveries_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "list-del-parent",
		Name:    "list-del-hook",
		URL:     "https://example.com/list",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	// Create multiple deliveries
	statusCode := 200
	latencyMs := 10
	for i := 0; i < 3; i++ {
		d := &WebhookDelivery{
			WebhookID:  "list-del-parent",
			EventType:  "task.completed",
			StatusCode: &statusCode,
			Success:    true,
			LatencyMs:  &latencyMs,
		}
		if err := s.CreateDelivery(ctx, d); err != nil {
			t.Fatalf("CreateDelivery %d failed: %v", i, err)
		}
	}

	deliveries, err := s.ListDeliveries(ctx, "list-del-parent", 10)
	if err != nil {
		t.Fatalf("ListDeliveries failed: %v", err)
	}
	if len(deliveries) != 3 {
		t.Fatalf("got %d deliveries, want 3", len(deliveries))
	}

	// Check fields parsed correctly
	d := deliveries[0]
	if d.WebhookID != "list-del-parent" {
		t.Errorf("WebhookID = %q, want %q", d.WebhookID, "list-del-parent")
	}
	if d.EventType != "task.completed" {
		t.Errorf("EventType = %q, want %q", d.EventType, "task.completed")
	}
	if !d.Success {
		t.Error("Success should be true")
	}
	if d.StatusCode == nil || *d.StatusCode != 200 {
		t.Errorf("StatusCode = %v, want 200", d.StatusCode)
	}
	if d.LatencyMs == nil || *d.LatencyMs != 10 {
		t.Errorf("LatencyMs = %v, want 10", d.LatencyMs)
	}
}

func TestListDeliveries_Empty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	deliveries, err := s.ListDeliveries(ctx, "nonexistent", 10)
	if err != nil {
		t.Fatalf("ListDeliveries failed: %v", err)
	}
	if deliveries == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(deliveries) != 0 {
		t.Errorf("got %d deliveries, want 0", len(deliveries))
	}
}

func TestListDeliveries_RespectsLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "limit-parent",
		Name:    "limit-hook",
		URL:     "https://example.com/limit",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	statusCode := 200
	for i := 0; i < 5; i++ {
		d := &WebhookDelivery{
			WebhookID:  "limit-parent",
			EventType:  "task.completed",
			StatusCode: &statusCode,
			Success:    true,
		}
		if err := s.CreateDelivery(ctx, d); err != nil {
			t.Fatalf("CreateDelivery %d failed: %v", i, err)
		}
	}

	deliveries, err := s.ListDeliveries(ctx, "limit-parent", 2)
	if err != nil {
		t.Fatalf("ListDeliveries failed: %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("got %d deliveries, want 2 (limit)", len(deliveries))
	}
}

func TestListDeliveries_NullableFields(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:      "nullable-parent",
		Name:    "nullable-hook",
		URL:     "https://example.com/nullable",
		Events:  []string{"task.completed"},
		Filter:  map[string]interface{}{},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	// Create delivery with nil status_code and latency_ms
	d := &WebhookDelivery{
		WebhookID: "nullable-parent",
		EventType: "task.completed",
		Success:   false,
		Error:     "connection refused",
	}
	if err := s.CreateDelivery(ctx, d); err != nil {
		t.Fatalf("CreateDelivery failed: %v", err)
	}

	deliveries, err := s.ListDeliveries(ctx, "nullable-parent", 10)
	if err != nil {
		t.Fatalf("ListDeliveries failed: %v", err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("got %d deliveries, want 1", len(deliveries))
	}

	got := deliveries[0]
	if got.StatusCode != nil {
		t.Errorf("StatusCode should be nil, got %v", got.StatusCode)
	}
	if got.LatencyMs != nil {
		t.Errorf("LatencyMs should be nil, got %v", got.LatencyMs)
	}
	if !got.Success == true {
		// Success should be false
	}
	if got.Success {
		t.Error("Success should be false")
	}
	if got.Error != "connection refused" {
		t.Errorf("Error = %q, want %q", got.Error, "connection refused")
	}
}

// ---------------------------------------------------------------------------
// Schema: tables exist after init
// ---------------------------------------------------------------------------

func TestSchemaCreation_WebhookTablesExist(t *testing.T) {
	s := newTestStorage(t)

	tables := []string{"webhooks", "webhook_deliveries"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
			).Scan(&name)
			if err != nil {
				t.Fatalf("table %q not found: %v", table, err)
			}
		})
	}
}

func TestSchemaCreation_WebhookIndexesExist(t *testing.T) {
	s := newTestStorage(t)

	indexes := []string{
		"idx_webhooks_enabled",
		"idx_webhook_deliveries_webhook",
		"idx_webhook_deliveries_created",
	}
	for _, idx := range indexes {
		t.Run(idx, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
			).Scan(&name)
			if err != nil {
				t.Fatalf("index %q not found: %v", idx, err)
			}
		})
	}
}

func TestSchemaVersion_IsFive(t *testing.T) {
	s := newTestStorage(t)

	ver, err := GetSchemaVersion(s.DB())
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}
	if ver != 5 {
		t.Errorf("schema version = %d, want 5", ver)
	}
}

// ---------------------------------------------------------------------------
// JSON round-trip for events and filter
// ---------------------------------------------------------------------------

func TestWebhook_JSONRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	wh := &Webhook{
		ID:   "json-roundtrip",
		Name: "json-hook",
		URL:  "https://example.com/json",
		Events: []string{
			"task.completed",
			"task.failed",
			"task.*",
		},
		Filter: map[string]interface{}{
			"project":  "brain-api",
			"priority": "high",
		},
		Enabled: true,
	}
	if err := s.CreateWebhook(ctx, wh); err != nil {
		t.Fatalf("CreateWebhook failed: %v", err)
	}

	got, err := s.GetWebhook(ctx, "json-roundtrip")
	if err != nil {
		t.Fatalf("GetWebhook failed: %v", err)
	}

	// Events
	if len(got.Events) != 3 {
		t.Fatalf("Events len = %d, want 3", len(got.Events))
	}
	expectedEvents := []string{"task.completed", "task.failed", "task.*"}
	for i, e := range expectedEvents {
		if got.Events[i] != e {
			t.Errorf("Events[%d] = %q, want %q", i, got.Events[i], e)
		}
	}

	// Filter
	if got.Filter["project"] != "brain-api" {
		t.Errorf("Filter[project] = %v, want %q", got.Filter["project"], "brain-api")
	}
	if got.Filter["priority"] != "high" {
		t.Errorf("Filter[priority] = %v, want %q", got.Filter["priority"], "high")
	}
}
