package service

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Monitor Tag/Title Helpers
// =============================================================================

func TestBuildMonitorTag_AllScope(t *testing.T) {
	tag := BuildMonitorTag("blocked-inspector", MonitorScope{Type: "all"})
	if tag != "monitor:blocked-inspector:all" {
		t.Errorf("expected monitor:blocked-inspector:all, got %s", tag)
	}
}

func TestBuildMonitorTag_ProjectScope(t *testing.T) {
	tag := BuildMonitorTag("blocked-inspector", MonitorScope{Type: "project", Project: "brain-api"})
	if tag != "monitor:blocked-inspector:project:brain-api" {
		t.Errorf("expected monitor:blocked-inspector:project:brain-api, got %s", tag)
	}
}

func TestBuildMonitorTag_FeatureScope(t *testing.T) {
	tag := BuildMonitorTag("feature-review", MonitorScope{Type: "feature", FeatureID: "auth", Project: "brain-api"})
	if tag != "monitor:feature-review:feature:auth:brain-api" {
		t.Errorf("expected monitor:feature-review:feature:auth:brain-api, got %s", tag)
	}
}

func TestParseMonitorTag_AllScope(t *testing.T) {
	result := ParseMonitorTag("monitor:blocked-inspector:all")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TemplateID != "blocked-inspector" {
		t.Errorf("expected templateId blocked-inspector, got %s", result.TemplateID)
	}
	if result.Scope.Type != "all" {
		t.Errorf("expected scope type all, got %s", result.Scope.Type)
	}
}

func TestParseMonitorTag_ProjectScope(t *testing.T) {
	result := ParseMonitorTag("monitor:blocked-inspector:project:brain-api")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TemplateID != "blocked-inspector" {
		t.Errorf("expected templateId blocked-inspector, got %s", result.TemplateID)
	}
	if result.Scope.Type != "project" {
		t.Errorf("expected scope type project, got %s", result.Scope.Type)
	}
	if result.Scope.Project != "brain-api" {
		t.Errorf("expected project brain-api, got %s", result.Scope.Project)
	}
}

func TestParseMonitorTag_FeatureScope(t *testing.T) {
	result := ParseMonitorTag("monitor:feature-review:feature:auth:brain-api")
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.TemplateID != "feature-review" {
		t.Errorf("expected templateId feature-review, got %s", result.TemplateID)
	}
	if result.Scope.Type != "feature" {
		t.Errorf("expected scope type feature, got %s", result.Scope.Type)
	}
	if result.Scope.FeatureID != "auth" {
		t.Errorf("expected featureId auth, got %s", result.Scope.FeatureID)
	}
	if result.Scope.Project != "brain-api" {
		t.Errorf("expected project brain-api, got %s", result.Scope.Project)
	}
}

func TestParseMonitorTag_InvalidPrefix(t *testing.T) {
	result := ParseMonitorTag("not-a-monitor-tag")
	if result != nil {
		t.Error("expected nil for non-monitor tag")
	}
}

func TestParseMonitorTag_InvalidFormat(t *testing.T) {
	result := ParseMonitorTag("monitor:")
	if result != nil {
		t.Error("expected nil for incomplete tag")
	}
}

func TestBuildMonitorTitle(t *testing.T) {
	title := BuildMonitorTitle("Blocked Task Inspector", MonitorScope{Type: "project", Project: "brain-api"})
	if title != "Monitor: Blocked Task Inspector (project brain-api)" {
		t.Errorf("unexpected title: %s", title)
	}
}

func TestBuildMonitorTitle_AllScope(t *testing.T) {
	title := BuildMonitorTitle("Blocked Task Inspector", MonitorScope{Type: "all"})
	if title != "Monitor: Blocked Task Inspector (all projects)" {
		t.Errorf("unexpected title: %s", title)
	}
}

func TestBuildMonitorTitle_FeatureScope(t *testing.T) {
	title := BuildMonitorTitle("Feature Code Review", MonitorScope{Type: "feature", FeatureID: "auth", Project: "brain-api"})
	if title != "Monitor: Feature Code Review (feature auth)" {
		t.Errorf("unexpected title: %s", title)
	}
}

// =============================================================================
// MonitorService Tests (with mock BrainService)
// =============================================================================

// mockBrainForMonitor is a minimal mock that implements the BrainService methods
// used by MonitorService: Save, List, Recall, Update, Delete.
type mockBrainForMonitor struct {
	entries map[string]*types.BrainEntry // keyed by path
	nextID  int
}

func newMockBrainForMonitor() *mockBrainForMonitor {
	return &mockBrainForMonitor{
		entries: make(map[string]*types.BrainEntry),
		nextID:  1,
	}
}

func (m *mockBrainForMonitor) Save(_ context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
	id := fmt.Sprintf("aaaaaaa%d", m.nextID)
	m.nextID++
	path := "projects/test/task/" + id + ".md"
	entry := &types.BrainEntry{
		ID:              id,
		Path:            path,
		Title:           req.Title,
		Type:            req.Type,
		Status:          req.Status,
		Tags:            req.Tags,
		Schedule:        req.Schedule,
		ScheduleEnabled: req.ScheduleEnabled,
		DirectPrompt:    req.DirectPrompt,
		ExecutionMode:   req.ExecutionMode,
		CompleteOnIdle:  req.CompleteOnIdle,
		FeatureID:       req.FeatureID,
		ProjectID:       req.Project,
	}
	m.entries[path] = entry
	return &types.CreateEntryResponse{
		ID:   id,
		Path: path,
	}, nil
}

func (m *mockBrainForMonitor) List(_ context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	var results []types.BrainEntry
	for _, entry := range m.entries {
		if req.Type != "" && entry.Type != req.Type {
			continue
		}
		if req.Tags != "" {
			// Tags is a comma-separated string in the request
			reqTags := strings.Split(req.Tags, ",")
			matched := false
			for _, reqTag := range reqTags {
				reqTag = strings.TrimSpace(reqTag)
				for _, entryTag := range entry.Tags {
					if entryTag == reqTag || strings.HasPrefix(entryTag, reqTag+":") {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				continue
			}
		}
		results = append(results, *entry)
	}
	return &types.ListEntriesResponse{
		Entries: results,
		Total:   len(results),
	}, nil
}

func (m *mockBrainForMonitor) Recall(_ context.Context, pathOrID string) (*types.BrainEntry, error) {
	if entry, ok := m.entries[pathOrID]; ok {
		return entry, nil
	}
	for _, entry := range m.entries {
		if entry.ID == pathOrID {
			return entry, nil
		}
	}
	return nil, fmt.Errorf("not found: %s", pathOrID)
}

func (m *mockBrainForMonitor) Update(_ context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
	entry, err := m.Recall(context.Background(), pathOrID)
	if err != nil {
		return nil, err
	}
	if req.ScheduleEnabled != nil {
		entry.ScheduleEnabled = req.ScheduleEnabled
	}
	return entry, nil
}

func (m *mockBrainForMonitor) Delete(_ context.Context, pathOrID string) error {
	if _, ok := m.entries[pathOrID]; ok {
		delete(m.entries, pathOrID)
		return nil
	}
	for path, entry := range m.entries {
		if entry.ID == pathOrID {
			delete(m.entries, path)
			return nil
		}
	}
	return fmt.Errorf("not found: %s", pathOrID)
}

// Unused methods — satisfy interface
func (m *mockBrainForMonitor) Move(context.Context, string, string) (*types.MoveResult, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) Search(context.Context, types.SearchRequest) (*types.SearchResponse, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) Inject(context.Context, types.InjectRequest) (*types.InjectResponse, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetBacklinks(context.Context, string) ([]types.BrainEntry, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetOutlinks(context.Context, string) ([]types.BrainEntry, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetRelated(context.Context, string, int) ([]types.BrainEntry, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetSections(context.Context, string) (*types.SectionsResponse, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetSection(context.Context, string, string, bool) (*types.SectionContentResponse, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetStats(context.Context, bool) (*types.StatsResponse, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetOrphans(context.Context, string, int) ([]types.BrainEntry, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GetStale(context.Context, int, string, int) ([]types.BrainEntry, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) Verify(context.Context, string) (*types.VerifyResponse, error) {
	return nil, nil
}
func (m *mockBrainForMonitor) GenerateLink(context.Context, types.LinkRequest) (*types.LinkResponse, error) {
	return nil, nil
}

// =============================================================================
// MonitorService.Create Tests
// =============================================================================

func TestMonitorService_Create(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	result, err := svc.Create(ctx, "blocked-inspector", MonitorScope{Type: "project", Project: "brain-api"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID == "" {
		t.Error("expected non-empty ID")
	}
	if result.Path == "" {
		t.Error("expected non-empty Path")
	}
	if !strings.Contains(result.Title, "Blocked Task Inspector") {
		t.Errorf("expected title to contain 'Blocked Task Inspector', got %s", result.Title)
	}
}

func TestMonitorService_Create_UnknownTemplate(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	_, err := svc.Create(ctx, "nonexistent", MonitorScope{Type: "all"}, nil)
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "unknown monitor template") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMonitorService_Create_Duplicate(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	_, err := svc.Create(ctx, "blocked-inspector", MonitorScope{Type: "all"}, nil)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	_, err = svc.Create(ctx, "blocked-inspector", MonitorScope{Type: "all"}, nil)
	if err == nil {
		t.Fatal("expected error for duplicate monitor")
	}
}

func TestMonitorService_Find(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	_, err := svc.Create(ctx, "blocked-inspector", MonitorScope{Type: "project", Project: "brain-api"}, nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	found, err := svc.Find(ctx, "blocked-inspector", MonitorScope{Type: "project", Project: "brain-api"})
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find monitor")
	}
	if found.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestMonitorService_Find_NotFound(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	found, err := svc.Find(ctx, "blocked-inspector", MonitorScope{Type: "all"})
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if found != nil {
		t.Error("expected nil for non-existent monitor")
	}
}

func TestMonitorService_Toggle(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	result, err := svc.Create(ctx, "blocked-inspector", MonitorScope{Type: "all"}, nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	path, err := svc.Toggle(ctx, result.ID, false)
	if err != nil {
		t.Fatalf("toggle failed: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}
}

func TestMonitorService_Delete(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	result, err := svc.Create(ctx, "blocked-inspector", MonitorScope{Type: "all"}, nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	path, err := svc.Delete(ctx, result.ID)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path")
	}

	// Verify it's gone
	found, err := svc.Find(ctx, "blocked-inspector", MonitorScope{Type: "all"})
	if err != nil {
		t.Fatalf("find after delete failed: %v", err)
	}
	if found != nil {
		t.Error("expected nil after delete")
	}
}

func TestMonitorService_List(t *testing.T) {
	mock := newMockBrainForMonitor()
	svc := NewMonitorService(mock)
	ctx := context.Background()

	_, err := svc.Create(ctx, "blocked-inspector", MonitorScope{Type: "project", Project: "brain-api"}, nil)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	monitors, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(monitors) != 1 {
		t.Fatalf("expected 1 monitor, got %d", len(monitors))
	}
	if monitors[0].TemplateID != "blocked-inspector" {
		t.Errorf("expected templateId blocked-inspector, got %s", monitors[0].TemplateID)
	}
}
