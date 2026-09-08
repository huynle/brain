package storage

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

// Content tests opt in; newTestStorage remains a raw owner for control tests.
func newTestContentStorage(t *testing.T) *TenantStore {
	t.Helper()
	s, err := newTestStorage(t).ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func migratedNoteStores(t *testing.T) (*StorageLayer, *TenantStore, *TenantStore) {
	t.Helper()
	db, _ := completeMigrationFixture(t)
	owner := &StorageLayer{db: db}
	// Bind before the migration: execution must consult the published schema,
	// not the schema that happened to exist when ForTenant was called.
	a, err := owner.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	b, err := owner.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.GetNoteByPath(context.Background(), "missing"); err == nil {
		t.Fatal("v28 foreign read accepted")
	}
	if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	relationalExec(t, tx, "INSERT INTO tenants(id,name,status,created_at) VALUES('acme','Acme','active','now')")
	const indexID = "0123456789abcdef0123456789abcdef"
	name, err := tenantFTSName(indexID)
	if err != nil {
		t.Fatal(err)
	}
	relationalExec(t, tx, tenantFTSTableDDL(name))
	// Test-only provisioning; production mapping remains immutable.
	relationalExec(t, tx, "DROP TRIGGER p4_fts_map_insert")
	relationalExec(t, tx, "INSERT INTO tenant_fts(tenant_id,internal_id) VALUES('acme','"+indexID+"')")
	mapping, err := readTenantFTSMapping(tx)
	if err != nil {
		t.Fatal(err)
	}
	for name, ddl := range tenantFTSTriggers(mapping) {
		relationalExec(t, tx, "DROP TRIGGER IF EXISTS "+name)
		relationalExec(t, tx, ddl)
	}
	if err := checkFinalTenantSearchSchema(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return owner, a, b
}

func TestTenantNotesIdenticalPathRead(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	for _, id := range []string{"local", "acme"} {
		if _, err := owner.db.Exec("INSERT INTO notes(tenant_id,path,short_id,title) VALUES(?,?,?,?)", id, "projects/shared/note/same.md", "same0001", id); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []*TenantStore{a, b} {
		n, err := s.GetNoteByPath(context.Background(), "projects/shared/note/same.md")
		if err != nil || n == nil || n.Title != s.TenantID().String() {
			t.Fatalf("tenant %s read = %+v, %v", s.TenantID(), n, err)
		}
	}
}

func TestTenantNotesCRUDIsolation(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	const path = "projects/shared/note/crud.md"
	for _, s := range []*TenantStore{a, b} {
		n, err := s.InsertNote(ctx, sampleNote(path, "crud0001", s.TenantID().String()))
		if err != nil || n == nil || n.Title != s.TenantID().String() {
			t.Fatalf("insert/readback: %+v %v", n, err)
		}
		if _, err := s.InsertNote(ctx, sampleNote(path, "crud0001", "duplicate")); err == nil {
			t.Fatal("accepted duplicate within tenant")
		}
	}
	if n, err := a.UpdateNote(ctx, path, map[string]interface{}{"title": "changed"}); err != nil || n == nil || n.Title != "changed" {
		t.Fatalf("update: %+v %v", n, err)
	}
	if n, err := a.MergeMetadata(ctx, path, map[string]interface{}{"status": "completed", "sessions": map[string]interface{}{"one": "a"}}); err != nil || n == nil || n.Status == nil || *n.Status != "completed" {
		t.Fatalf("merge: %+v %v", n, err)
	}
	if n, err := b.GetNoteByShortID(ctx, "crud0001"); err != nil || n == nil || n.Title != "acme" || n.Metadata != sampleNote(path, "", "").Metadata {
		t.Fatalf("foreign write: %+v %v", n, err)
	}
	if deleted, err := a.DeleteNote(ctx, path); err != nil || !deleted {
		t.Fatalf("delete: %v %v", deleted, err)
	}
	if n, err := a.GetNoteByPath(ctx, path); err != nil || n != nil {
		t.Fatalf("deleted read: %+v %v", n, err)
	}
	if n, err := b.GetNoteByPath(ctx, path); err != nil || n == nil || n.Title != "acme" {
		t.Fatalf("foreign delete: %+v %v", n, err)
	}
}

func TestTenantNotesTitleRanking(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	project := "shared"
	for _, s := range []*TenantStore{b, a} {
		for _, scope := range []string{"elsewhere", "", project} {
			n := sampleNote("projects/"+scope+"/rank.md", "rank0001", "Same title")
			if scope != "" {
				n.ProjectID = &scope
			} else {
				n.ProjectID = nil
			}
			if _, err := s.InsertNote(ctx, n); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range []*TenantStore{a, b} {
		n, err := s.GetNoteByTitleScoped(ctx, "Same title", &project)
		if err != nil || n == nil || n.ProjectID == nil || *n.ProjectID != project {
			t.Fatalf("project rank: %+v %v", n, err)
		}
		want, err := s.GetNoteByPath(ctx, n.Path)
		if err != nil || want == nil || n.ID != want.ID {
			t.Fatal("title crossed tenant")
		}
		n, err = s.GetNoteByTitle(ctx, "Same title")
		if err != nil || n == nil || n.ProjectID != nil {
			t.Fatalf("global rank: %+v %v", n, err)
		}
		want, err = s.GetNoteByPath(ctx, n.Path)
		if err != nil || want == nil || n.ID != want.ID {
			t.Fatal("global title crossed tenant")
		}
	}
}

func TestTenantNotesTitleFallbackIsolation(t *testing.T) {
	for _, ownTenant := range []string{"local", "acme"} {
		t.Run(ownTenant, func(t *testing.T) {
			for _, tc := range []struct {
				name        string
				ownProjects []string // Empty means a global note (NULL project_id).
			}{
				{"foreign preferred project vs own global", []string{""}},
				{"own other project only", []string{"elsewhere"}},
				{"preferred project tie lowest own ID", []string{"preferred", "preferred"}},
				{"global tie lowest own ID", []string{"", ""}},
				{"other project tie lowest own ID", []string{"z-other", "a-other"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					_, own, foreign := migratedNoteStores(t)
					if ownTenant == "acme" {
						own, foreign = foreign, own
					}
					ctx := context.Background()
					preferred := "preferred"
					const title = "Adversarial fallback title"
					insert := func(s *TenantStore, path, project string) *NoteRow {
						t.Helper()
						n := sampleNote(path, "fall0001", title)
						n.ProjectID = nil
						if project != "" {
							n.ProjectID = &project
						}
						got, err := s.InsertNote(ctx, n)
						if err != nil || got == nil {
							t.Fatalf("insert %s/%s: %+v, %v", s.TenantID(), path, got, err)
						}
						return got
					}
					// Foreign candidates have lower IDs and every possible rank.
					// Filtering only after LIMIT would lose the own fallback.
					insert(foreign, "projects/preferred/note/foreign.md", preferred)
					insert(foreign, "global/note/foreign.md", "")
					insert(foreign, "projects/elsewhere/note/foreign.md", "elsewhere")
					var want *NoteRow
					for i, project := range tc.ownProjects {
						// Reverse path ordering so lexicographic order is not ID order.
						path := "global/note/z-first.md"
						if i > 0 {
							path = "global/note/a-second.md"
						}
						if project != "" {
							path = "projects/" + project + "/note/" + path[len("global/note/"):]
						}
						n := insert(own, path, project)
						if want == nil {
							want = n
						} else if n.ID <= want.ID {
							t.Fatal("fixture must insert the lowest own ID first")
						}
					}
					for _, lookup := range []struct {
						name string
						get  func() (*NoteRow, error)
					}{
						{"scoped", func() (*NoteRow, error) { return own.GetNoteByTitleScoped(ctx, title, &preferred) }},
						{"nil scope", func() (*NoteRow, error) { return own.GetNoteByTitleScoped(ctx, title, nil) }},
						{"title", func() (*NoteRow, error) { return own.GetNoteByTitle(ctx, title) }},
					} {
						t.Run(lookup.name, func(t *testing.T) {
							got, err := lookup.get()
							if err != nil || got == nil || got.ID != want.ID || got.Path != want.Path {
								t.Fatalf("title lookup = %+v, %v; want own ID %d path %q", got, err, want.ID, want.Path)
							}
						})
					}
				})
			}
		})
	}
}

func TestTenantNotesForeignOnlyLookupsNotFound(t *testing.T) {
	for _, ownTenant := range []string{"local", "acme"} {
		t.Run(ownTenant, func(t *testing.T) {
			_, own, foreign := migratedNoteStores(t)
			if ownTenant == "acme" {
				own, foreign = foreign, own
			}
			ctx := context.Background()
			project := "preferred"
			n := sampleNote("projects/preferred/note/foreign-only.md", "foreign1", "Foreign only title")
			n.ProjectID = &project
			inserted, err := foreign.InsertNote(ctx, n)
			if err != nil || inserted == nil {
				t.Fatalf("insert foreign fixture: %+v, %v", inserted, err)
			}
			for _, tc := range []struct {
				name string
				get  func(*TenantStore) (*NoteRow, error)
			}{
				{"path", func(s *TenantStore) (*NoteRow, error) { return s.GetNoteByPath(ctx, n.Path) }},
				{"short ID", func(s *TenantStore) (*NoteRow, error) { return s.GetNoteByShortID(ctx, n.ShortID) }},
				{"title", func(s *TenantStore) (*NoteRow, error) { return s.GetNoteByTitle(ctx, n.Title) }},
				{"scoped title", func(s *TenantStore) (*NoteRow, error) { return s.GetNoteByTitleScoped(ctx, n.Title, &project) }},
				{"nil scoped title", func(s *TenantStore) (*NoteRow, error) { return s.GetNoteByTitleScoped(ctx, n.Title, nil) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got, err := tc.get(foreign); err != nil || got == nil || got.ID != inserted.ID {
						t.Fatalf("owner positive control = %+v, %v; want ID %d", got, err, inserted.ID)
					}
					if got, err := tc.get(own); err != nil || got != nil {
						t.Fatalf("foreign-only lookup = %+v, %v; want nil, nil", got, err)
					}
				})
			}
		})
	}
}

func TestTenantNotesListFilters(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, s := range []*TenantStore{a, b} {
		n, err := s.InsertNote(ctx, sampleNote("projects/shared/note/list.md", "list0001", s.TenantID().String()))
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range []string{"common", s.TenantID().String()} {
			if _, err := owner.db.Exec("INSERT INTO tags(tenant_id,note_id,tag) VALUES(?,?,?)", s.TenantID().String(), n.ID, tag); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, s := range []*TenantStore{a, b} {
		for _, opts := range []*ListOptions{{PathPrefix: "projects/shared/"}, {Tag: s.TenantID().String()}, {Tags: []string{"common", s.TenantID().String()}}} {
			notes, err := s.ListNotes(ctx, opts)
			if err != nil || len(notes) != 1 || notes[0].Title != s.TenantID().String() {
				t.Fatalf("list %+v: %+v %v", opts, notes, err)
			}
		}
	}
}

func TestTenantNotesExecutionSchemaGuard(t *testing.T) {
	ctx := context.Background()
	owner := newTestStorage(t)
	local, _ := owner.ForTenant(tenant.Local)
	foreign, _ := owner.ForTenant(tenant.MustParse("acme"))
	// Bound on v28: routing must not be cached on the handle.
	if _, err := local.GetNoteByPath(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{28, 0, 27, 29, 30} {
		relationalExec(t, owner.db, "DELETE FROM schema_version")
		if _, err := owner.db.Exec("INSERT INTO schema_version(version) VALUES(?)", version); err != nil {
			t.Fatal(err)
		}
		handles := []*TenantStore{foreign}
		if version != 28 {
			handles = append(handles, local)
		}
		for _, s := range handles {
			assertNoteOperationsDenied(t, s, ctx)
		}
	}
	relationalExec(t, owner.db, "DROP TABLE schema_version")
	assertNoteOperationsDenied(t, local, ctx)
	assertNoteOperationsDenied(t, local, nil)
	closed := newTestContentStorage(t)
	if err := closed.db.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoteOperationsDenied(t, closed, ctx)
	for _, s := range []*TenantStore{nil, {}, {tenantID: tenant.Local}, {StorageLayer: &StorageLayer{}, tenantID: tenant.Local}} {
		assertNoteOperationsDenied(t, s, ctx)
	}
}

func TestTenantNotesLegacyBridgesRefuseV29(t *testing.T) {
	owner, _, _ := migratedNoteStores(t)
	ctx := context.Background()
	if h, err := legacyLocalContent(ctx, owner); err == nil || h != nil {
		t.Fatal("v29 bridge bound local")
	}
	if _, err := legacyNoteByPath(ctx, owner, "missing"); err == nil {
		t.Fatal("v29 path bridge accepted")
	}
	if err := owner.SetLinks(ctx, "missing", nil); err == nil {
		t.Fatal("v29 SetLinks accepted")
	}
	if _, err := owner.GetLinks(ctx, "missing"); err == nil {
		t.Fatal("v29 GetLinks accepted")
	}
	if err := owner.SetTags(ctx, "missing", nil); err == nil {
		t.Fatal("v29 SetTags accepted")
	}
	if _, err := owner.GetTags(ctx, "missing"); err == nil {
		t.Fatal("v29 GetTags accepted")
	}
	if err := owner.LinkAttachmentToEntry(ctx, "missing", 1, "inline"); err == nil {
		t.Fatal("v29 attachment link accepted")
	}
	if _, err := owner.UnlinkAttachmentFromEntry(ctx, "missing", 1, "inline"); err == nil {
		t.Fatal("v29 attachment unlink accepted")
	}
	if _, err := owner.ListAttachmentsForEntry(ctx, "missing"); err == nil {
		t.Fatal("v29 attachment list accepted")
	}
	if err := owner.ActivateTask(ctx, "missing", nil); err == nil {
		t.Fatal("v29 activation accepted")
	}
}

// Simulate damaged child ownership with FK checks disabled only during fixture
// injection. Queries must still constrain each child table and source join, not
// rely on globally unique note IDs or intact foreign keys as tenant predicates.
func TestTenantNotesCorruptChildOwnership(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	local, err := a.InsertNote(ctx, sampleNote("projects/shared/child.md", "child001", "local"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := b.InsertNote(ctx, sampleNote("projects/shared/child.md", "child001", "foreign"))
	if err != nil {
		t.Fatal(err)
	}
	relationalExec(t, owner.db, "PRAGMA foreign_keys=OFF")
	if _, err := owner.db.Exec("INSERT INTO tags(tenant_id,note_id,tag) VALUES('acme',?,'foreign-tag')", local.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec("INSERT INTO links(tenant_id,source_id,target_path,type,href) VALUES('local',?,'projects/shared/repair.md','wiki','projects/shared/repair.md')", foreign.ID); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, owner.db, "PRAGMA foreign_keys=ON")
	for _, opts := range []*ListOptions{{Tag: "foreign-tag"}, {Tags: []string{"foreign-tag"}}} {
		notes, err := a.ListNotes(ctx, opts)
		if err != nil || len(notes) != 0 {
			t.Fatalf("foreign child tag selected notes: %+v %v", notes, err)
		}
	}
	if _, err := a.InsertNote(ctx, sampleNote("projects/shared/repair.md", "repair01", "Repair")); err != nil {
		t.Fatal(err)
	}
	var resolved int
	if err := owner.db.QueryRow("SELECT count(*) FROM links WHERE tenant_id='local' AND source_id=? AND target_id IS NOT NULL", foreign.ID).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	if resolved != 0 {
		t.Fatal("repair captured foreign source")
	}
}

func TestTenantNotesInsertRepair(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	project := "shared"
	for _, s := range []*TenantStore{a, b} {
		for _, p := range []string{project, "other"} {
			source := sampleNote("projects/"+p+"/source.md", "source01", "source")
			source.ProjectID = &p
			n, err := s.InsertNote(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			for _, target := range []string{"projects/shared/target.md", "target01", "target01.md", "Target"} {
				if _, err := owner.db.Exec("INSERT INTO links(tenant_id,source_id,target_path,type,href) VALUES(?,?,?,?,?)", s.TenantID().String(), n.ID, target, LinkTypeWiki, target); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := owner.db.Exec("INSERT INTO links(tenant_id,source_id,target_path,type,href) VALUES(?,?,?,?,?)", s.TenantID().String(), n.ID, "Target", LinkTypeMarkdown, "Target"); err != nil {
				t.Fatal(err)
			}
		}
	}
	target := sampleNote("projects/shared/target.md", "target01", "Target")
	target.ProjectID = &project
	n, err := a.InsertNote(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	var local, foreign int
	if err := owner.db.QueryRow("SELECT count(*) FROM links WHERE tenant_id='local' AND target_id=?", n.ID).Scan(&local); err != nil {
		t.Fatal(err)
	}
	if err := owner.db.QueryRow("SELECT count(*) FROM links WHERE tenant_id='acme' AND target_id IS NOT NULL").Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if local != 7 || foreign != 0 {
		t.Fatalf("repair local=%d want 7, foreign=%d want 0", local, foreign)
	}
}

func assertNoteOperationsDenied(t *testing.T, s *TenantStore, ctx context.Context) {
	t.Helper()
	ops := map[string]func() error{
		"insert":         func() error { _, err := s.InsertNote(ctx, nil); return err },
		"path":           func() error { _, err := s.GetNoteByPath(ctx, ""); return err },
		"short":          func() error { _, err := s.GetNoteByShortID(ctx, ""); return err },
		"title":          func() error { _, err := s.GetNoteByTitle(ctx, ""); return err },
		"scoped title":   func() error { _, err := s.GetNoteByTitleScoped(ctx, "", nil); return err },
		"merge":          func() error { _, err := s.MergeMetadata(ctx, "", nil); return err },
		"empty update":   func() error { _, err := s.UpdateNote(ctx, "", nil); return err },
		"invalid update": func() error { _, err := s.UpdateNote(ctx, "", map[string]interface{}{"tenant_id": "acme"}); return err },
		"delete":         func() error { _, err := s.DeleteNote(ctx, ""); return err },
		"list":           func() error { _, err := s.ListNotes(ctx, nil); return err },
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			if err := op(); err == nil {
				t.Fatal("invalid execution accepted")
			}
		})
	}
}
