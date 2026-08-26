package storage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// SetLinks
// ---------------------------------------------------------------------------

func TestSetLinks_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/linked.md", "lnk12345", "Linked Note")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	links := []LinkInput{
		{TargetPath: "other/note.md", Href: "other/note.md", Title: "Other Note", Type: "markdown", Snippet: "see also"},
		{TargetPath: "another/note.md", Href: "another/note.md"},
	}
	err = s.SetLinks(ctx, "projects/test/plan/linked.md", links)
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/linked.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d links, want 2", len(got))
	}

	// First link should have all fields.
	if got[0].TargetPath != "other/note.md" {
		t.Errorf("link[0].TargetPath = %q, want %q", got[0].TargetPath, "other/note.md")
	}
	if got[0].Title != "Other Note" {
		t.Errorf("link[0].Title = %q, want %q", got[0].Title, "Other Note")
	}
	if got[0].Type != "markdown" {
		t.Errorf("link[0].Type = %q, want %q", got[0].Type, "markdown")
	}
	if got[0].Snippet != "see also" {
		t.Errorf("link[0].Snippet = %q, want %q", got[0].Snippet, "see also")
	}

	// Second link should have defaults for empty fields.
	if got[1].TargetPath != "another/note.md" {
		t.Errorf("link[1].TargetPath = %q, want %q", got[1].TargetPath, "another/note.md")
	}
	if got[1].Type != "markdown" {
		t.Errorf("link[1].Type = %q, want %q (default)", got[1].Type, "markdown")
	}
}

func TestSetLinks_ReplacesExisting(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/replace-links.md", "rpl12345", "Replace Links")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set initial links.
	err = s.SetLinks(ctx, "projects/test/plan/replace-links.md", []LinkInput{
		{TargetPath: "old/link.md", Href: "old/link.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks (initial) failed: %v", err)
	}

	// Replace with new links.
	err = s.SetLinks(ctx, "projects/test/plan/replace-links.md", []LinkInput{
		{TargetPath: "new/link1.md", Href: "new/link1.md"},
		{TargetPath: "new/link2.md", Href: "new/link2.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks (replace) failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/replace-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d links, want 2", len(got))
	}
	if got[0].TargetPath != "new/link1.md" {
		t.Errorf("link[0].TargetPath = %q, want %q", got[0].TargetPath, "new/link1.md")
	}
	if got[1].TargetPath != "new/link2.md" {
		t.Errorf("link[1].TargetPath = %q, want %q", got[1].TargetPath, "new/link2.md")
	}
}

func TestSetLinks_ClearWithEmptySlice(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/clear-links.md", "clr12345", "Clear Links")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set some links first.
	err = s.SetLinks(ctx, "projects/test/plan/clear-links.md", []LinkInput{
		{TargetPath: "some/path.md", Href: "some/path.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	// Clear with empty slice.
	err = s.SetLinks(ctx, "projects/test/plan/clear-links.md", []LinkInput{})
	if err != nil {
		t.Fatalf("SetLinks (clear) failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/clear-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 links after clear, got %d", len(got))
	}
}

func TestSetLinks_NoteNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.SetLinks(ctx, "nonexistent/path.md", []LinkInput{
		{TargetPath: "any.md", Href: "any.md"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent note, got nil")
	}
}

func TestSetLinks_TargetResolution_Exists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create source note.
	source := sampleNote("projects/test/plan/source.md", "src12345", "Source")
	_, err := s.InsertNote(ctx, source)
	if err != nil {
		t.Fatalf("InsertNote (source) failed: %v", err)
	}

	// Create target note.
	target := sampleNote("projects/test/plan/target.md", "tgt12345", "Target")
	insertedTarget, err := s.InsertNote(ctx, target)
	if err != nil {
		t.Fatalf("InsertNote (target) failed: %v", err)
	}

	// Set link from source to target.
	err = s.SetLinks(ctx, "projects/test/plan/source.md", []LinkInput{
		{TargetPath: "projects/test/plan/target.md", Href: "projects/test/plan/target.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/source.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d links, want 1", len(got))
	}

	// target_id should be resolved to the target note's ID.
	if got[0].TargetID == nil {
		t.Fatal("expected TargetID to be set (target exists), got nil")
	}
	if *got[0].TargetID != insertedTarget.ID {
		t.Errorf("TargetID = %d, want %d", *got[0].TargetID, insertedTarget.ID)
	}
}

func TestSetLinks_TargetResolution_NotExists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create source note only — no target.
	source := sampleNote("projects/test/plan/source-only.md", "sro12345", "Source Only")
	_, err := s.InsertNote(ctx, source)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set link to a non-existent target.
	err = s.SetLinks(ctx, "projects/test/plan/source-only.md", []LinkInput{
		{TargetPath: "nonexistent/target.md", Href: "nonexistent/target.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/source-only.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d links, want 1", len(got))
	}

	// target_id should be nil (target doesn't exist).
	if got[0].TargetID != nil {
		t.Errorf("expected TargetID to be nil (target doesn't exist), got %d", *got[0].TargetID)
	}
	// target_path should still be set.
	if got[0].TargetPath != "nonexistent/target.md" {
		t.Errorf("TargetPath = %q, want %q", got[0].TargetPath, "nonexistent/target.md")
	}
}

// ---------------------------------------------------------------------------
// GetLinks
// ---------------------------------------------------------------------------

func TestGetLinks_WithLinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/get-links.md", "gtl12345", "Get Links")
	inserted, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Insert links directly to test GetLinks in isolation.
	_, err = s.DB().ExecContext(ctx,
		"INSERT INTO links (source_id, target_path, href, title, type, snippet) VALUES (?, ?, ?, ?, ?, ?)",
		inserted.ID, "some/path.md", "some/path.md", "Some Note", "markdown", "snippet text",
	)
	if err != nil {
		t.Fatalf("insert link failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/get-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d links, want 1", len(got))
	}
	if got[0].TargetPath != "some/path.md" {
		t.Errorf("TargetPath = %q, want %q", got[0].TargetPath, "some/path.md")
	}
	if got[0].Title != "Some Note" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Some Note")
	}
}

func TestGetLinks_NoLinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/no-links.md", "nol12345", "No Links")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/no-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 links, got %d", len(got))
	}
}

func TestGetLinks_NoteNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.GetLinks(ctx, "nonexistent/path.md")
	if err == nil {
		t.Fatal("expected error for nonexistent note, got nil")
	}
}

func TestSetLinks_TargetResolution_ShortID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	source := sampleNote("projects/test/plan/idsource.md", "ids12345", "ID Source")
	if _, err := s.InsertNote(ctx, source); err != nil {
		t.Fatalf("InsertNote (source) failed: %v", err)
	}
	target := sampleNote("projects/test/plan/idtarget.md", "idt12345", "ID Target")
	insertedTarget, err := s.InsertNote(ctx, target)
	if err != nil {
		t.Fatalf("InsertNote (target) failed: %v", err)
	}

	// The API's link formatter emits "[Title](idt12345)" — bare short ID.
	err = s.SetLinks(ctx, "projects/test/plan/idsource.md", []LinkInput{
		{TargetPath: "idt12345", Href: "idt12345"},
		{TargetPath: "idt12345.md", Href: "idt12345.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/idsource.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d links, want 2", len(got))
	}
	for i, l := range got {
		if l.TargetID == nil {
			t.Fatalf("link[%d]: expected TargetID resolved via short ID, got nil", i)
		}
		if *l.TargetID != insertedTarget.ID {
			t.Errorf("link[%d].TargetID = %d, want %d", i, *l.TargetID, insertedTarget.ID)
		}
	}

	// Short-ID-resolved links must show up as backlinks of the target.
	backs, err := s.GetBacklinks(ctx, "projects/test/plan/idtarget.md")
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if len(backs) != 1 || backs[0].Path != "projects/test/plan/idsource.md" {
		t.Errorf("GetBacklinks = %+v, want the ID source note", backs)
	}
}

func TestShortIDFromHref(t *testing.T) {
	cases := map[string]string{
		"n8eox9v4":                     "n8eox9v4",
		"n8eox9v4.md":                  "n8eox9v4",
		"projects/x/plan/n8eox9v4.md":  "",
		"https://example.com/aaaaaaaa": "",
		"UPPERCASE":                    "",
		"short":                        "",
		"toolongid9":                   "",
		"#anchor12":                    "",
	}
	for in, want := range cases {
		if got := shortIDFromHref(in); got != want {
			t.Errorf("shortIDFromHref(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInsertNote_RepairsDanglingLinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Source is indexed FIRST, with links to a target that doesn't exist
	// yet (WalkDir order during a bulk reindex).
	source := sampleNote("projects/test/plan/early.md", "ear12345", "Early Source")
	if _, err := s.InsertNote(ctx, source); err != nil {
		t.Fatalf("InsertNote (source) failed: %v", err)
	}
	err := s.SetLinks(ctx, "projects/test/plan/early.md", []LinkInput{
		{TargetPath: "projects/test/plan/late.md", Href: "projects/test/plan/late.md"},
		{TargetPath: "lat12345", Href: "lat12345"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	// Both links dangle until the target is inserted.
	links, err := s.GetLinks(ctx, "projects/test/plan/early.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	for i, l := range links {
		if l.TargetID != nil {
			t.Fatalf("link[%d]: expected dangling before target insert", i)
		}
	}

	// Inserting the target repairs both hrefs.
	target := sampleNote("projects/test/plan/late.md", "lat12345", "Late Target")
	insertedTarget, err := s.InsertNote(ctx, target)
	if err != nil {
		t.Fatalf("InsertNote (target) failed: %v", err)
	}
	links, err = s.GetLinks(ctx, "projects/test/plan/early.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	for i, l := range links {
		if l.TargetID == nil || *l.TargetID != insertedTarget.ID {
			t.Errorf("link[%d]: expected repaired TargetID %d, got %v", i, insertedTarget.ID, l.TargetID)
		}
	}

	backs, err := s.GetBacklinks(ctx, "projects/test/plan/late.md")
	if err != nil {
		t.Fatalf("GetBacklinks failed: %v", err)
	}
	if len(backs) != 1 || backs[0].Path != "projects/test/plan/early.md" {
		t.Errorf("GetBacklinks after repair = %+v, want the early source", backs)
	}
}

// ---------------------------------------------------------------------------
// Wiki-link resolution (task 5t174je3)
//
// Wiki-links are title-addressed, so SetLinks resolves them by path, then
// short ID, then title. Markdown hrefs deliberately stop before the title step.
// ---------------------------------------------------------------------------

// noteIn returns a sample note pinned to a project, or to global scope when
// project is "".
func noteIn(path, shortID, title, project string) *NoteRow {
	n := sampleNote(path, shortID, title)
	if project == "" {
		n.ProjectID = nil
	} else {
		p := project
		n.ProjectID = &p
	}
	return n
}

func wikiLink(target string) LinkInput {
	return LinkInput{TargetPath: target, Href: target, Title: target, Type: LinkTypeWiki}
}

// targetIDOf returns the resolved target id of the single link on notePath.
func targetIDOf(t *testing.T, s *StorageLayer, notePath string) *int64 {
	t.Helper()
	links, err := s.GetLinks(context.Background(), notePath)
	if err != nil {
		t.Fatalf("GetLinks(%q): %v", notePath, err)
	}
	if len(links) != 1 {
		t.Fatalf("GetLinks(%q): got %d links, want 1", notePath, len(links))
	}
	return links[0].TargetID
}

func TestSetLinks_ResolvesWikiLinkByTitle(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	target, err := s.InsertNote(ctx, noteIn("projects/p/plan/target.md", "wkitgt01", "Target Note", "p"))
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}
	if _, err := s.InsertNote(ctx, noteIn("projects/p/plan/source.md", "wkisrc01", "Source Note", "p")); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	if err := s.SetLinks(ctx, "projects/p/plan/source.md", []LinkInput{wikiLink("Target Note")}); err != nil {
		t.Fatalf("SetLinks: %v", err)
	}

	got := targetIDOf(t, s, "projects/p/plan/source.md")
	if got == nil {
		t.Fatal("wiki-link by title did not resolve (target_id is NULL)")
	}
	if *got != target.ID {
		t.Errorf("target_id = %d, want %d", *got, target.ID)
	}
}

// A wiki-link may also name a path or a short ID; those steps run before the
// title lookup, so all three shapes work without the author declaring which.
func TestSetLinks_WikiLinkResolvesPathAndShortID(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	target, err := s.InsertNote(ctx, noteIn("projects/p/plan/target.md", "wkitgt02", "Target Note", "p"))
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}

	for name, href := range map[string]string{
		"path":       "projects/p/plan/target.md",
		"short_id":   "wkitgt02",
		"short_id_m": "wkitgt02.md",
	} {
		t.Run(name, func(t *testing.T) {
			path := "projects/p/plan/src-" + name + ".md"
			if _, err := s.InsertNote(ctx, noteIn(path, "wkis"+name[:4], "Src "+name, "p")); err != nil {
				t.Fatalf("insert source: %v", err)
			}
			if err := s.SetLinks(ctx, path, []LinkInput{wikiLink(href)}); err != nil {
				t.Fatalf("SetLinks: %v", err)
			}
			got := targetIDOf(t, s, path)
			if got == nil || *got != target.ID {
				t.Errorf("target_id = %v, want %d", got, target.ID)
			}
		})
	}
}

func TestSetLinks_WikiLinkPrefersSameProject(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Same title in three scopes. The link is written from project "mine".
	other, err := s.InsertNote(ctx, noteIn("projects/other/plan/dup.md", "duptgt01", "Shared Title", "other"))
	if err != nil {
		t.Fatalf("insert other: %v", err)
	}
	global, err := s.InsertNote(ctx, noteIn("global/plan/dup.md", "duptgt02", "Shared Title", ""))
	if err != nil {
		t.Fatalf("insert global: %v", err)
	}
	mine, err := s.InsertNote(ctx, noteIn("projects/mine/plan/dup.md", "duptgt03", "Shared Title", "mine"))
	if err != nil {
		t.Fatalf("insert mine: %v", err)
	}

	if _, err := s.InsertNote(ctx, noteIn("projects/mine/plan/src.md", "dupsrc01", "Source", "mine")); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := s.SetLinks(ctx, "projects/mine/plan/src.md", []LinkInput{wikiLink("Shared Title")}); err != nil {
		t.Fatalf("SetLinks: %v", err)
	}

	got := targetIDOf(t, s, "projects/mine/plan/src.md")
	if got == nil {
		t.Fatal("wiki-link did not resolve")
	}
	switch *got {
	case mine.ID: // correct
	case global.ID:
		t.Error("resolved to the global entry; the same-project entry should win")
	case other.ID:
		t.Error("resolved into another project; the same-project entry should win")
	default:
		t.Errorf("resolved to unexpected id %d", *got)
	}
}

func TestSetLinks_WikiLinkFallsBackToGlobal(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	other, err := s.InsertNote(ctx, noteIn("projects/other/plan/dup.md", "gbltgt01", "Shared Title", "other"))
	if err != nil {
		t.Fatalf("insert other: %v", err)
	}
	global, err := s.InsertNote(ctx, noteIn("global/plan/dup.md", "gbltgt02", "Shared Title", ""))
	if err != nil {
		t.Fatalf("insert global: %v", err)
	}
	if _, err := s.InsertNote(ctx, noteIn("projects/mine/plan/src.md", "gblsrc01", "Source", "mine")); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := s.SetLinks(ctx, "projects/mine/plan/src.md", []LinkInput{wikiLink("Shared Title")}); err != nil {
		t.Fatalf("SetLinks: %v", err)
	}

	got := targetIDOf(t, s, "projects/mine/plan/src.md")
	if got == nil {
		t.Fatal("wiki-link did not resolve")
	}
	if *got == other.ID {
		t.Error("resolved into an unrelated project; the global entry should win")
	}
	if *got != global.ID {
		t.Errorf("target_id = %d, want the global entry %d", *got, global.ID)
	}
}

// A markdown href names a location, not a title. Resolving it by title would
// bind syntax examples like "[see the plan](plan-id)" to any entry that happens
// to be titled "plan-id".
func TestSetLinks_MarkdownHrefIsNotResolvedByTitle(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if _, err := s.InsertNote(ctx, noteIn("projects/p/plan/decoy.md", "mdtgt001", "plan-id", "p")); err != nil {
		t.Fatalf("insert decoy: %v", err)
	}
	if _, err := s.InsertNote(ctx, noteIn("projects/p/plan/src.md", "mdsrc001", "Source", "p")); err != nil {
		t.Fatalf("insert source: %v", err)
	}

	link := LinkInput{TargetPath: "plan-id", Href: "plan-id", Title: "see the plan", Type: LinkTypeMarkdown}
	if err := s.SetLinks(ctx, "projects/p/plan/src.md", []LinkInput{link}); err != nil {
		t.Fatalf("SetLinks: %v", err)
	}

	if got := targetIDOf(t, s, "projects/p/plan/src.md"); got != nil {
		t.Errorf("markdown href resolved by title to id %d; it should stay unresolved", *got)
	}
}

// ---------------------------------------------------------------------------
// ResolveLinksToNote back-fill
// ---------------------------------------------------------------------------

func TestResolveLinksToNote_BackfillsWikiLinkByTitle(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Source written first: the target does not exist yet, so the link dangles.
	if _, err := s.InsertNote(ctx, noteIn("projects/p/plan/src.md", "bfsrc001", "Source", "p")); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := s.SetLinks(ctx, "projects/p/plan/src.md", []LinkInput{wikiLink("Arrives Later")}); err != nil {
		t.Fatalf("SetLinks: %v", err)
	}
	if got := targetIDOf(t, s, "projects/p/plan/src.md"); got != nil {
		t.Fatalf("link resolved before the target existed: %d", *got)
	}

	// Inserting the target must repair it.
	target, err := s.InsertNote(ctx, noteIn("projects/p/plan/later.md", "bftgt001", "Arrives Later", "p"))
	if err != nil {
		t.Fatalf("insert target: %v", err)
	}
	got := targetIDOf(t, s, "projects/p/plan/src.md")
	if got == nil {
		t.Fatal("dangling wiki-link was not back-filled when its target appeared")
	}
	if *got != target.ID {
		t.Errorf("target_id = %d, want %d", *got, target.ID)
	}
}

func TestResolveLinksToNote_TitleBackfillDoesNotCrossProjects(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if _, err := s.InsertNote(ctx, noteIn("projects/mine/plan/src.md", "xpsrc001", "Source", "mine")); err != nil {
		t.Fatalf("insert source: %v", err)
	}
	if err := s.SetLinks(ctx, "projects/mine/plan/src.md", []LinkInput{wikiLink("Summary")}); err != nil {
		t.Fatalf("SetLinks: %v", err)
	}

	// A same-titled entry appearing in an unrelated project must not claim it.
	if _, err := s.InsertNote(ctx, noteIn("projects/other/plan/sum.md", "xptgt001", "Summary", "other")); err != nil {
		t.Fatalf("insert other-project target: %v", err)
	}
	if got := targetIDOf(t, s, "projects/mine/plan/src.md"); got != nil {
		t.Fatalf("a same-titled entry in another project captured the link (id %d)", *got)
	}

	// A global entry is visible from everywhere, so it may.
	global, err := s.InsertNote(ctx, noteIn("global/plan/sum.md", "xptgt002", "Summary", ""))
	if err != nil {
		t.Fatalf("insert global target: %v", err)
	}
	got := targetIDOf(t, s, "projects/mine/plan/src.md")
	if got == nil || *got != global.ID {
		t.Errorf("target_id = %v, want the global entry %d", got, global.ID)
	}
}
