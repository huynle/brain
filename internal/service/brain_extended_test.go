package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// GetBacklinks tests
// =============================================================================

func TestGetBacklinks_ReturnsEmptySlice(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create an entry with no backlinks
	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Isolated Entry",
		Content: "No one links to me.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	results, err := svc.GetBacklinks(ctx, saved.Path)
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 backlinks, got %d", len(results))
	}
}

// =============================================================================
// GetOutlinks tests
// =============================================================================

func TestGetOutlinks_ReturnsEmptySlice(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "No Outlinks",
		Content: "No links here.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	results, err := svc.GetOutlinks(ctx, saved.Path)
	if err != nil {
		t.Fatalf("GetOutlinks failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 outlinks, got %d", len(results))
	}
}

// =============================================================================
// GetRelated tests
// =============================================================================

func TestGetRelated_ReturnsEmptySlice(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "No Related",
		Content: "Nothing related.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	results, err := svc.GetRelated(ctx, saved.Path, 10)
	if err != nil {
		t.Fatalf("GetRelated failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(results) != 0 {
		t.Errorf("expected 0 related, got %d", len(results))
	}
}

func TestGetRelated_DefaultLimit(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Default Limit Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// limit=0 should default to 10, not error
	results, err := svc.GetRelated(ctx, saved.Path, 0)
	if err != nil {
		t.Fatalf("GetRelated with limit=0 failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice")
	}
}

// =============================================================================
// GetSections tests
// =============================================================================

func TestGetSections_ExtractsHeadings(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	content := `## Introduction
Some intro text.

### Details
Detail text.

## Conclusion
Final thoughts.`

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Sections Test",
		Content: content,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GetSections(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetSections failed: %v", err)
	}

	if resp.Path != saved.Path {
		t.Errorf("expected path %q, got %q", saved.Path, resp.Path)
	}

	if len(resp.Sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(resp.Sections))
	}

	// Check first section
	if resp.Sections[0].Title != "Introduction" {
		t.Errorf("expected 'Introduction', got %q", resp.Sections[0].Title)
	}
	if resp.Sections[0].Level != 2 {
		t.Errorf("expected level 2, got %d", resp.Sections[0].Level)
	}

	// Check second section
	if resp.Sections[1].Title != "Details" {
		t.Errorf("expected 'Details', got %q", resp.Sections[1].Title)
	}
	if resp.Sections[1].Level != 3 {
		t.Errorf("expected level 3, got %d", resp.Sections[1].Level)
	}

	// Check third section
	if resp.Sections[2].Title != "Conclusion" {
		t.Errorf("expected 'Conclusion', got %q", resp.Sections[2].Title)
	}
	if resp.Sections[2].Level != 2 {
		t.Errorf("expected level 2, got %d", resp.Sections[2].Level)
	}
}

func TestGetSections_NoHeadings(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "No Headings",
		Content: "Just plain text without any headings.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GetSections(ctx, saved.ID)
	if err != nil {
		t.Fatalf("GetSections failed: %v", err)
	}

	if len(resp.Sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(resp.Sections))
	}
	if resp.Sections == nil {
		t.Error("expected non-nil empty slice")
	}
}

func TestGetSections_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.GetSections(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

// =============================================================================
// GetSection tests
// =============================================================================

func TestGetSection_ExtractsContent(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	content := `## Introduction
Intro paragraph.

## Implementation
Implementation details here.
More implementation.

## Conclusion
Final thoughts.`

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Section Content Test",
		Content: content,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GetSection(ctx, saved.ID, "Implementation", false)
	if err != nil {
		t.Fatalf("GetSection failed: %v", err)
	}

	if resp.Title != "Implementation" {
		t.Errorf("expected title 'Implementation', got %q", resp.Title)
	}
	if !strings.Contains(resp.Content, "Implementation details here.") {
		t.Errorf("expected content to contain 'Implementation details here.', got %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "More implementation.") {
		t.Errorf("expected content to contain 'More implementation.', got %q", resp.Content)
	}
	// Should NOT contain content from other sections
	if strings.Contains(resp.Content, "Final thoughts.") {
		t.Error("content should not contain text from next section")
	}
	if resp.Path != saved.Path {
		t.Errorf("expected path %q, got %q", saved.Path, resp.Path)
	}
}

func TestGetSection_CaseInsensitiveMatch(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	content := `## My Important Section
Section content here.

## Other Section
Other content.`

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Case Test",
		Content: content,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Search with different case
	resp, err := svc.GetSection(ctx, saved.ID, "my important", false)
	if err != nil {
		t.Fatalf("GetSection failed: %v", err)
	}

	if resp.Title != "My Important Section" {
		t.Errorf("expected 'My Important Section', got %q", resp.Title)
	}
	if !strings.Contains(resp.Content, "Section content here.") {
		t.Errorf("expected section content, got %q", resp.Content)
	}
}

func TestGetSection_SubstringMatch(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	content := `## JWT Middleware Implementation
JWT details.

## Database Layer
DB details.`

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Substring Test",
		Content: content,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Search with substring
	resp, err := svc.GetSection(ctx, saved.ID, "JWT", false)
	if err != nil {
		t.Fatalf("GetSection failed: %v", err)
	}

	if resp.Title != "JWT Middleware Implementation" {
		t.Errorf("expected 'JWT Middleware Implementation', got %q", resp.Title)
	}
}

func TestGetSection_WithSubsections(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	content := `## Parent Section
Parent content.

### Child Section
Child content.

### Another Child
More child content.

## Next Top Section
Next content.`

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Subsections Test",
		Content: content,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GetSection(ctx, saved.ID, "Parent Section", true)
	if err != nil {
		t.Fatalf("GetSection failed: %v", err)
	}

	// Should include child sections
	if !strings.Contains(resp.Content, "Child content.") {
		t.Error("expected content to include child section content")
	}
	if !strings.Contains(resp.Content, "More child content.") {
		t.Error("expected content to include another child section content")
	}
	// Should NOT include next top-level section
	if strings.Contains(resp.Content, "Next content.") {
		t.Error("content should not include next top-level section")
	}
	if !resp.IncludeSubsections {
		t.Error("expected IncludeSubsections=true")
	}
}

func TestGetSection_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Section Not Found",
		Content: "## Existing\nContent.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	_, err = svc.GetSection(ctx, saved.ID, "Nonexistent Section", false)
	if err == nil {
		t.Fatal("expected error for nonexistent section")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should contain 'not found', got: %v", err)
	}
}

func TestGetSection_LastSection(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	content := `## First
First content.

## Last
Last content here.
More last content.`

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Last Section Test",
		Content: content,
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GetSection(ctx, saved.ID, "Last", false)
	if err != nil {
		t.Fatalf("GetSection failed: %v", err)
	}

	if !strings.Contains(resp.Content, "Last content here.") {
		t.Errorf("expected last section content, got %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "More last content.") {
		t.Errorf("expected more last section content, got %q", resp.Content)
	}
}

// =============================================================================
// GetStats tests
// =============================================================================

func TestGetStats_ReturnsStats(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Create some entries
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan 1"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Task 1"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan 2"})

	resp, err := svc.GetStats(ctx, false, "")
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if resp.TotalEntries != 3 {
		t.Errorf("expected 3 total entries, got %d", resp.TotalEntries)
	}
	if resp.ByType["plan"] != 2 {
		t.Errorf("expected 2 plans, got %d", resp.ByType["plan"])
	}
	if resp.ByType["task"] != 1 {
		t.Errorf("expected 1 task, got %d", resp.ByType["task"])
	}
	if resp.BrainDir == "" {
		t.Error("expected non-empty BrainDir")
	}
	if resp.DBPath == "" {
		t.Error("expected non-empty DBPath")
	}
}

func TestGetStats_GlobalFilter(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "pattern", Title: "Global", Global: boolPtr(true)})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Project"})

	resp, err := svc.GetStats(ctx, true, "")
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// Global filter should only count global entries
	if resp.TotalEntries != 1 {
		t.Errorf("expected 1 global entry, got %d", resp.TotalEntries)
	}
}

// TestGetStats_ProjectFilter verifies that passing a project name scopes
// TotalEntries to the entries under projects/<name>/. This is the P1.b end
// of the "brain_stats needs a project param" fix — proves the plumbing
// works from service through storage.
func TestGetStats_ProjectFilter(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Two projects with clearly different counts.
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Alpha 1", Project: "alpha"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Alpha 2", Project: "alpha"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Alpha 3", Project: "alpha"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Beta 1", Project: "beta"})

	// Baseline: unscoped call sees all 4 entries.
	all, err := svc.GetStats(ctx, false, "")
	if err != nil {
		t.Fatalf("GetStats(all) failed: %v", err)
	}
	if all.TotalEntries != 4 {
		t.Fatalf("unscoped TotalEntries = %d, want 4", all.TotalEntries)
	}

	// Scoped to alpha: only the 3 alpha entries count in the primary total.
	alpha, err := svc.GetStats(ctx, false, "alpha")
	if err != nil {
		t.Fatalf("GetStats(alpha) failed: %v", err)
	}
	if alpha.TotalEntries != 3 {
		t.Errorf("project=alpha TotalEntries = %d, want 3", alpha.TotalEntries)
	}

	// Scoped to beta: only the 1 beta entry counts.
	beta, err := svc.GetStats(ctx, false, "beta")
	if err != nil {
		t.Fatalf("GetStats(beta) failed: %v", err)
	}
	if beta.TotalEntries != 1 {
		t.Errorf("project=beta TotalEntries = %d, want 1", beta.TotalEntries)
	}

	// The GlobalEntries and ProjectEntries roll-ups should be independent of
	// the primary scope (they use their own storage.GetStats calls).
	if beta.ProjectEntries != 4 {
		t.Errorf("beta.ProjectEntries (roll-up) = %d, want 4", beta.ProjectEntries)
	}
}

// TestGetStats_ProjectWinsOverGlobal verifies the precedence rule: when
// both `global=true` and `project=X` are supplied, the project scope wins.
func TestGetStats_ProjectWinsOverGlobal(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "pattern", Title: "Global One", Global: boolPtr(true)})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "pattern", Title: "Global Two", Global: boolPtr(true)})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "P One", Project: "alpha"})

	resp, err := svc.GetStats(ctx, true, "alpha")
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if resp.TotalEntries != 1 {
		t.Errorf("project should win over global: TotalEntries = %d, want 1", resp.TotalEntries)
	}
}

// TestGetOrphans_ProjectFilter verifies P1.d storage plumbing: the
// project param is translated into a path-prefix filter and only orphans
// under that prefix are returned.
func TestGetOrphans_ProjectFilter(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Alpha Orphan", Project: "alpha"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Beta Orphan", Project: "beta"})

	results, err := svc.GetOrphans(ctx, "", 50, "alpha")
	if err != nil {
		t.Fatalf("GetOrphans failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 orphan in alpha, got %d", len(results))
	}
	if results[0].Title != "Alpha Orphan" {
		t.Errorf("got %q, want %q", results[0].Title, "Alpha Orphan")
	}
}

// TestGetStale_ProjectFilter verifies P1.c storage plumbing.
func TestGetStale_ProjectFilter(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Alpha Stale", Project: "alpha"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Beta Stale", Project: "beta"})

	// days=0 → after normalization becomes 30, and both entries are
	// unverified (last_verified NULL) so both qualify as stale.
	// Scoping to alpha should filter to one.
	results, err := svc.GetStale(ctx, 30, "", 50, "alpha")
	if err != nil {
		t.Fatalf("GetStale failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 stale in alpha, got %d", len(results))
	}
	if results[0].Title != "Alpha Stale" {
		t.Errorf("got %q, want %q", results[0].Title, "Alpha Stale")
	}
}

// =============================================================================
// GetOrphans tests
// =============================================================================

func TestGetOrphans_ReturnsEntries(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// All entries without links are orphans
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Orphan 1"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Orphan 2"})

	results, err := svc.GetOrphans(ctx, "", 50, "")
	if err != nil {
		t.Fatalf("GetOrphans failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 orphans, got %d", len(results))
	}
}

func TestGetOrphans_FilterByType(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Plan Orphan"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Task Orphan"})

	results, err := svc.GetOrphans(ctx, "plan", 50, "")
	if err != nil {
		t.Fatalf("GetOrphans failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 plan orphan, got %d", len(results))
	}
	if results[0].Type != "plan" {
		t.Errorf("expected type 'plan', got %q", results[0].Type)
	}
}

func TestGetOrphans_DefaultLimit(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Orphan"})

	// limit=0 should default to 50, not error
	results, err := svc.GetOrphans(ctx, "", 0, "")
	if err != nil {
		t.Fatalf("GetOrphans with limit=0 failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice")
	}
}

// =============================================================================
// GetStale tests
// =============================================================================

func TestGetStale_ReturnsUnverifiedEntries(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Entries without verification are stale
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Stale 1"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Stale 2"})

	results, err := svc.GetStale(ctx, 30, "", 50, "")
	if err != nil {
		t.Fatalf("GetStale failed: %v", err)
	}

	if len(results) < 2 {
		t.Errorf("expected at least 2 stale entries, got %d", len(results))
	}
}

func TestGetStale_FilterByType(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Stale Plan"})
	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "task", Title: "Stale Task"})

	results, err := svc.GetStale(ctx, 30, "plan", 50, "")
	if err != nil {
		t.Fatalf("GetStale failed: %v", err)
	}

	for _, r := range results {
		if r.Type != "plan" {
			t.Errorf("expected type 'plan', got %q", r.Type)
		}
	}
}

func TestGetStale_DefaultValues(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{Type: "plan", Title: "Default Stale"})

	// days=0 and limit=0 should use defaults
	results, err := svc.GetStale(ctx, 0, "", 0, "")
	if err != nil {
		t.Fatalf("GetStale with defaults failed: %v", err)
	}
	if results == nil {
		t.Fatal("expected non-nil slice")
	}
}

// =============================================================================
// Verify tests
// =============================================================================

func TestVerify_MarksEntryVerified(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	freezeTime(t, time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC))

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Verify Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.Verify(ctx, saved.ID)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Path != saved.Path {
		t.Errorf("expected path %q, got %q", saved.Path, resp.Path)
	}
	if resp.VerifiedAt == "" {
		t.Error("expected non-empty VerifiedAt")
	}
	if !strings.Contains(resp.VerifiedAt, "2025-07-01") {
		t.Errorf("expected VerifiedAt to contain '2025-07-01', got %q", resp.VerifiedAt)
	}
}

func TestVerify_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.Verify(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

func TestVerify_ByPath(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Verify By Path",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.Verify(ctx, saved.Path)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if resp.Path != saved.Path {
		t.Errorf("expected path %q, got %q", saved.Path, resp.Path)
	}
}

// =============================================================================
// GenerateLink tests
// =============================================================================

func TestGenerateLink_ByPath(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Link Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GenerateLink(ctx, types.LinkRequest{Path: saved.Path})
	if err != nil {
		t.Fatalf("GenerateLink failed: %v", err)
	}

	if resp.Link == "" {
		t.Error("expected non-empty link")
	}
	// Link should be in format [title](id)
	if !strings.Contains(resp.Link, "Link Test") {
		t.Errorf("link should contain title, got %q", resp.Link)
	}
	if !strings.Contains(resp.Link, saved.ID) {
		t.Errorf("link should contain ID, got %q", resp.Link)
	}
}

func TestGenerateLink_ByTitle(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "Unique Link Title",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GenerateLink(ctx, types.LinkRequest{Title: "Unique Link Title"})
	if err != nil {
		t.Fatalf("GenerateLink failed: %v", err)
	}

	if !strings.Contains(resp.Link, saved.ID) {
		t.Errorf("link should contain ID %q, got %q", saved.ID, resp.Link)
	}
}

func TestGenerateLink_ByShortID(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:  "plan",
		Title: "ID Link Test",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	resp, err := svc.GenerateLink(ctx, types.LinkRequest{Path: saved.ID})
	if err != nil {
		t.Fatalf("GenerateLink failed: %v", err)
	}

	if !strings.Contains(resp.Link, "ID Link Test") {
		t.Errorf("link should contain title, got %q", resp.Link)
	}
}

func TestGenerateLink_EmptyRequest(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.GenerateLink(ctx, types.LinkRequest{})
	if err == nil {
		t.Fatal("expected error for empty request")
	}
}

func TestGenerateLink_NotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, err := svc.GenerateLink(ctx, types.LinkRequest{Path: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent entry")
	}
}

// =============================================================================
// extractSectionHeaders unit tests
// =============================================================================

func TestExtractSectionHeaders_AllLevels(t *testing.T) {
	body := `# H1
## H2
### H3
#### H4
##### H5
###### H6`

	sections := extractSectionHeaders(body)
	if len(sections) != 6 {
		t.Fatalf("expected 6 sections, got %d", len(sections))
	}

	for i, s := range sections {
		expectedLevel := i + 1
		if s.Level != expectedLevel {
			t.Errorf("section %d: expected level %d, got %d", i, expectedLevel, s.Level)
		}
	}
}

func TestExtractSectionHeaders_EmptyBody(t *testing.T) {
	sections := extractSectionHeaders("")
	if sections == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(sections))
	}
}

func TestExtractSectionHeaders_NoHashInMiddle(t *testing.T) {
	// Lines that look like headings but aren't at the start of line
	body := `Some text with ## in it
## Real Heading
Not a heading: ### nope`

	sections := extractSectionHeaders(body)
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Title != "Real Heading" {
		t.Errorf("expected 'Real Heading', got %q", sections[0].Title)
	}
}

// =============================================================================
// extractSectionContent unit tests
// =============================================================================

func TestExtractSectionContent_Basic(t *testing.T) {
	body := `## First
First content.

## Second
Second content.

## Third
Third content.`

	content, title, found := extractSectionContent(body, "Second", false)
	if !found {
		t.Fatal("expected section to be found")
	}
	if title != "Second" {
		t.Errorf("expected title 'Second', got %q", title)
	}
	if !strings.Contains(content, "Second content.") {
		t.Errorf("expected 'Second content.', got %q", content)
	}
	if strings.Contains(content, "Third content.") {
		t.Error("should not contain content from next section")
	}
}

func TestExtractSectionContent_NotFound(t *testing.T) {
	body := `## Existing
Content.`

	_, _, found := extractSectionContent(body, "Missing", false)
	if found {
		t.Error("expected section not to be found")
	}
}

func TestExtractSectionContent_IncludeSubsections(t *testing.T) {
	body := `## Parent
Parent text.

### Child 1
Child 1 text.

### Child 2
Child 2 text.

## Sibling
Sibling text.`

	content, _, found := extractSectionContent(body, "Parent", true)
	if !found {
		t.Fatal("expected section to be found")
	}
	if !strings.Contains(content, "Child 1 text.") {
		t.Error("expected child 1 content with includeSubsections=true")
	}
	if !strings.Contains(content, "Child 2 text.") {
		t.Error("expected child 2 content with includeSubsections=true")
	}
	if strings.Contains(content, "Sibling text.") {
		t.Error("should not contain sibling section content")
	}
}

func TestGraphEndpoints_ResolveShortID(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	target, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "walkthrough",
		Title:   "Link Target",
		Content: "I am linked to.",
	})
	if err != nil {
		t.Fatalf("Save target failed: %v", err)
	}

	if _, err = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "summary",
		Title:   "Link Source",
		Content: "See [Link Target](" + target.Path + ") for details.",
	}); err != nil {
		t.Fatalf("Save source failed: %v", err)
	}

	// The HTTP graph routes only carry a single path segment, so the PWA
	// addresses these by 8-char short ID. Resolution must work for both.
	for _, ident := range []string{target.ID, target.Path} {
		backs, err := svc.GetBacklinks(ctx, ident)
		if err != nil {
			t.Fatalf("GetBacklinks(%q) failed: %v", ident, err)
		}
		if len(backs) != 1 || backs[0].Title != "Link Source" {
			t.Errorf("GetBacklinks(%q) = %d results, want the source entry", ident, len(backs))
		}
	}

	// Unknown identifiers are now reported as not-found rather than as an
	// empty result. This block previously asserted the opposite: when short-ID
	// resolution was added, the not-found path was deliberately left alone to
	// keep that change narrow, and this assertion pinned that carry-over. It
	// was never the intended contract — all three HTTP handlers already had an
	// ErrNotFound -> 404 branch waiting for it. Fuller coverage of both
	// directions lives in TestGraphLookups_UnknownIdentifierIsNotFound.
	if _, err := svc.GetBacklinks(ctx, "zzzzzzzz"); !errors.Is(err, api.ErrNotFound) {
		t.Errorf("GetBacklinks(unknown) error = %v, want api.ErrNotFound", err)
	}
}

// =============================================================================
// Graph lookups: missing entry vs. unlinked entry
// =============================================================================

// TestGraphLookups_UnknownIdentifierIsNotFound locks in the distinction between
// "this entry has no links" and "this entry does not exist". Unknown
// identifiers used to be passed through to the storage queries, which matched
// nothing and returned an empty slice — so a typo'd path or a stale ID produced
// a confident "no backlinks found", the same answer a real but unlinked entry
// gives. The HTTP handlers have always had an ErrNotFound -> 404 branch for
// exactly this; it was unreachable.
func TestGraphLookups_UnknownIdentifierIsNotFound(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Both an ID-shaped identifier and a path-shaped one, since the two take
	// different routes through resolveEntry.
	for _, ident := range []string{"zzzznope", "projects/nope/plan/missing.md"} {
		t.Run(ident, func(t *testing.T) {
			if _, err := svc.GetBacklinks(ctx, ident); !errors.Is(err, api.ErrNotFound) {
				t.Errorf("GetBacklinks(%q) error = %v, want api.ErrNotFound", ident, err)
			}
			if _, err := svc.GetOutlinks(ctx, ident); !errors.Is(err, api.ErrNotFound) {
				t.Errorf("GetOutlinks(%q) error = %v, want api.ErrNotFound", ident, err)
			}
			if _, err := svc.GetRelated(ctx, ident, 10); !errors.Is(err, api.ErrNotFound) {
				t.Errorf("GetRelated(%q) error = %v, want api.ErrNotFound", ident, err)
			}
		})
	}
}

// TestGraphLookups_ExistingEntryWithNoLinksStaysEmpty is the other half of the
// pair. Making unknown identifiers an error must NOT turn a legitimately
// unlinked entry into one — that empty result is a real answer.
func TestGraphLookups_ExistingEntryWithNoLinksStaysEmpty(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	saved, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Genuinely Unlinked",
		Content: "Nothing links here and this links nowhere.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// By path and by short ID — both must resolve and both must be empty,
	// not errors.
	for _, ident := range []string{saved.Path, saved.ID} {
		back, err := svc.GetBacklinks(ctx, ident)
		if err != nil {
			t.Fatalf("GetBacklinks(%q) unexpected error: %v", ident, err)
		}
		if len(back) != 0 {
			t.Errorf("GetBacklinks(%q) = %d entries, want 0", ident, len(back))
		}
		out, err := svc.GetOutlinks(ctx, ident)
		if err != nil {
			t.Fatalf("GetOutlinks(%q) unexpected error: %v", ident, err)
		}
		if len(out) != 0 {
			t.Errorf("GetOutlinks(%q) = %d entries, want 0", ident, len(out))
		}
	}
}

// =============================================================================
// list(filename:) must see past one SQL page
// =============================================================================

// TestList_FilenameFilterSeesPastTheFirstPage is the regression test for a
// lookup-by-id that could not find entries which plainly exist. ListNotes
// applies LIMIT in SQL and the filename filter then runs in Go over that page —
// so asking for one specific entry searched only the first
// storage.DefaultListLimit rows. On the live store (~69k entries) that is a
// guaranteed miss for anything outside the first page, rendered as a confident
// "No entries found".
//
// Same shape as the automation_runs filter fixed in 73dd98d: filter applied
// after the limit rather than before it.
//
// Ordering is pinned by TITLE rather than left to the default modified-DESC.
// A tight creation loop stamps every entry with the same timestamp, so tie
// order is arbitrary and the needle can land inside the first page by luck —
// which made an earlier version of this test pass with the fix disabled.
func TestList_FilenameFilterSeesPastTheFirstPage(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	// Haystack sorts before the needle by title, and is larger than one page.
	for i := 0; i < storage.DefaultListLimit+20; i++ {
		if _, err := svc.Save(ctx, types.CreateEntryRequest{
			Type:    "note",
			Title:   fmt.Sprintf("aaa-haystack-%04d", i),
			Content: "noise",
		}); err != nil {
			t.Fatalf("Save %d failed: %v", i, err)
		}
	}
	needle, err := svc.Save(ctx, types.CreateEntryRequest{
		Type: "note", Title: "zzz-needle", Content: "find me",
	})
	if err != nil {
		t.Fatalf("Save needle failed: %v", err)
	}

	req := types.ListEntriesRequest{Filename: needle.ID, SortBy: "title", SortOrder: "asc"}

	// Guard against the test going vacuous: the needle must genuinely sit
	// outside the first page under this ordering, or it proves nothing.
	page, err := svc.List(ctx, types.ListEntriesRequest{SortBy: "title", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("List (page probe) failed: %v", err)
	}
	for _, e := range page.Entries {
		if e.ID == needle.ID {
			t.Fatalf("premise broken: the needle is inside the first page of %d, so this test cannot detect the bug", len(page.Entries))
		}
	}

	resp, err := svc.List(ctx, req)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("List(filename: %q) returned %d entries, want the one that exists", needle.ID, len(resp.Entries))
	}
	if resp.Entries[0].ID != needle.ID {
		t.Errorf("got entry %s, want %s", resp.Entries[0].ID, needle.ID)
	}
}

// TestList_WithoutFilenameFilterKeepsItsPageSize — the over-fetch must apply
// ONLY when a post-SQL filter needs it. An unfiltered list must still return one
// page, not a scan-window's worth.
func TestList_WithoutFilenameFilterKeepsItsPageSize(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	for i := 0; i < 15; i++ {
		if _, err := svc.Save(ctx, types.CreateEntryRequest{
			Type: "note", Title: fmt.Sprintf("Entry %d", i), Content: "x",
		}); err != nil {
			t.Fatalf("Save %d failed: %v", i, err)
		}
	}

	resp, err := svc.List(ctx, types.ListEntriesRequest{Limit: 5})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(resp.Entries) != 5 {
		t.Errorf("List(limit: 5) returned %d entries, want 5", len(resp.Entries))
	}
	if resp.Truncated {
		t.Error("an unfiltered list is not scan-truncated")
	}
}
