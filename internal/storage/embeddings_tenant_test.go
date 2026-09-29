package storage

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

type embeddingIndexStorage interface {
	ListEmbeddingNotes(context.Context, string, string, bool) ([]*NoteRow, error)
	EmbeddingSource(context.Context, int64) (string, error)
	EmbeddingHealthCounts(context.Context) (int, int, int, error)
}

func TestTenantEmbeddingIndexStorage(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	api, ok := interface{}(a).(embeddingIndexStorage)
	if !ok {
		t.Fatal("missing guarded embedding index storage API")
	}
	var foreignID int64
	for i, s := range []*TenantStore{a, b} {
		n, err := s.InsertNote(ctx, sampleNote("global/index-source.md", "source01", "same corpus"))
		if err != nil {
			t.Fatal(err)
		}
		if i == 1 {
			foreignID = n.ID
		}
		att, err := s.CreateAttachment(ctx, AttachmentInput{Digest: strings.Repeat("e", 64), Size: 1, MediaType: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.LinkAttachmentToEntry(ctx, n.Path, att.ID, "inline"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{AttachmentID: att.ID, Kind: "text", Status: "ready", Text: []string{"A-only text", "B-only text"}[i]}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{{NoteID: n.ID, Vector: []float32{1, 0}}}); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.db.Exec("UPDATE notes SET indexed_at='2000' WHERE id=?", n.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.db.Exec("UPDATE attachment_derived SET updated_at=? WHERE tenant_id=?", []string{"2000", "9999"}[i], s.TenantID().String()); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []*TenantStore{a, b} {
		api := interface{}(s).(embeddingIndexStorage)
		rows, err := api.ListEmbeddingNotes(ctx, "", "global/index-source", true)
		if err != nil || len(rows) != 1 {
			t.Fatalf("owned candidates: %+v %v", rows, err)
		}
		text, err := api.EmbeddingSource(ctx, rows[0].ID)
		want, foreign := "A-only text", "B-only text"
		if s == b {
			want, foreign = foreign, want
		}
		if err != nil || !strings.Contains(text, want) || strings.Contains(text, foreign) {
			t.Fatalf("source: %q %v", text, err)
		}
		stale, err := api.ListEmbeddingNotes(ctx, "", "global/index-source", false)
		wantCount := 0
		if s == b {
			wantCount = 1
		}
		if err != nil || len(stale) != wantCount {
			t.Fatalf("staleness: %+v %v", stale, err)
		}
		status, err := s.EmbeddingStatus(ctx, rows[0])
		wantStatus := "current"
		if s == b {
			wantStatus = "stale"
		}
		if err != nil || status != wantStatus {
			t.Fatalf("status: %s %v", status, err)
		}
		total, with, staleCount, err := api.EmbeddingHealthCounts(ctx)
		var wantTotal, wantWith int
		if err := owner.db.QueryRow("SELECT count(*) FROM notes WHERE tenant_id=?", s.TenantID().String()).Scan(&wantTotal); err != nil {
			t.Fatal(err)
		}
		if err := owner.db.QueryRow("SELECT count(DISTINCT note_id) FROM note_embeddings_meta WHERE tenant_id=?", s.TenantID().String()).Scan(&wantWith); err != nil {
			t.Fatal(err)
		}
		if err != nil || total != wantTotal || with != wantWith {
			t.Fatalf("health: %d %d %v want %d %d", total, with, err, wantTotal, wantWith)
		}
		if s == b && staleCount != 1 {
			t.Fatalf("B stale count = %d, want 1", staleCount)
		}
	}
	if _, err := api.EmbeddingSource(ctx, foreignID); err == nil {
		t.Error("foreign source accepted")
	}
}

func TestTenantEmbeddingCandidatesBeforeLimit(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	if _, err := owner.db.Exec("DELETE FROM note_embeddings_meta; DELETE FROM note_embeddings"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ids := map[*TenantStore]int64{}
	for i, s := range []*TenantStore{a, b} {
		n, err := s.InsertNote(ctx, sampleNote("global/similar.md", "similar1", "identical semantic corpus"))
		if err != nil {
			t.Fatal(err)
		}
		ids[s] = n.ID
		// B outranks A, so filtering only after limit=1 loses A entirely.
		v := []float32{1, 0.1}
		if i == 1 {
			v = []float32{1, 0}
		}
		if _, err := owner.db.Exec("INSERT INTO note_embeddings(tenant_id,note_id,chunk_index,embedding) VALUES(?,?,0,?)", s.TenantID().String(), n.ID, packFloat32s(v)); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.db.Exec("INSERT INTO note_embeddings_meta(tenant_id,note_id,chunk_index,embedding_indexed_at) VALUES(?,?,0,'9999')", s.TenantID().String(), n.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.SetTags(ctx, n.Path, []string{"shared"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, opts := range []*EmbeddingSearchOptions{{Limit: 1}, {Limit: 1, Tags: []string{"shared"}}, {Limit: 1, ProjectIDs: []string{"absent"}, IncludeGlobalPath: true}} {
		for _, s := range []*TenantStore{a, b} {
			query := []float32{1, 0}
			if s == b {
				query = []float32{1, 0.1}
			} // A now outranks B.
			got, err := s.SearchByEmbedding(ctx, query, opts)
			if err != nil || len(got) != 1 || got[0].ID != ids[s] {
				t.Errorf("%s search = %+v, %v; want owned ID %d", s.TenantID(), got, err, ids[s])
			}
		}
	}
	if v, err := a.GetNoteEmbedding(ctx, ids[b], 0); err != nil || v != nil {
		t.Errorf("foreign vector = %v, %v", v, err)
	}
	if v, err := b.GetNoteEmbedding(ctx, ids[a], 0); err != nil || v != nil {
		t.Errorf("reverse foreign vector = %v, %v", v, err)
	}
}

func TestTenantEmbeddingMutationOwnership(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	an, err := a.InsertNote(ctx, sampleNote("global/a.md", "embedaaa", "A"))
	if err != nil {
		t.Fatal(err)
	}
	bn, err := b.InsertNote(ctx, sampleNote("global/b.md", "embedbbb", "B"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		s *TenantStore
		n *NoteRow
	}{{a, an}, {b, bn}} {
		if err := pair.s.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{{NoteID: pair.n.ID, Vector: []float32{1, 0}}}); err != nil {
			t.Fatalf("owned upsert: %v", err)
		}
	}
	changedProject := "must-roll-back"
	if err := a.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{{NoteID: an.ID, Vector: []float32{0, 1}, ProjectID: &changedProject}, {NoteID: bn.ID, Vector: []float32{0, 1}}}); err == nil {
		t.Error("accepted mixed-owner batch")
	}
	if v, err := a.GetNoteEmbedding(ctx, an.ID, 0); err != nil || len(v) != 2 || v[0] != 1 {
		t.Errorf("batch did not roll back: %v %v", v, err)
	}
	var project *string
	if err := owner.db.QueryRow("SELECT project_id FROM note_embeddings_meta WHERE tenant_id='local' AND note_id=?", an.ID).Scan(&project); err != nil || project != nil {
		t.Fatalf("metadata rollback: %v %v", project, err)
	}
	if err := a.SyncNoteEmbeddingMetadata(ctx, bn); err == nil {
		t.Error("accepted foreign metadata")
	}
	if _, err := a.EmbeddingStatus(ctx, bn); err == nil {
		t.Error("accepted foreign status")
	}
	_ = a.DeleteNoteEmbeddings(ctx, bn.ID)
	if v, err := b.GetNoteEmbedding(ctx, bn.ID, 0); err != nil || len(v) != 2 || v[0] != 1 {
		t.Errorf("foreign delete changed B: %v %v", v, err)
	}
	if err := a.DeleteNoteEmbeddings(ctx, an.ID); err != nil {
		t.Fatal(err)
	}
	if v, err := a.GetNoteEmbedding(ctx, an.ID, 0); err != nil || v != nil {
		t.Errorf("delete = %v %v", v, err)
	}
}

func TestTenantEmbeddingGuardsBeforeFastPaths(t *testing.T) {
	raw := newTestStorage(t)
	foreign, err := raw.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []*TenantStore{foreign, {}, nil} {
		if err := s.UpsertNoteEmbeddings(context.Background(), nil); err == nil {
			t.Error("unguarded empty upsert")
		}
		if _, err := s.SearchByEmbedding(context.Background(), nil, nil); err == nil {
			t.Error("unguarded empty search")
		}
		if _, err := s.EmbeddingStatus(context.Background(), nil); err == nil {
			t.Error("unguarded nil status")
		}
		if err := s.SyncNoteEmbeddingMetadata(context.Background(), nil); err == nil {
			t.Error("unguarded nil sync")
		}
		assertEmbeddingReadGuards(t, s, context.Background())
	}
	local := newTestContentStorage(t)
	assertEmbeddingReadGuards(t, local, nil)
	if err := local.UpsertNoteEmbeddings(nil, nil); err == nil { //nolint:staticcheck // Deliberately verify invalid-context rejection.
		t.Error("nil context upsert")
	}
	if _, err := local.SearchByEmbedding(nil, nil, nil); err == nil { //nolint:staticcheck // Deliberately verify invalid-context rejection.
		t.Error("nil context search")
	}
	if _, err := local.EmbeddingStatus(nil, nil); err == nil { //nolint:staticcheck // Deliberately verify invalid-context rejection.
		t.Error("nil context status")
	}
	if err := local.SyncNoteEmbeddingMetadata(nil, nil); err == nil { //nolint:staticcheck // Deliberately verify invalid-context rejection.
		t.Error("nil context sync")
	}
}

func assertEmbeddingReadGuards(t *testing.T, s *TenantStore, ctx context.Context) {
	t.Helper()
	if _, err := s.GetNoteEmbedding(ctx, 1, 0); err == nil {
		t.Error("unguarded vector read")
	}
	if err := s.DeleteNoteEmbeddings(ctx, 1); err == nil {
		t.Error("unguarded delete")
	}
	if _, err := s.ListEmbeddingNotes(ctx, "", "", true); err == nil {
		t.Error("unguarded candidates")
	}
	if _, err := s.EmbeddingSource(ctx, 1); err == nil {
		t.Error("unguarded source")
	}
	if _, _, _, err := s.EmbeddingHealthCounts(ctx); err == nil {
		t.Error("unguarded health")
	}
}

func TestTenantEmbeddingMigratedSchemaNeverFallsBack(t *testing.T) {
	owner, a, _ := migratedNoteStores(t)
	// Break one v29 ownership column. A legacy SQL fallback would succeed.
	if _, err := owner.db.Exec("ALTER TABLE note_embeddings RENAME COLUMN tenant_id TO broken_owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetNoteEmbedding(context.Background(), 10, 0); err == nil {
		t.Error("v29 vector read fell back to legacy SQL")
	}
}
