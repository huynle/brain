package service

import (
	"context"
	"strings"
	"testing"
	"time"

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

	resp, err := svc.GetStats(ctx, false)
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

	resp, err := svc.GetStats(ctx, true)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// Global filter should only count global entries
	if resp.TotalEntries != 1 {
		t.Errorf("expected 1 global entry, got %d", resp.TotalEntries)
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

	results, err := svc.GetOrphans(ctx, "", 50)
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

	results, err := svc.GetOrphans(ctx, "plan", 50)
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
	results, err := svc.GetOrphans(ctx, "", 0)
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

	results, err := svc.GetStale(ctx, 30, "", 50)
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

	results, err := svc.GetStale(ctx, 30, "plan", 50)
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
	results, err := svc.GetStale(ctx, 0, "", 0)
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
