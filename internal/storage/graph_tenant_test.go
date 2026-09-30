package storage

import (
	"context"
	"testing"
)

func TestTenantGraphForeignCorpus(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	target, err := a.InsertNote(ctx, sampleNote("projects/shared/target.md", "target01", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := b.InsertNote(ctx, sampleNote("projects/shared/source.md", "source01", "Foreign"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec("INSERT INTO links(tenant_id,source_id,target_path,href,type) VALUES('acme',?,?,?,'markdown')", source.ID, target.Path, target.Path); err != nil {
		t.Fatal(err)
	}
	back, err := a.GetBacklinks(ctx, target.Path)
	if err != nil || len(back) != 0 {
		t.Fatalf("foreign backlinks = %+v, %v; want empty", back, err)
	}
	orphans, err := a.GetOrphans(ctx, &OrphanOptions{Path: target.Path})
	if err != nil || len(orphans) != 1 || orphans[0].ID != target.ID {
		t.Fatalf("own orphan changed by foreign link: %+v, %v", orphans, err)
	}
}

func TestTenantLinksAndTagsRoundTrip(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, s := range []*TenantStore{b, a} {
		n, err := s.InsertNote(ctx, sampleNote("projects/shared/target.md", "target01", "Target"))
		if err != nil {
			t.Fatal(err)
		}
		const source = "projects/shared/source.md"
		if _, err := s.InsertNote(ctx, sampleNote(source, "source01", "Source")); err != nil {
			t.Fatal(err)
		}
		if err := s.SetLinks(ctx, source, []LinkInput{{TargetPath: "target01"}}); err != nil {
			t.Fatalf("tenant link write: %v", err)
		}
		links, err := s.GetLinks(ctx, source)
		if err != nil || len(links) != 1 || links[0].TargetID == nil || *links[0].TargetID != n.ID {
			t.Fatalf("tenant links: %+v, %v", links, err)
		}
		if err := s.SetTags(ctx, source, []string{s.TenantID().String()}); err != nil {
			t.Fatalf("tenant tag write: %v", err)
		}
		tags, err := s.GetTags(ctx, source)
		if err != nil || len(tags) != 1 || tags[0] != s.TenantID().String() {
			t.Fatalf("tenant tags: %v, %v", tags, err)
		}
	}
}

func TestTenantRepairRejectsForgedTarget(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	n, err := b.InsertNote(ctx, sampleNote("projects/shared/target.md", "target01", "Target"))
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ResolveLinksToNote(ctx, n.ID, n.Path, n.ShortID, n.Title, n.ProjectID); err == nil {
		t.Fatal("foreign repair target accepted")
	}
	if err := b.ResolveLinksToNote(ctx, n.ID, "forged.md", n.ShortID, n.Title, n.ProjectID); err == nil {
		t.Fatal("forged repair metadata accepted")
	}
}

func TestGraphOrphanLiteralPrefix(t *testing.T) {
	s := newTestContentStorage(t)
	ctx := context.Background()
	for _, path := range []string{"projects/a_b%\\/own.md", "projects/axbZZ/foreign.md"} {
		if _, err := s.InsertNote(ctx, sampleNote(path, "prefix01", "Prefix")); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := s.GetOrphans(ctx, &OrphanOptions{Path: "projects/a_b%\\/"})
	if err != nil || len(notes) != 1 || notes[0].Path != "projects/a_b%\\/own.md" {
		t.Fatalf("literal prefix: %+v, %v", notes, err)
	}
	// An underscore must not match an arbitrary character.
	notes, err = s.GetOrphans(ctx, &OrphanOptions{Path: "projects/a_b"})
	if err != nil || len(notes) != 1 {
		t.Fatalf("wildcard prefix escaped incorrectly: %+v, %v", notes, err)
	}
}
