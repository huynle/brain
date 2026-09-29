package storage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantSearchIsolation(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	want := map[*TenantStore]*NoteRow{}
	for _, s := range []*TenantStore{b, a} {
		n := sampleNote("projects/shared/note/same.md", "same0001", "searchneedle")
		body := s.TenantID().String() + " searchneedle"
		n.Body = &body
		row, err := s.InsertNote(ctx, n)
		if err != nil {
			t.Fatal(err)
		}
		want[s] = row
	}
	for _, s := range []*TenantStore{a, b} {
		for _, strategy := range []string{"exact", "like", "fts", "unknown"} {
			t.Run(s.TenantID().String()+"/"+strategy, func(t *testing.T) {
				rows, err := s.SearchNotes(ctx, "searchneedle", &SearchOptions{Strategy: strategy, Limit: 1})
				if err != nil || len(rows) != 1 {
					t.Fatalf("search = %v, %v", rows, err)
				}
				expected := *want[s]
				expected.MatchSource = "entry"
				if !reflect.DeepEqual(rows[0], &expected) {
					t.Fatalf("full tenant row = %+v; want %+v", rows[0], expected)
				}
			})
		}
	}
}

func TestTenantSearchUnavailableFallbacks(t *testing.T) {
	for _, damage := range []string{
		"DROP TRIGGER p4_fts_map_delete; DELETE FROM tenant_fts WHERE tenant_id='local'",
		"DROP TRIGGER p4_fts_map_update; PRAGMA ignore_check_constraints=ON; UPDATE tenant_fts SET internal_id='invalid' WHERE tenant_id='local'",
		"DROP TABLE tenant_fts_insert_guard",
		"DROP TABLE %s",
		"DROP TABLE %s; CREATE TABLE %s(title,body,path)",
		"CREATE TRIGGER nested_note AFTER INSERT ON notes BEGIN INSERT INTO notes(tenant_id,path,short_id) VALUES(NEW.tenant_id,'nested','nested'); END",
	} {
		t.Run(damage, func(t *testing.T) {
			owner, s, _ := migratedNoteStores(t)
			addTenantSearchAttachment(t, owner, s, "projects/shared/available.md", "needle absent", "ready")
			var id string
			if err := owner.db.QueryRow("SELECT internal_id FROM tenant_fts WHERE tenant_id='local'").Scan(&id); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, owner.db, strings.ReplaceAll(damage, "%s", "fts_t_"+id))
			for _, query := range []string{"needle", "needle absent", `"unclosed needle`, "***", ""} {
				for _, strategy := range []string{"fts", "unknown", "exact", "like"} {
					rows, err := s.SearchNotes(context.Background(), query, &SearchOptions{Strategy: strategy})
					if !errors.Is(err, ErrTenantSearchUnavailable) || len(rows) != 0 {
						t.Errorf("%s %q: rows=%v err=%v; want unavailable", strategy, query, rows, err)
					}
				}
			}
			for name, op := range map[string]func() ([]*NoteRow, error){
				"match": func() ([]*NoteRow, error) { return s.ftsMatch(context.Background(), "needle", 1, nil) },
				"words": func() ([]*NoteRow, error) {
					return s.ftsMatchWordsResult(context.Background(), "needle absent", 1, nil)
				},
				"fts":   func() ([]*NoteRow, error) { return s.searchFTS(context.Background(), `"needle`, 1, nil) },
				"exact": func() ([]*NoteRow, error) { return s.searchExact(context.Background(), "needle", 1, nil) },
				"like":  func() ([]*NoteRow, error) { return s.searchLike(context.Background(), "needle", 1, nil) },
				"attachment": func() ([]*NoteRow, error) {
					return s.searchAttachmentDerivedText(context.Background(), "needle", 1, nil)
				},
			} {
				rows, err := op()
				if !errors.Is(err, ErrTenantSearchUnavailable) || len(rows) != 0 {
					t.Errorf("%s dropped sentinel: %v %v", name, rows, err)
				}
			}
		})
	}
}

func TestTenantSearchSchemaBeforeEmpty(t *testing.T) {
	owner := newTestStorage(t)
	foreign, _ := owner.ForTenant(tenant.MustParse("acme"))
	if _, err := foreign.SearchNotes(context.Background(), "", nil); err == nil {
		t.Fatal("v28 nonlocal empty search accepted")
	}
	local, _ := owner.ForTenant(tenant.Local)
	assertSearchDenied := func(s *TenantStore, ctx context.Context) {
		t.Helper()
		for _, op := range []func() error{
			func() error { _, err := s.SearchNotes(ctx, "", nil); return err },
			func() error { _, err := s.searchFTS(ctx, "***", 1, nil); return err },
			func() error { _, err := s.ftsMatchWordsResult(ctx, "", 1, nil); return err },
			func() error { _, err := s.ftsMatch(ctx, "", 1, nil); return err },
			func() error { _, err := s.searchLike(ctx, "", 1, nil); return err },
			func() error { _, err := s.searchExact(ctx, "", 1, nil); return err },
			func() error { _, err := s.searchAttachmentDerivedText(ctx, "", 1, nil); return err },
		} {
			if err := op(); err == nil {
				t.Fatal("invalid search execution accepted")
			}
		}
	}
	assertSearchDenied(foreign, context.Background())
	assertSearchDenied(local, nil)
	for _, s := range []*TenantStore{nil, {}, {tenantID: tenant.Local}} {
		assertSearchDenied(s, context.Background())
	}
	for _, version := range []int{0, 27, 29, 32} {
		relationalExec(t, owner.db, "DELETE FROM schema_version")
		if _, err := owner.db.Exec("INSERT INTO schema_version(version) VALUES(?)", version); err != nil {
			t.Fatal(err)
		}
		assertSearchDenied(local, context.Background())
	}
	relationalExec(t, owner.db, "DROP TABLE schema_version")
	assertSearchDenied(local, context.Background())
	_ = owner.db.Close()
	assertSearchDenied(local, context.Background())
}

func TestTenantSearchFiltersAndPagination(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range []*TenantStore{b, a} {
		for i := 0; i < 4; i++ {
			n := sampleNote(fmt.Sprintf("projects/shared/note/filter%d.md", i), fmt.Sprintf("filter0%d", i), "filterneedle")
			typ, status, priority, project, feature := "task", "pending", "high", "shared", "feature"
			n.Type, n.Status, n.Priority, n.ProjectID, n.FeatureID = &typ, &status, &priority, &project, &feature
			if i == 0 {
				status = "completed"
			}
			row, err := s.InsertNote(ctx, n)
			if err != nil {
				t.Fatal(err)
			}
			for _, tag := range []string{"common", s.TenantID().String()} {
				if _, err := owner.db.Exec("INSERT INTO tags(tenant_id,note_id,tag) VALUES(?,?,?)", s.TenantID().String(), row.ID, tag); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, s := range []*TenantStore{a, b} {
		for _, strategy := range []string{"fts", "like", "exact"} {
			opts := &SearchOptions{Strategy: strategy, Limit: 2, PathPrefix: "projects/shared/", Type: "task", Status: "pending", Priority: "high", ProjectID: "shared", FeatureID: "feature", Tags: []string{"common", s.TenantID().String()}}
			rows, err := s.SearchNotes(ctx, "filterneedle", opts)
			if err != nil || len(rows) != 2 {
				t.Fatalf("filtered %s: %v %v", strategy, rows, err)
			}
			for _, row := range rows {
				local, err := s.GetNoteByPath(ctx, row.Path)
				if err != nil || local.ID != row.ID || *row.Status != "pending" {
					t.Fatalf("foreign/unfiltered row: %+v %v", row, err)
				}
			}
			// ProjectID wins; otherwise ProjectIDs + global path must stay tenant-bound.
			opts.ProjectID, opts.ProjectIDs = "", []string{"shared"}
			if rows, err := s.SearchNotes(ctx, "filterneedle", opts); err != nil || len(rows) != 2 {
				t.Fatalf("project set: %v %v", rows, err)
			}
			opts.Tags = []string{"absent"}
			if rows, err := s.SearchNotes(ctx, "filterneedle", opts); err != nil || len(rows) != 0 {
				t.Fatalf("missing tag: %v %v", rows, err)
			}
		}
		seen := map[int64]bool{}
		for offset := 0; offset < 3; offset += 2 {
			rows, err := s.ListNotes(ctx, &ListOptions{Status: "pending", Tags: []string{"common", s.TenantID().String()}, SortBy: "title", SortOrder: "asc", Limit: 2, Offset: offset})
			if err != nil || len(rows) != min(2, 3-offset) {
				t.Fatalf("page %d: %v %v", offset, rows, err)
			}
			for _, row := range rows {
				if seen[row.ID] {
					t.Fatal("duplicate page row")
				}
				seen[row.ID] = true
			}
		}
		// The count and hydration share the caller's reserved single connection.
		tx, err := owner.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		name, err := tenantSearchTable(ctx, tx, s.TenantID())
		if err != nil {
			t.Fatal(err)
		}
		ss := tenantSearchSnapshot{tx: tx, owner: s.TenantID().String(), table: name}
		rows, count, err := ss.match(ctx, "filterneedle", 1, &SearchOptions{Status: "pending", Tags: []string{"common", s.TenantID().String()}})
		_ = tx.Rollback()
		if err != nil || count != 3 || len(rows) != 1 {
			t.Fatalf("filtered count=%d rows=%v err=%v", count, rows, err)
		}
	}
}

func addTenantSearchAttachment(t *testing.T, owner *StorageLayer, s *TenantStore, path, text, status string) (int64, int64) {
	t.Helper()
	row, err := s.InsertNote(context.Background(), sampleNote(path, "attach01", "ordinary"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := owner.db.Exec("INSERT INTO attachments(tenant_id,digest,size,media_type,metadata) VALUES(?,?,1,'text/plain','{}')", s.TenantID().String(), path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec("INSERT INTO entry_attachments(tenant_id,note_id,attachment_id,role) VALUES(?,?,?,'source')", s.TenantID().String(), row.ID, id); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec("INSERT INTO attachment_derived(tenant_id,attachment_id,kind,status,text) VALUES(?,?,'text',?,?)", s.TenantID().String(), id, status, text); err != nil {
		t.Fatal(err)
	}
	return row.ID, id
}

func TestTenantSearchAttachmentOwnership(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	want := map[*TenantStore]int64{}
	for _, s := range []*TenantStore{b, a} {
		id, _ := addTenantSearchAttachment(t, owner, s, "projects/shared/attachment.md", "ocrneedle "+s.TenantID().String(), "ready")
		want[s] = id
		addTenantSearchAttachment(t, owner, s, "projects/shared/pending.md", "ocrneedle", "pending")
	}
	for _, s := range []*TenantStore{a, b} {
		for _, strategy := range []string{"fts", "like", "exact"} {
			rows, err := s.SearchNotes(ctx, "ocrneedle", &SearchOptions{Strategy: strategy, Limit: 1})
			if err != nil || len(rows) != 1 || rows[0].ID != want[s] || rows[0].MatchSource != "attachment" {
				t.Fatalf("attachment %s %s: %v %v", s.TenantID(), strategy, rows, err)
			}
			rows, err = s.SearchNotes(ctx, "ocrneedle", &SearchOptions{Strategy: strategy, PathPrefix: "projects/shared/pending"})
			if err != nil || len(rows) != 0 {
				t.Fatalf("pending attachment: %v %v", rows, err)
			}
		}
	}
	// Corrupt each ownership edge independently: global integer IDs are not a
	// substitute for a predicate on ea, ad, or its attachment parent.
	for _, column := range []string{"entry_attachments", "attachment_derived", "attachments"} {
		noteID, attID := addTenantSearchAttachment(t, owner, a, "projects/shared/"+column+".md", "badownership", "ready")
		relationalExec(t, owner.db, "PRAGMA foreign_keys=OFF")
		key := "attachment_id"
		if column == "attachments" {
			key = "id"
		}
		if _, err := owner.db.Exec("UPDATE "+column+" SET tenant_id='acme' WHERE "+key+"=?", attID); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.db.Exec("INSERT INTO tags(tenant_id,note_id,tag) VALUES('acme',?,'foreign-child')", noteID); err != nil {
			t.Fatal(err)
		}
		relationalExec(t, owner.db, "PRAGMA foreign_keys=ON")
	}
	for _, strategy := range []string{"fts", "exact", "like"} {
		for _, s := range []*TenantStore{a, b} {
			rows, err := s.SearchNotes(ctx, "badownership", &SearchOptions{Strategy: strategy})
			if err != nil || len(rows) != 0 {
				t.Fatalf("cross-owner attachment: %v %v", rows, err)
			}
		}
		rows, err := a.SearchNotes(ctx, "ordinary", &SearchOptions{Strategy: strategy, Tags: []string{"foreign-child"}})
		if err != nil || len(rows) != 0 {
			t.Fatalf("foreign nested tag: %v %v", rows, err)
		}
	}
}

func TestTenantSearchFallbackQuality(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, s := range []*TenantStore{b, a} {
		if _, err := s.InsertNote(ctx, sampleNote("projects/shared/quality.md", "quality1", "running cooldown")); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []*TenantStore{a, b} {
		// Pin the original no-error signature, without using it in production
		// fallback control flow where the unavailable sentinel must survive.
		var words func(context.Context, string, int, *SearchOptions) []*NoteRow = s.ftsMatchWords
		if rows := words(ctx, "run", 1, nil); len(rows) != 1 {
			t.Fatalf("legacy helper contract: %v", rows)
		}
		for _, query := range []string{"run", "cooldown: absent", `"unclosed cooldown`, "run OR absent", "run*"} {
			rows, err := s.SearchNotes(ctx, query, nil)
			if err != nil || len(rows) != 1 {
				t.Fatalf("fallback %q: %v %v", query, rows, err)
			}
			want, err := s.GetNoteByPath(ctx, rows[0].Path)
			if err != nil || want.ID != rows[0].ID {
				t.Fatal("fallback crossed owner")
			}
		}
	}
}

func TestTenantSearchUnavailableIsNeverSyntax(t *testing.T) {
	for _, text := range []string{"fts5: syntax error", "unterminated string", "no such column: title", "unknown special query:"} {
		err := fmt.Errorf("%w: %s", ErrTenantSearchUnavailable, text)
		if tenantFTSSyntaxError(err) {
			t.Errorf("unavailable error classified as fallback syntax: %v", err)
		}
	}
}

func TestTenantSearchLegacyTokenSurvivesMigration(t *testing.T) {
	db, _ := completeMigrationFixture(t)
	// Literal pre-move schema row: do not mint/rotate through a new token API.
	const secret = "fixture-pre-p4-local-token"
	if _, err := db.Exec("INSERT INTO api_tokens(name,token,scope) VALUES('pre-p4',?,'read:*')", secret); err != nil {
		t.Fatal(err)
	}
	if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	owner := &StorageLayer{db: db}
	control, err := owner.Control()
	if err != nil {
		t.Fatal(err)
	}
	token, err := control.ValidateToken(context.Background(), secret)
	if err != nil || token == nil || token.Name != "pre-p4" || token.Token != secret || token.Scope != "read:*" {
		t.Fatalf("legacy token validation: %+v %v", token, err)
	}
	if CurrentSchemaVersion != 30 {
		t.Fatal("runtime activated")
	}
}
