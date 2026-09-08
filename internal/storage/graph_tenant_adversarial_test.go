package storage

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func graphNote(t *testing.T, s *TenantStore, path, shortID, title, project string) *NoteRow {
	t.Helper()
	n, err := s.InsertNote(context.Background(), noteIn(path, shortID, title, project))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func graphLinks(t *testing.T, s *TenantStore, path string, links ...LinkInput) {
	t.Helper()
	if err := s.SetLinks(context.Background(), path, links); err != nil {
		t.Fatal(err)
	}
}

func graphWantNotes(t *testing.T, got []*NoteRow, err error, want ...*NoteRow) {
	t.Helper()
	ids := func(notes []*NoteRow) []int64 {
		out := make([]int64, 0, len(notes))
		for _, n := range notes {
			out = append(out, n.ID)
		}
		slices.Sort(out)
		return out
	}
	if err != nil || got == nil || !slices.Equal(ids(got), ids(want)) {
		t.Fatalf("graph IDs = %v, %v; want %v (non-nil)", ids(got), err, ids(want))
	}
}

func TestTenantGraphExactForeignPaths(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, pair := range [][2]*TenantStore{{a, b}, {b, a}} {
		own, foreign := pair[0], pair[1]
		t.Run(own.TenantID().String(), func(t *testing.T) {
			target := graphNote(t, foreign, "projects/p/"+own.TenantID().String()+"-target.md", "foreign1", "Foreign only", "p")
			source := graphNote(t, own, "projects/p/"+own.TenantID().String()+"-source.md", "source01", "Source", "p")
			graphLinks(t, own, source.Path, LinkInput{TargetPath: target.Path})
			if id := targetIDOf(t, own, source.Path); id != nil {
				t.Fatalf("exact foreign path resolved to %d", *id)
			}
			got, err := own.GetOutlinks(ctx, source.Path)
			graphWantNotes(t, got, err)
			got, err = foreign.GetBacklinks(ctx, target.Path)
			graphWantNotes(t, got, err)
			// Raw unresolved backlinks remain useful within the source tenant.
			got, err = own.GetBacklinks(ctx, target.Path)
			graphWantNotes(t, got, err, source)
			for _, path := range []string{target.Path, "missing.md"} {
				for name, op := range map[string]func() error{
					"get links":   func() error { _, e := own.GetLinks(ctx, path); return e },
					"get tags":    func() error { _, e := own.GetTags(ctx, path); return e },
					"clear links": func() error { return own.SetLinks(ctx, path, nil) },
					"clear tags":  func() error { return own.SetTags(ctx, path, nil) },
				} {
					if err := op(); err == nil {
						t.Errorf("%s accepted absent own source %q", name, path)
					}
				}
			}
		})
	}
}

func TestTenantGraphIdenticalCorpusAndForeignMutation(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	corpora := map[*TenantStore][]*NoteRow{}
	for _, s := range []*TenantStore{b, a} { // Foreign IDs win any unscoped first-row lookup.
		for _, name := range []string{"target", "source", "by-id", "by-raw", "orphan"} {
			corpora[s] = append(corpora[s], graphNote(t, s, "projects/graph-corpus/"+name+".md", name+"01", name, "p"))
		}
		n := corpora[s]
		graphLinks(t, s, n[1].Path, LinkInput{TargetPath: n[0].Path}, LinkInput{TargetPath: "missing-target"})
		graphLinks(t, s, n[2].Path, LinkInput{TargetPath: n[0].ShortID})
		graphLinks(t, s, n[3].Path, LinkInput{TargetPath: "missing-target"})
	}
	n := corpora[a]
	for _, s := range []*TenantStore{a, b} {
		c := corpora[s]
		got, err := s.GetRelated(ctx, c[1].Path, 20)
		graphWantNotes(t, got, err, c[2], c[3]) // Different hrefs require the ID arm; dangling target requires raw arm.
		got, err = s.GetOutlinks(ctx, c[1].Path)
		graphWantNotes(t, got, err, c[0])
		got, err = s.GetBacklinks(ctx, c[0].Path)
		graphWantNotes(t, got, err, c[1], c[2])
	}
	opts := &OrphanOptions{Path: "projects/graph-corpus/"}
	baseline, err := a.GetOrphans(ctx, opts)
	graphWantNotes(t, baseline, err, n[1], n[2], n[3], n[4])
	for _, links := range [][]LinkInput{{{TargetPath: n[4].Path}}, {{TargetPath: n[1].Path}}, nil} {
		graphLinks(t, b, corpora[b][4].Path, links...)
		got, err := a.GetOrphans(ctx, opts)
		graphWantNotes(t, got, err, baseline...)
		var foreignWant []*NoteRow
		for _, candidate := range corpora[b][1:] {
			if len(links) == 0 || candidate.Path != links[0].TargetPath {
				foreignWant = append(foreignWant, candidate)
			}
		}
		got, err = b.GetOrphans(ctx, opts)
		graphWantNotes(t, got, err, foreignWant...)
	}
}

func TestTenantLinksResolutionPrecedence(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	var own []*NoteRow
	for _, s := range []*TenantStore{b, a} {
		var notes []*NoteRow
		for i, project := range []string{"other", "", "p"} {
			notes = append(notes, graphNote(t, s, fmt.Sprintf("rank/%d.md", i), fmt.Sprintf("rank000%d", i), "Shared title", project))
		}
		// A title that looks like an ID must lose to the ID; a path that looks
		// like an ID must win over both. All candidates also exist in B first.
		notes = append(notes, graphNote(t, s, "rank/title.md", "decoy001", "rank0002", "p"))
		notes = append(notes, graphNote(t, s, "rank0001", "path0001", "Path wins", "p"))
		graphNote(t, s, "source.md", "source01", "Source", "p")
		if s == a {
			own = notes
		}
	}
	for _, tc := range []struct {
		name string
		link LinkInput
		want *NoteRow
	}{
		{"path", wikiLink(own[2].Path), own[2]},
		{"ID before title", wikiLink("rank0002"), own[2]},
		{"ID md", wikiLink("rank0002.md"), own[2]},
		{"path before ID", wikiLink("rank0001"), own[4]},
		{"project title rank", wikiLink("Shared title"), own[2]},
		{"markdown not title", LinkInput{TargetPath: "Shared title", Type: LinkTypeMarkdown}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graphLinks(t, a, "source.md", tc.link)
			got := targetIDOf(t, a, "source.md")
			if tc.want == nil {
				if got != nil {
					t.Fatalf("unexpected target %d", *got)
				}
			} else if got == nil || *got != tc.want.ID {
				t.Fatalf("target = %v; want %d", got, tc.want.ID)
			}
		})
	}
	// Drop the own preferred candidate: foreign preferred candidates still
	// exist, but own global wins. Other-project fallback is already covered
	// comprehensively by TestTenantNotesTitleFallbackIsolation.
	if _, err := a.DeleteNote(ctx, own[2].Path); err != nil {
		t.Fatal(err)
	}
	graphLinks(t, a, "source.md", wikiLink("Shared title"))
	if got := targetIDOf(t, a, "source.md"); got == nil || *got != own[1].ID {
		t.Fatalf("global fallback = %v; want %d", got, own[1].ID)
	}
}

func TestTenantLinksDelayedGlobalRepairKeepsResolved(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	for _, s := range []*TenantStore{b, a} {
		for _, project := range []string{"p", "other", ""} {
			path := project + "/source.md"
			graphNote(t, s, path, "source01", "Source", project)
			graphLinks(t, s, path, wikiLink("Later"))
			if got := targetIDOf(t, s, path); got != nil {
				t.Fatalf("link resolved before target arrival: %d", *got)
			}
		}
	}
	// Cross-project restriction and all href repair shapes have coverage in
	// TestTenantNotesInsertRepair; here verify global capture and no overwrite.
	preferred := graphNote(t, a, "preferred.md", "prefer01", "Later", "p")
	for _, path := range []string{"other/source.md", "/source.md"} {
		if got := targetIDOf(t, a, path); got != nil {
			t.Fatalf("project target captured %q: %d", path, *got)
		}
	}
	global := graphNote(t, a, "global/later.md", "global01", "Later", "")
	for _, project := range []string{"p", "other", ""} {
		want := global.ID
		if project == "p" {
			want = preferred.ID
		}
		if got := targetIDOf(t, a, project+"/source.md"); got == nil || *got != want {
			t.Fatalf("%q repair = %v; want %d", project, got, want)
		}
		if got := targetIDOf(t, b, project+"/source.md"); got != nil {
			t.Fatalf("foreign link repaired: %d", *got)
		}
	}
}

func TestTenantLinksTagsReplacePreservesForeign(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, s := range []*TenantStore{b, a} {
		graphNote(t, s, "source.md", "source01", "Source", "p")
		graphLinks(t, s, "source.md", wikiLink("initial"))
		if err := s.SetTags(ctx, "source.md", []string{"initial"}); err != nil {
			t.Fatal(err)
		}
	}
	foreignLinks, err := b.GetLinks(ctx, "source.md")
	if err != nil {
		t.Fatal(err)
	}
	foreignTags, err := b.GetTags(ctx, "source.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, values := range [][]string{{"replacement", "second"}, {}, nil} {
		links := make([]LinkInput, 0, len(values))
		for _, v := range values {
			links = append(links, wikiLink(v))
		}
		graphLinks(t, a, "source.md", links...)
		if err := a.SetTags(ctx, "source.md", values); err != nil {
			t.Fatal(err)
		}
		gotLinks, err := a.GetLinks(ctx, "source.md")
		if err != nil || gotLinks == nil || len(gotLinks) != len(values) {
			t.Fatalf("own links = %+v, %v", gotLinks, err)
		}
		var paths []string
		for _, link := range gotLinks {
			paths = append(paths, link.TargetPath)
		}
		if !slices.Equal(paths, values) {
			t.Fatalf("replacement paths = %v; want %v", paths, values)
		}
		gotTags, err := a.GetTags(ctx, "source.md")
		if err != nil || gotTags == nil || !slices.Equal(gotTags, values) {
			t.Fatalf("own tags = %v, %v", gotTags, err)
		}
		gotLinks, err = b.GetLinks(ctx, "source.md")
		if err != nil || !reflect.DeepEqual(gotLinks, foreignLinks) {
			t.Fatalf("foreign links changed: %+v, %v", gotLinks, err)
		}
		gotTags, err = b.GetTags(ctx, "source.md")
		if err != nil || !reflect.DeepEqual(gotTags, foreignTags) {
			t.Fatalf("foreign tags changed: %v, %v", gotTags, err)
		}
	}
}

func graphOperations(s *TenantStore, ctx context.Context) map[string]func() error {
	return map[string]func() error{
		"backlinks":          func() error { _, e := s.GetBacklinks(ctx, ""); return e },
		"outlinks":           func() error { _, e := s.GetOutlinks(ctx, ""); return e },
		"related zero limit": func() error { _, e := s.GetRelated(ctx, "", 0); return e },
		"orphans":            func() error { _, e := s.GetOrphans(ctx, nil); return e },
		"get links":          func() error { _, e := s.GetLinks(ctx, ""); return e },
		"get tags":           func() error { _, e := s.GetTags(ctx, ""); return e },
		"nil links":          func() error { return s.SetLinks(ctx, "", nil) },
		"empty links":        func() error { return s.SetLinks(ctx, "", []LinkInput{}) },
		"nil tags":           func() error { return s.SetTags(ctx, "", nil) },
		"empty tags":         func() error { return s.SetTags(ctx, "", []string{}) },
		"repair wrapper":     func() error { return s.ResolveLinksTo(ctx, 0, "", "") },
		"repair full":        func() error { return s.ResolveLinksToNote(ctx, 0, "", "", "", nil) },
	}
}

func TestTenantGraphExecutionGuards(t *testing.T) {
	deny := func(t *testing.T, s *TenantStore, ctx context.Context, oldColumns bool) {
		t.Helper()
		_, guardErr := s.contentScope(ctx)
		var want string
		if oldColumns {
			// A v29 stamp selects scoped SQL, but cannot add tenant columns to
			// v28 tables. Require that precise failure, not a missing-note error.
			if guardErr != nil {
				t.Fatalf("v29 stamp should pass contentScope: %v", guardErr)
			}
			want = "no such column: tenant_id"
		} else {
			if guardErr == nil {
				t.Fatal("invalid fixture unexpectedly passed contentScope")
			}
			want = guardErr.Error()
		}
		for name, op := range graphOperations(s, ctx) {
			t.Run(name, func(t *testing.T) {
				if err := op(); err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("operation error = %v; want guard error containing %q", err, want)
				}
			})
		}
	}
	ctx := context.Background()
	for i, s := range []*TenantStore{nil, {}, {tenantID: tenant.Local}, {StorageLayer: &StorageLayer{}, tenantID: tenant.Local}} {
		t.Run(fmt.Sprintf("invalid handle %d", i), func(t *testing.T) { deny(t, s, ctx, false) })
	}
	owner := newTestStorage(t)
	local, _ := owner.ForTenant(tenant.Local)
	foreign, _ := owner.ForTenant(tenant.MustParse("acme"))
	t.Run("nonlocal v28", func(t *testing.T) { deny(t, foreign, ctx, false) })
	t.Run("nil context valid v28", func(t *testing.T) { deny(t, local, nil, false) })
	for _, version := range []int{0, 27, 29, 30} {
		relationalExec(t, owner.db, "DELETE FROM schema_version")
		if _, err := owner.db.Exec("INSERT INTO schema_version(version) VALUES(?)", version); err != nil {
			t.Fatal(err)
		}
		t.Run(fmt.Sprintf("version %d old columns", version), func(t *testing.T) {
			deny(t, local, ctx, version == 29)
			deny(t, foreign, ctx, version == 29)
		})
	}
	relationalExec(t, owner.db, "DELETE FROM schema_version")
	t.Run("missing stamp", func(t *testing.T) { deny(t, local, ctx, false) })
	relationalExec(t, owner.db, "DROP TABLE schema_version")
	t.Run("missing schema", func(t *testing.T) { deny(t, local, ctx, false) })
	_, a, _ := migratedNoteStores(t)
	t.Run("nil context valid v29", func(t *testing.T) { deny(t, a, nil, false) })
}

func TestTenantGraphLiteralPrefixes(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	for _, tc := range []struct{ prefix, sibling string }{
		{"projects/a%/", "projects/abc/"},
		{"projects/a_/", "projects/ab/"},
		{`projects/a\/`, "projects/a/"},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			graphNote(t, b, tc.prefix+"foreign.md", "prefix01", "Prefix", "p")
			graphNote(t, a, tc.sibling+"sibling.md", "prefix01", "Prefix", "p")
			want := graphNote(t, a, tc.prefix+"own.md", "prefix01", "Prefix", "p")
			got, err := a.GetOrphans(context.Background(), &OrphanOptions{Path: tc.prefix})
			graphWantNotes(t, got, err, want)
		})
	}
}

func TestTenantRepairMetadataAdmission(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	foreign := graphNote(t, b, "target.md", "target01", "Target", "p")
	own := graphNote(t, a, "target.md", "target01", "Target", "p")
	graphNote(t, a, "source.md", "source01", "Source", "p")
	graphLinks(t, a, "source.md", wikiLink("forged title"))
	for _, tc := range []struct {
		name   string
		target NoteRow
	}{
		{"foreign ID identical metadata", *foreign},
		{"forged path", func() NoteRow { n := *own; n.Path = "forged.md"; return n }()},
		{"forged short ID", func() NoteRow { n := *own; n.ShortID = "forged01"; return n }()},
		{"forged title", func() NoteRow { n := *own; n.Title = "forged title"; return n }()},
		{"forged global", func() NoteRow { n := *own; n.ProjectID = nil; return n }()},
		{"forged project", func() NoteRow { n := *own; p := "other"; n.ProjectID = &p; return n }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.target
			if err := a.ResolveLinksToNote(ctx, n.ID, n.Path, n.ShortID, n.Title, n.ProjectID); err == nil {
				t.Fatal("forged repair target accepted")
			}
			if got := targetIDOf(t, a, "source.md"); got != nil {
				t.Fatalf("rejected repair mutated link: %d", *got)
			}
		})
	}
	if err := a.ResolveLinksTo(ctx, foreign.ID, own.Path, own.ShortID); err == nil {
		t.Fatal("wrapper accepted foreign ID")
	}
	if err := a.ResolveLinksTo(ctx, own.ID, own.Path, own.ShortID); err != nil {
		t.Fatal(err)
	}
}

func TestTenantGraphCorruptChildEndpoints(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	source := graphNote(t, a, "source.md", "source01", "Source", "p")
	peer := graphNote(t, a, "peer.md", "peer0001", "Peer", "p")
	target := graphNote(t, a, "target.md", "target01", "Target", "p")
	foreign := graphNote(t, b, "foreign.md", "foreign1", "Foreign", "p")
	// Positive controls ensure the scoped graph is not merely empty.
	graphLinks(t, a, source.Path, LinkInput{TargetPath: target.Path})
	graphLinks(t, a, peer.Path, LinkInput{TargetPath: target.ShortID})
	if err := a.SetTags(ctx, source.Path, []string{"own"}); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, owner.db, "PRAGMA foreign_keys=OFF")
	var enabled int
	if err := owner.db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 0 {
		t.Fatalf("FK injection not enabled: %d %v", enabled, err)
	}
	// Wrong child tenant, wrong source tenant, and wrong target tenant must
	// each be excluded independently, even when the raw path names an own note.
	for _, row := range []struct {
		tenant string
		source int64
		target interface{}
		path   string
	}{
		{"acme", source.ID, target.ID, target.Path},
		{"local", foreign.ID, target.ID, target.Path},
		{"local", source.ID, foreign.ID, peer.Path},
		{"acme", source.ID, nil, "later.md"},
		{"local", foreign.ID, nil, "later.md"},
	} {
		if _, err := owner.db.Exec("INSERT INTO links(tenant_id,source_id,target_id,target_path,href,type) VALUES(?,?,?,?,?,'wiki')", row.tenant, row.source, row.target, row.path, row.path); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []struct {
		tenant string
		id     int64
	}{{"acme", source.ID}, {"local", foreign.ID}} {
		if _, err := owner.db.Exec("INSERT INTO tags(tenant_id,note_id,tag) VALUES(?,?,'malicious')", row.tenant, row.id); err != nil {
			t.Fatal(err)
		}
	}
	relationalExec(t, owner.db, "PRAGMA foreign_keys=ON")
	links, err := a.GetLinks(ctx, source.Path)
	if err != nil || len(links) != 1 || links[0].TargetID == nil || *links[0].TargetID != target.ID {
		t.Fatalf("corrupt links visible: %+v, %v", links, err)
	}
	tags, err := a.GetTags(ctx, source.Path)
	if err != nil || !slices.Equal(tags, []string{"own"}) {
		t.Fatalf("corrupt tags visible: %v, %v", tags, err)
	}
	tags, err = b.GetTags(ctx, foreign.Path)
	if err != nil || len(tags) != 0 {
		t.Fatalf("wrong source tag visible: %v, %v", tags, err)
	}
	got, err := a.GetBacklinks(ctx, target.Path)
	graphWantNotes(t, got, err, source, peer)
	got, err = a.GetOutlinks(ctx, source.Path)
	graphWantNotes(t, got, err, target)
	got, err = a.GetRelated(ctx, source.Path, 20)
	graphWantNotes(t, got, err, peer)
	got, err = a.GetOrphans(ctx, &OrphanOptions{Path: peer.Path})
	graphWantNotes(t, got, err, peer) // Wrong target ID must not suppress via raw path either.
	graphNote(t, a, "later.md", "later001", "Later", "p")
	var repaired int
	if err := owner.db.QueryRow("SELECT count(*) FROM links WHERE target_path='later.md' AND target_id IS NOT NULL").Scan(&repaired); err != nil || repaired != 0 {
		t.Fatalf("corrupt repair: %d %v", repaired, err)
	}
	// Replacement must not delete wrong-tenant rows sharing the owned source ID.
	graphLinks(t, a, source.Path)
	if err := a.SetTags(ctx, source.Path, nil); err != nil {
		t.Fatal(err)
	}
	var linksLeft, tagsLeft int
	if err := owner.db.QueryRow("SELECT count(*) FROM links WHERE tenant_id='acme' AND source_id=?", source.ID).Scan(&linksLeft); err != nil {
		t.Fatal(err)
	}
	if err := owner.db.QueryRow("SELECT count(*) FROM tags WHERE tenant_id='acme' AND note_id=?", source.ID).Scan(&tagsLeft); err != nil {
		t.Fatal(err)
	}
	if linksLeft != 2 || tagsLeft != 1 {
		t.Fatalf("foreign child deletion: links=%d tags=%d", linksLeft, tagsLeft)
	}
}
