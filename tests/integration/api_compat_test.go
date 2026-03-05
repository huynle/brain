// Package integration provides end-to-end API compatibility tests for the Go
// rewrite of brain-api. These tests stand up a real HTTP server with in-memory
// SQLite and exercise every core endpoint, verifying response shapes match the
// TypeScript implementation.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/glebarez/go-sqlite"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// ---------------------------------------------------------------------------
// Test server setup
// ---------------------------------------------------------------------------

// testEnv bundles everything needed for an integration test.
type testEnv struct {
	server  *httptest.Server
	baseURL string
	hub     *realtime.Hub
	t       *testing.T
}

// newTestServer creates a fully-wired test server with in-memory SQLite.
func newTestServer(t *testing.T) *testEnv {
	t.Helper()

	// 1. In-memory SQLite
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}

	// 2. Config with temp brain dir
	brainDir := t.TempDir()
	cfg := &config.Config{
		BrainDir:   brainDir,
		Port:       0,
		Host:       "127.0.0.1",
		EnableAuth: false,
		CORSOrigin: "*",
		LogLevel:   "error",
	}

	// 3. Indexer, services, hub
	idx := indexer.NewIndexer(brainDir, store)
	brainSvc := service.NewBrainService(cfg, store, idx)
	taskSvc := service.NewTaskService(cfg, store)
	hub := realtime.NewHub()

	// 4. Handler + Router
	handler := api.NewHandler(brainSvc,
		api.WithTaskService(taskSvc),
		api.WithHub(hub),
	)
	router := api.NewRouter(*cfg, api.WithHandler(handler))

	// 5. httptest server
	srv := httptest.NewServer(router)
	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})

	return &testEnv{
		server:  srv,
		baseURL: srv.URL + "/api/v1",
		hub:     hub,
		t:       t,
	}
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

func (e *testEnv) get(path string) *http.Response {
	e.t.Helper()
	resp, err := http.Get(e.baseURL + path)
	if err != nil {
		e.t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func (e *testEnv) post(path string, body interface{}) *http.Response {
	e.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		e.t.Fatalf("marshal body: %v", err)
	}
	resp, err := http.Post(e.baseURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		e.t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

func (e *testEnv) patch(path string, body interface{}) *http.Response {
	e.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		e.t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPatch, e.baseURL+path, bytes.NewReader(b))
	if err != nil {
		e.t.Fatalf("new PATCH request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("PATCH %s: %v", path, err)
	}
	return resp
}

func (e *testEnv) delete(path string) *http.Response {
	e.t.Helper()
	req, err := http.NewRequest(http.MethodDelete, e.baseURL+path, nil)
	if err != nil {
		e.t.Fatalf("new DELETE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

func (e *testEnv) put(path string, body interface{}) *http.Response {
	e.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		e.t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, e.baseURL+path, bytes.NewReader(b))
	if err != nil {
		e.t.Fatalf("new PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("PUT %s: %v", path, err)
	}
	return resp
}

// decodeJSON reads and decodes a JSON response body into v.
func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(body, v); err != nil {
		t.Fatalf("decode JSON: %v\nbody: %s", err, string(body))
	}
}

// expectStatus asserts the HTTP status code.
func expectStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, string(body))
	}
}

// createEntry is a convenience helper that creates an entry and returns its ID.
func (e *testEnv) createEntry(entryType, title, content string) string {
	e.t.Helper()
	resp := e.post("/entries", types.CreateEntryRequest{
		Type:    entryType,
		Title:   title,
		Content: content,
	})
	expectStatus(e.t, resp, http.StatusCreated)
	var created types.CreateEntryResponse
	decodeJSON(e.t, resp, &created)
	if created.ID == "" {
		e.t.Fatal("created entry has empty ID")
	}
	return created.ID
}

// =============================================================================
// Health
// =============================================================================

func TestHealth(t *testing.T) {
	env := newTestServer(t)
	resp := env.get("/health")
	expectStatus(t, resp, http.StatusOK)

	var health types.HealthResponse
	decodeJSON(t, resp, &health)

	if health.Status != "healthy" {
		t.Errorf("health.Status = %q, want %q", health.Status, "healthy")
	}
	if health.Timestamp == "" {
		t.Error("health.Timestamp is empty")
	}
	// Verify timestamp is valid RFC3339
	if _, err := time.Parse(time.RFC3339, health.Timestamp); err != nil {
		t.Errorf("health.Timestamp %q is not valid RFC3339: %v", health.Timestamp, err)
	}
}

// =============================================================================
// Entry CRUD
// =============================================================================

func TestCreateEntry(t *testing.T) {
	env := newTestServer(t)

	resp := env.post("/entries", types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Test Plan",
		Content: "This is a test plan",
		Tags:    []string{"test", "integration"},
	})
	expectStatus(t, resp, http.StatusCreated)

	var created types.CreateEntryResponse
	decodeJSON(t, resp, &created)

	// Verify response shape
	if created.ID == "" {
		t.Error("ID is empty")
	}
	if len(created.ID) != 8 {
		t.Errorf("ID length = %d, want 8", len(created.ID))
	}
	if created.Path == "" {
		t.Error("Path is empty")
	}
	if created.Title != "Test Plan" {
		t.Errorf("Title = %q, want %q", created.Title, "Test Plan")
	}
	if created.Type != "plan" {
		t.Errorf("Type = %q, want %q", created.Type, "plan")
	}
	if created.Status != "active" {
		t.Errorf("Status = %q, want %q (default)", created.Status, "active")
	}
}

func TestGetEntry(t *testing.T) {
	env := newTestServer(t)
	id := env.createEntry("plan", "Get Test", "Content for get test")

	resp := env.get("/entries/" + id)
	expectStatus(t, resp, http.StatusOK)

	var entry types.BrainEntry
	decodeJSON(t, resp, &entry)

	if entry.ID != id {
		t.Errorf("ID = %q, want %q", entry.ID, id)
	}
	if entry.Title != "Get Test" {
		t.Errorf("Title = %q, want %q", entry.Title, "Get Test")
	}
	if entry.Type != "plan" {
		t.Errorf("Type = %q, want %q", entry.Type, "plan")
	}
	if entry.Content == "" {
		t.Error("Content is empty")
	}
	if entry.Created == "" {
		t.Error("Created is empty")
	}
	if entry.Modified == "" {
		t.Error("Modified is empty")
	}
}

func TestGetEntry_NotFound(t *testing.T) {
	env := newTestServer(t)
	resp := env.get("/entries/nonexist")
	expectStatus(t, resp, http.StatusNotFound)

	var errResp types.ErrorResponse
	decodeJSON(t, resp, &errResp)
	if errResp.Error != "Not Found" {
		t.Errorf("error = %q, want %q", errResp.Error, "Not Found")
	}
}

func TestUpdateEntry(t *testing.T) {
	env := newTestServer(t)
	id := env.createEntry("plan", "Update Test", "Original content")

	newStatus := "completed"
	resp := env.patch("/entries/"+id, types.UpdateEntryRequest{
		Status: &newStatus,
	})
	expectStatus(t, resp, http.StatusOK)

	var updated types.BrainEntry
	decodeJSON(t, resp, &updated)

	if updated.Status != "completed" {
		t.Errorf("Status = %q, want %q", updated.Status, "completed")
	}
}

func TestUpdateEntry_Append(t *testing.T) {
	env := newTestServer(t)
	id := env.createEntry("plan", "Append Test", "Original content")

	appendText := "\n\n## Added Section\nNew content"
	resp := env.patch("/entries/"+id, types.UpdateEntryRequest{
		Append: &appendText,
	})
	expectStatus(t, resp, http.StatusOK)

	var updated types.BrainEntry
	decodeJSON(t, resp, &updated)

	if !strings.Contains(updated.Content, "Added Section") {
		t.Error("Content does not contain appended text")
	}
	if !strings.Contains(updated.Content, "Original content") {
		t.Error("Content lost original text after append")
	}
}

func TestDeleteEntry(t *testing.T) {
	env := newTestServer(t)
	id := env.createEntry("plan", "Delete Test", "To be deleted")

	// Delete without confirm should fail
	resp := env.delete("/entries/" + id)
	expectStatus(t, resp, http.StatusBadRequest)

	// Delete with confirm should succeed
	resp = env.delete("/entries/" + id + "?confirm=true")
	expectStatus(t, resp, http.StatusNoContent)

	// Verify it's gone
	resp = env.get("/entries/" + id)
	expectStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Create Entry Validation
// =============================================================================

func TestCreateEntry_ValidationErrors(t *testing.T) {
	env := newTestServer(t)

	tests := []struct {
		name    string
		body    types.CreateEntryRequest
		wantErr string
	}{
		{
			name:    "missing type",
			body:    types.CreateEntryRequest{Title: "T", Content: "C"},
			wantErr: "type",
		},
		{
			name:    "missing title",
			body:    types.CreateEntryRequest{Type: "plan", Content: "C"},
			wantErr: "title",
		},
		{
			name:    "missing content",
			body:    types.CreateEntryRequest{Type: "plan", Title: "T"},
			wantErr: "content",
		},
		{
			name:    "invalid type",
			body:    types.CreateEntryRequest{Type: "bogus", Title: "T", Content: "C"},
			wantErr: "type",
		},
		{
			name:    "invalid status",
			body:    types.CreateEntryRequest{Type: "plan", Title: "T", Content: "C", Status: "bogus"},
			wantErr: "status",
		},
		{
			name:    "invalid priority",
			body:    types.CreateEntryRequest{Type: "plan", Title: "T", Content: "C", Priority: "bogus"},
			wantErr: "priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := env.post("/entries", tt.body)
			expectStatus(t, resp, http.StatusBadRequest)

			var errResp types.ErrorResponse
			decodeJSON(t, resp, &errResp)

			if errResp.Error == "" {
				t.Error("error field is empty")
			}

			// Check that the expected field is mentioned in details
			found := false
			for _, d := range errResp.Details {
				if d.Field == tt.wantErr {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected validation detail for field %q, got details: %+v", tt.wantErr, errResp.Details)
			}
		})
	}
}

func TestCreateEntry_InvalidJSON(t *testing.T) {
	env := newTestServer(t)
	resp, err := http.Post(env.baseURL+"/entries", "application/json", strings.NewReader("{invalid"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	expectStatus(t, resp, http.StatusBadRequest)
	resp.Body.Close()
}

// =============================================================================
// All Entry Types
// =============================================================================

func TestCreateEntry_AllTypes(t *testing.T) {
	env := newTestServer(t)

	for _, entryType := range types.EntryTypes {
		t.Run(entryType, func(t *testing.T) {
			resp := env.post("/entries", types.CreateEntryRequest{
				Type:    entryType,
				Title:   fmt.Sprintf("Test %s", entryType),
				Content: fmt.Sprintf("Content for %s", entryType),
			})
			expectStatus(t, resp, http.StatusCreated)

			var created types.CreateEntryResponse
			decodeJSON(t, resp, &created)

			if created.Type != entryType {
				t.Errorf("Type = %q, want %q", created.Type, entryType)
			}
		})
	}
}

// =============================================================================
// List Entries
// =============================================================================

func TestListEntries(t *testing.T) {
	env := newTestServer(t)

	// Create a few entries
	env.createEntry("plan", "Plan A", "Content A")
	env.createEntry("task", "Task B", "Content B")
	env.createEntry("plan", "Plan C", "Content C")

	resp := env.get("/entries")
	expectStatus(t, resp, http.StatusOK)

	var list types.ListEntriesResponse
	decodeJSON(t, resp, &list)

	if list.Total < 3 {
		t.Errorf("Total = %d, want >= 3", list.Total)
	}
	if len(list.Entries) < 3 {
		t.Errorf("len(Entries) = %d, want >= 3", len(list.Entries))
	}
}

func TestListEntries_FilterByType(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Plan X", "Content")
	env.createEntry("task", "Task Y", "Content")
	env.createEntry("plan", "Plan Z", "Content")

	resp := env.get("/entries?type=plan")
	expectStatus(t, resp, http.StatusOK)

	var list types.ListEntriesResponse
	decodeJSON(t, resp, &list)

	for _, e := range list.Entries {
		if e.Type != "plan" {
			t.Errorf("entry %q has type %q, want %q", e.ID, e.Type, "plan")
		}
	}
	if list.Total < 2 {
		t.Errorf("Total = %d, want >= 2 plans", list.Total)
	}
}

func TestListEntries_FilterByStatus(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Status Test", "Content")
	completed := "completed"
	env.patch("/entries/"+id, types.UpdateEntryRequest{Status: &completed})

	resp := env.get("/entries?status=completed")
	expectStatus(t, resp, http.StatusOK)

	var list types.ListEntriesResponse
	decodeJSON(t, resp, &list)

	for _, e := range list.Entries {
		if e.Status != "completed" {
			t.Errorf("entry %q has status %q, want %q", e.ID, e.Status, "completed")
		}
	}
}

func TestListEntries_Pagination(t *testing.T) {
	env := newTestServer(t)

	// Create 5 entries
	for i := 0; i < 5; i++ {
		env.createEntry("plan", fmt.Sprintf("Page Test %d", i), "Content")
	}

	resp := env.get("/entries?limit=2&offset=0")
	expectStatus(t, resp, http.StatusOK)

	var page1 types.ListEntriesResponse
	decodeJSON(t, resp, &page1)

	if len(page1.Entries) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page1.Entries))
	}
	if page1.Limit != 2 {
		t.Errorf("page1.Limit = %d, want 2", page1.Limit)
	}
	if page1.Offset != 0 {
		t.Errorf("page1.Offset = %d, want 0", page1.Offset)
	}

	resp = env.get("/entries?limit=2&offset=2")
	expectStatus(t, resp, http.StatusOK)

	var page2 types.ListEntriesResponse
	decodeJSON(t, resp, &page2)

	if len(page2.Entries) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2.Entries))
	}
	if page2.Offset != 2 {
		t.Errorf("page2.Offset = %d, want 2", page2.Offset)
	}

	// Entries should be different between pages
	if len(page1.Entries) > 0 && len(page2.Entries) > 0 {
		if page1.Entries[0].ID == page2.Entries[0].ID {
			t.Error("page1 and page2 returned the same first entry")
		}
	}
}

func TestListEntries_SortBy(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Sort A", "Content")
	time.Sleep(10 * time.Millisecond) // ensure different timestamps
	env.createEntry("plan", "Sort B", "Content")

	resp := env.get("/entries?sortBy=modified")
	expectStatus(t, resp, http.StatusOK)

	var list types.ListEntriesResponse
	decodeJSON(t, resp, &list)

	if len(list.Entries) < 2 {
		t.Fatalf("expected >= 2 entries, got %d", len(list.Entries))
	}
}

// =============================================================================
// Search
// =============================================================================

func TestSearch(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Go Migration Plan", "Migrate the codebase to Go")
	env.createEntry("task", "TypeScript Cleanup", "Remove old TypeScript code")

	tests := []struct {
		name      string
		query     string
		wantMin   int
		wantTitle string
	}{
		{
			name:      "search by title keyword",
			query:     "Migration",
			wantMin:   1,
			wantTitle: "Go Migration Plan",
		},
		{
			name:    "search by body keyword",
			query:   "TypeScript",
			wantMin: 1,
		},
		{
			name:    "no results",
			query:   "xyznonexistent",
			wantMin: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := env.post("/search", types.SearchRequest{Query: tt.query})
			expectStatus(t, resp, http.StatusOK)

			var result types.SearchResponse
			decodeJSON(t, resp, &result)

			if result.Total < tt.wantMin {
				t.Errorf("Total = %d, want >= %d", result.Total, tt.wantMin)
			}
			if len(result.Results) < tt.wantMin {
				t.Errorf("len(Results) = %d, want >= %d", len(result.Results), tt.wantMin)
			}

			// Verify result shape
			for _, r := range result.Results {
				if r.ID == "" {
					t.Error("result has empty ID")
				}
				if r.Path == "" {
					t.Error("result has empty Path")
				}
				if r.Title == "" {
					t.Error("result has empty Title")
				}
				if r.Type == "" {
					t.Error("result has empty Type")
				}
			}

			if tt.wantTitle != "" && len(result.Results) > 0 {
				if result.Results[0].Title != tt.wantTitle {
					t.Errorf("first result title = %q, want %q", result.Results[0].Title, tt.wantTitle)
				}
			}
		})
	}
}

func TestSearch_WithTypeFilter(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Search Plan", "Searchable plan content")
	env.createEntry("task", "Search Task", "Searchable task content")

	resp := env.post("/search", types.SearchRequest{
		Query: "Searchable",
		Type:  "plan",
	})
	expectStatus(t, resp, http.StatusOK)

	var result types.SearchResponse
	decodeJSON(t, resp, &result)

	for _, r := range result.Results {
		if r.Type != "plan" {
			t.Errorf("result type = %q, want %q", r.Type, "plan")
		}
	}
}

func TestSearch_WithLimit(t *testing.T) {
	env := newTestServer(t)

	for i := 0; i < 5; i++ {
		env.createEntry("plan", fmt.Sprintf("Limit Test %d", i), "Searchable content for limit test")
	}

	limit := 2
	resp := env.post("/search", types.SearchRequest{
		Query: "Searchable",
		Limit: &limit,
	})
	expectStatus(t, resp, http.StatusOK)

	var result types.SearchResponse
	decodeJSON(t, resp, &result)

	if len(result.Results) > 2 {
		t.Errorf("len(Results) = %d, want <= 2", len(result.Results))
	}
}

func TestSearch_EmptyQuery(t *testing.T) {
	env := newTestServer(t)

	// Go server validates that query is required and returns 400
	resp := env.post("/search", types.SearchRequest{Query: ""})
	expectStatus(t, resp, http.StatusBadRequest)

	var errResp types.ErrorResponse
	decodeJSON(t, resp, &errResp)

	if errResp.Error == "" {
		t.Error("error field is empty")
	}
	// Should mention query field in validation details
	found := false
	for _, d := range errResp.Details {
		if d.Field == "query" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected validation detail for field %q, got details: %+v", "query", errResp.Details)
	}
}

// =============================================================================
// Inject
// =============================================================================

func TestInject(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Inject Plan", "Content about Go migration strategy")

	resp := env.post("/inject", types.InjectRequest{
		Query: "migration",
	})
	expectStatus(t, resp, http.StatusOK)

	var result types.InjectResponse
	decodeJSON(t, resp, &result)

	if result.Total < 0 {
		t.Errorf("Total = %d, want >= 0", result.Total)
	}
	if result.Context == "" && result.Total > 0 {
		t.Error("Context is empty but Total > 0")
	}
}

func TestInject_WithTypeFilter(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Inject Plan", "Inject test content")
	env.createEntry("task", "Inject Task", "Inject test content")

	resp := env.post("/inject", types.InjectRequest{
		Query: "Inject",
		Type:  "plan",
	})
	expectStatus(t, resp, http.StatusOK)

	var result types.InjectResponse
	decodeJSON(t, resp, &result)

	for _, e := range result.Entries {
		if e.Type != "plan" {
			t.Errorf("entry type = %q, want %q", e.Type, "plan")
		}
	}
}

// =============================================================================
// Graph Traversal
// =============================================================================

func TestGraph_Backlinks(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Graph Target", "Target entry for graph test")

	resp := env.get("/entries/" + id + "/backlinks")
	expectStatus(t, resp, http.StatusOK)

	var entries []types.BrainEntry
	decodeJSON(t, resp, &entries)
	_ = entries
}

func TestGraph_Outlinks(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Graph Source", "Source entry for graph test")

	resp := env.get("/entries/" + id + "/outlinks")
	expectStatus(t, resp, http.StatusOK)

	var entries []types.BrainEntry
	decodeJSON(t, resp, &entries)
	_ = entries
}

func TestGraph_Related(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Related Test", "Entry for related test")

	resp := env.get("/entries/" + id + "/related")
	expectStatus(t, resp, http.StatusOK)

	var entries []types.BrainEntry
	decodeJSON(t, resp, &entries)
	_ = entries
}

func TestGraph_NonexistentEntry(t *testing.T) {
	env := newTestServer(t)

	// Graph endpoints return 200 with empty array for nonexistent entries
	// (they query by path, and a missing path simply has no links)
	tests := []struct {
		name string
		path string
	}{
		{"backlinks", "/entries/nonexist/backlinks"},
		{"outlinks", "/entries/nonexist/outlinks"},
		{"related", "/entries/nonexist/related"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := env.get(tt.path)
			expectStatus(t, resp, http.StatusOK)

			var entries []types.BrainEntry
			decodeJSON(t, resp, &entries)

			if len(entries) != 0 {
				t.Errorf("expected empty array, got %d entries", len(entries))
			}
		})
	}
}

// =============================================================================
// Sections
// =============================================================================

func TestSections_List(t *testing.T) {
	env := newTestServer(t)

	content := "# Introduction\nSome intro text\n\n## Details\nSome details\n\n## Conclusion\nWrap up"
	id := env.createEntry("plan", "Sections Test", content)

	resp := env.get("/entries/" + id + "/sections")
	expectStatus(t, resp, http.StatusOK)

	var sections types.SectionsResponse
	decodeJSON(t, resp, &sections)

	if len(sections.Sections) == 0 {
		t.Error("expected at least one section")
	}
	if sections.Path == "" {
		t.Error("Path is empty")
	}
}

func TestSections_GetSpecific(t *testing.T) {
	env := newTestServer(t)

	content := "# Introduction\nSome intro text\n\n## Details\nSome details here\n\n## Conclusion\nWrap up"
	id := env.createEntry("plan", "Section Get Test", content)

	resp := env.get("/entries/" + id + "/sections/Details")
	expectStatus(t, resp, http.StatusOK)

	var section types.SectionContentResponse
	decodeJSON(t, resp, &section)

	if section.Title != "Details" {
		t.Errorf("Title = %q, want %q", section.Title, "Details")
	}
	if section.Content == "" {
		t.Error("Content is empty")
	}
	if section.Path == "" {
		t.Error("Path is empty")
	}
}

func TestSections_NotFound(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/entries/nonexist/sections")
	expectStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Stats
// =============================================================================

func TestStats(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Stats Plan", "Content")
	env.createEntry("task", "Stats Task", "Content")

	resp := env.get("/stats")
	expectStatus(t, resp, http.StatusOK)

	var stats types.StatsResponse
	decodeJSON(t, resp, &stats)

	if stats.TotalEntries < 2 {
		t.Errorf("TotalEntries = %d, want >= 2", stats.TotalEntries)
	}
	if stats.ByType == nil {
		t.Error("ByType is nil")
	}
}

// =============================================================================
// Orphans
// =============================================================================

func TestOrphans(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Orphan Test", "No links to this entry")

	resp := env.get("/orphans")
	expectStatus(t, resp, http.StatusOK)

	var entries []types.BrainEntry
	decodeJSON(t, resp, &entries)
	_ = entries
}

func TestOrphans_WithTypeFilter(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Orphan Plan", "Content")
	env.createEntry("task", "Orphan Task", "Content")

	resp := env.get("/orphans?type=plan")
	expectStatus(t, resp, http.StatusOK)

	var entries []types.BrainEntry
	decodeJSON(t, resp, &entries)

	for _, e := range entries {
		if e.Type != "plan" {
			t.Errorf("orphan type = %q, want %q", e.Type, "plan")
		}
	}
}

// =============================================================================
// Stale
// =============================================================================

func TestStale(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("plan", "Stale Test", "Content")

	resp := env.get("/stale?days=0")
	expectStatus(t, resp, http.StatusOK)

	var entries []types.BrainEntry
	decodeJSON(t, resp, &entries)
	_ = entries
}

// =============================================================================
// Verify
// =============================================================================

func TestVerify(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Verify Test", "Content to verify")

	resp := env.post("/entries/"+id+"/verify", nil)
	expectStatus(t, resp, http.StatusOK)

	var result types.VerifyResponse
	decodeJSON(t, resp, &result)

	if !result.Success {
		t.Error("Success = false, want true")
	}
	if result.Path == "" {
		t.Error("Path is empty")
	}
	if result.VerifiedAt == "" {
		t.Error("VerifiedAt is empty")
	}
}

func TestVerify_NotFound(t *testing.T) {
	env := newTestServer(t)

	resp := env.post("/entries/nonexist/verify", nil)
	expectStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Link Generation
// =============================================================================

func TestGenerateLink(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Link Test", "Content")

	getResp := env.get("/entries/" + id)
	expectStatus(t, getResp, http.StatusOK)
	var entry types.BrainEntry
	decodeJSON(t, getResp, &entry)

	resp := env.post("/link", types.LinkRequest{
		Path: entry.Path,
	})
	expectStatus(t, resp, http.StatusOK)

	var link types.LinkResponse
	decodeJSON(t, resp, &link)

	if link.Link == "" {
		t.Error("Link is empty")
	}
	if !strings.Contains(link.Link, "[") || !strings.Contains(link.Link, "]") {
		t.Errorf("Link %q doesn't look like a markdown link", link.Link)
	}
}

func TestGenerateLink_WithTitle(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Link Title Test", "Content")

	getResp := env.get("/entries/" + id)
	expectStatus(t, getResp, http.StatusOK)
	var entry types.BrainEntry
	decodeJSON(t, getResp, &entry)

	withTitle := true
	resp := env.post("/link", types.LinkRequest{
		Path:      entry.Path,
		WithTitle: &withTitle,
	})
	expectStatus(t, resp, http.StatusOK)

	var link types.LinkResponse
	decodeJSON(t, resp, &link)

	if link.Link == "" {
		t.Error("Link is empty")
	}
}

// =============================================================================
// Move Entry
// =============================================================================

func TestMoveEntry(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Move Test", "Content to move")

	resp := env.post("/entries/"+id+"/move", types.MoveEntryRequest{
		Project: "new-project",
	})
	expectStatus(t, resp, http.StatusOK)

	var result types.MoveResult
	decodeJSON(t, resp, &result)

	if !result.Success {
		t.Error("Success = false, want true")
	}
	if result.From == "" {
		t.Error("From is empty")
	}
	if result.To == "" {
		t.Error("To is empty")
	}
	if !strings.Contains(result.To, "new-project") {
		t.Errorf("To = %q, want to contain %q", result.To, "new-project")
	}
}

func TestMoveEntry_MissingProject(t *testing.T) {
	env := newTestServer(t)

	id := env.createEntry("plan", "Move Fail", "Content")

	resp := env.post("/entries/"+id+"/move", types.MoveEntryRequest{})
	expectStatus(t, resp, http.StatusBadRequest)
}

func TestMoveEntry_NotFound(t *testing.T) {
	env := newTestServer(t)

	resp := env.post("/entries/nonexist/move", types.MoveEntryRequest{
		Project: "target",
	})
	expectStatus(t, resp, http.StatusNotFound)
}

// =============================================================================
// Task Endpoints
// =============================================================================

func TestTasks_ListProjects(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/tasks")
	expectStatus(t, resp, http.StatusOK)

	var projects types.ProjectListResponse
	decodeJSON(t, resp, &projects)

	if projects.Projects == nil {
		t.Error("Projects is nil, want empty slice")
	}
}

func TestTasks_GetTasks(t *testing.T) {
	env := newTestServer(t)

	env.createEntry("task", "Task for Project", "Task content")

	resp := env.get("/tasks/default/")
	expectStatus(t, resp, http.StatusOK)

	var taskList types.TaskListResponse
	decodeJSON(t, resp, &taskList)

	if taskList.Tasks == nil {
		t.Error("Tasks is nil")
	}
}

func TestTasks_Ready(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/tasks/testproj/ready")
	expectStatus(t, resp, http.StatusOK)

	// Response is wrapped: {"tasks": [...]}
	var wrapper struct {
		Tasks []types.ResolvedTask `json:"tasks"`
	}
	decodeJSON(t, resp, &wrapper)
	_ = wrapper.Tasks
}

func TestTasks_Waiting(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/tasks/testproj/waiting")
	expectStatus(t, resp, http.StatusOK)

	// Response is wrapped: {"tasks": [...]}
	var wrapper struct {
		Tasks []types.ResolvedTask `json:"tasks"`
	}
	decodeJSON(t, resp, &wrapper)
	_ = wrapper.Tasks
}

func TestTasks_Blocked(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/tasks/testproj/blocked")
	expectStatus(t, resp, http.StatusOK)

	// Response is wrapped: {"tasks": [...]}
	var wrapper struct {
		Tasks []types.ResolvedTask `json:"tasks"`
	}
	decodeJSON(t, resp, &wrapper)
	_ = wrapper.Tasks
}

func TestTasks_Next(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/tasks/testproj/next")
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 200 or 404", resp.StatusCode)
	}
	resp.Body.Close()
}

// =============================================================================
// SSE Stream
// =============================================================================

func TestSSE_Stream(t *testing.T) {
	env := newTestServer(t)

	origInterval := api.DefaultHeartbeatInterval
	api.DefaultHeartbeatInterval = 100 * time.Millisecond
	t.Cleanup(func() { api.DefaultHeartbeatInterval = origInterval })

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.baseURL+"/tasks/testproj/stream", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()

	expectStatus(t, resp, http.StatusOK)

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	var events []sseEvent

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType := strings.TrimPrefix(line, "event: ")
			if scanner.Scan() {
				dataLine := scanner.Text()
				if strings.HasPrefix(dataLine, "data: ") {
					data := strings.TrimPrefix(dataLine, "data: ")
					events = append(events, sseEvent{Type: eventType, Data: data})
				}
			}

			if len(events) >= 3 {
				break
			}
		}
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 SSE events (connected + tasks_snapshot), got %d", len(events))
	}

	if events[0].Type != "connected" {
		t.Errorf("first event type = %q, want %q", events[0].Type, "connected")
	}

	var connData types.SSEConnectedData
	if err := json.Unmarshal([]byte(events[0].Data), &connData); err != nil {
		t.Fatalf("unmarshal connected data: %v", err)
	}
	if connData.Type != types.SSEEventConnected {
		t.Errorf("connected.Type = %q, want %q", connData.Type, types.SSEEventConnected)
	}
	if connData.Transport != "sse" {
		t.Errorf("connected.Transport = %q, want %q", connData.Transport, "sse")
	}
	if connData.ProjectID != "testproj" {
		t.Errorf("connected.ProjectID = %q, want %q", connData.ProjectID, "testproj")
	}

	if events[1].Type != "tasks_snapshot" {
		t.Errorf("second event type = %q, want %q", events[1].Type, "tasks_snapshot")
	}

	var snapData types.SSETasksSnapshotData
	if err := json.Unmarshal([]byte(events[1].Data), &snapData); err != nil {
		t.Fatalf("unmarshal snapshot data: %v", err)
	}
	if snapData.Type != types.SSEEventTasksSnapshot {
		t.Errorf("snapshot.Type = %q, want %q", snapData.Type, types.SSEEventTasksSnapshot)
	}
}

type sseEvent struct {
	Type string
	Data string
}

// =============================================================================
// Error Handling
// =============================================================================

func TestError_NotFoundRoute(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/nonexistent")
	expectStatus(t, resp, http.StatusNotFound)

	var errResp types.ErrorResponse
	decodeJSON(t, resp, &errResp)

	if errResp.Error != "Not Found" {
		t.Errorf("error = %q, want %q", errResp.Error, "Not Found")
	}
}

func TestError_MethodNotAllowed(t *testing.T) {
	env := newTestServer(t)

	resp := env.put("/entries", map[string]string{"test": "data"})
	expectStatus(t, resp, http.StatusMethodNotAllowed)

	var errResp types.ErrorResponse
	decodeJSON(t, resp, &errResp)

	if errResp.Error != "Method Not Allowed" {
		t.Errorf("error = %q, want %q", errResp.Error, "Method Not Allowed")
	}
}

// =============================================================================
// Response Format Compatibility
// =============================================================================

func TestResponseFormat_ContentType(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/health")
	expectStatus(t, resp, http.StatusOK)

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	resp.Body.Close()
}

func TestResponseFormat_ErrorShape(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/entries/nonexist")
	expectStatus(t, resp, http.StatusNotFound)

	var raw map[string]interface{}
	decodeJSON(t, resp, &raw)

	if _, ok := raw["error"]; !ok {
		t.Error("error response missing 'error' field")
	}
	if _, ok := raw["message"]; !ok {
		t.Error("error response missing 'message' field")
	}
}

func TestResponseFormat_ValidationErrorShape(t *testing.T) {
	env := newTestServer(t)

	resp := env.post("/entries", types.CreateEntryRequest{})
	expectStatus(t, resp, http.StatusBadRequest)

	var raw map[string]interface{}
	decodeJSON(t, resp, &raw)

	if _, ok := raw["error"]; !ok {
		t.Error("validation error missing 'error' field")
	}
	if _, ok := raw["message"]; !ok {
		t.Error("validation error missing 'message' field")
	}
	details, ok := raw["details"]
	if !ok {
		t.Error("validation error missing 'details' field")
	}
	if arr, ok := details.([]interface{}); ok {
		if len(arr) == 0 {
			t.Error("details array is empty")
		}
		for _, d := range arr {
			dm, ok := d.(map[string]interface{})
			if !ok {
				t.Error("detail is not an object")
				continue
			}
			if _, ok := dm["field"]; !ok {
				t.Error("detail missing 'field'")
			}
			if _, ok := dm["message"]; !ok {
				t.Error("detail missing 'message'")
			}
		}
	} else {
		t.Error("details is not an array")
	}
}

func TestResponseFormat_ListShape(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/entries")
	expectStatus(t, resp, http.StatusOK)

	var raw map[string]interface{}
	decodeJSON(t, resp, &raw)

	for _, field := range []string{"entries", "total", "limit", "offset"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("list response missing %q field", field)
		}
	}
}

func TestResponseFormat_SearchShape(t *testing.T) {
	env := newTestServer(t)

	resp := env.post("/search", types.SearchRequest{Query: "test"})
	expectStatus(t, resp, http.StatusOK)

	var raw map[string]interface{}
	decodeJSON(t, resp, &raw)

	for _, field := range []string{"results", "total"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("search response missing %q field", field)
		}
	}
}

// =============================================================================
// CORS Headers
// =============================================================================

func TestCORS_Headers(t *testing.T) {
	env := newTestServer(t)

	req, err := http.NewRequest(http.MethodOptions, env.baseURL+"/health", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer resp.Body.Close()

	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao == "" {
		t.Error("Access-Control-Allow-Origin header is missing")
	}
}

// =============================================================================
// Security Headers
// =============================================================================

func TestSecurityHeaders(t *testing.T) {
	env := newTestServer(t)

	resp := env.get("/health")
	expectStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	headers := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
	}

	for header, want := range headers {
		got := resp.Header.Get(header)
		if got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}
