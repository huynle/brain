package indexer

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
)

func TestEmbeddingIndexerRejectsNonlocalLegacy(t *testing.T) {
	store := newTestStorage(t)
	foreign, err := store.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer(t.TempDir(), foreign)
	ctx := context.Background()
	if _, err := idx.ListEmbeddingBackfillCandidates(ctx, EmbeddingIndexOptions{Force: true}); err == nil {
		t.Error("unguarded candidates")
	}
	if _, err := idx.IndexEmbeddingsWithOptions(ctx, &recordingEmbeddingClient{}, EmbeddingIndexOptions{Force: true}); err == nil {
		t.Error("unguarded reindex")
	}
	if _, err := idx.embeddingSourceForNote(ctx, 999); err == nil {
		t.Error("unguarded source")
	}
	if _, err := idx.GetEmbeddingHealth(); err == nil {
		t.Error("unguarded health")
	}
}

// Query-seam fixture only: the full dormant migration and composite FK contract
// are tested in storage with migratedNoteStores. No production constructor is
// taught to activate v29. Keep these records deliberately similar across owners.
func TestEmbeddingIndexerTenantIsolation(t *testing.T) {
	store, db := newTestStorageWithDB(t)
	ctx := context.Background()
	ids := []int64{}
	for i, path := range []string{"global/a.md", "global/b.md"} {
		n, err := store.InsertNote(ctx, &storage.NoteRow{Path: path, ShortID: path, Title: "same corpus", Body: strPtr("same body"), Metadata: "{}"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, n.ID)
		att, err := store.CreateAttachment(ctx, storage.AttachmentInput{Digest: strings.Repeat(string(rune('a'+i)), 64), Size: 1, MediaType: "text/plain"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.LinkAttachmentToEntry(ctx, path, att.ID, "inline"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpsertAttachmentDerived(ctx, storage.AttachmentDerivedInput{AttachmentID: att.ID, Kind: "text", Status: "ready", Text: []string{"A attachment", "B secret attachment"}[i]}); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{
		"ALTER TABLE notes ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'local'",
		"ALTER TABLE note_embeddings ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'local'",
		"ALTER TABLE note_embeddings_meta ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'local'",
		"ALTER TABLE entry_attachments ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'local'",
		"ALTER TABLE attachment_derived ADD COLUMN tenant_id TEXT NOT NULL DEFAULT 'local'",
		"CREATE UNIQUE INDEX test_embedding_key ON note_embeddings(tenant_id,note_id,chunk_index)",
		"CREATE UNIQUE INDEX test_meta_key ON note_embeddings_meta(tenant_id,note_id,chunk_index)",
		"UPDATE notes SET indexed_at='2000-01-01'",
		"UPDATE notes SET tenant_id='acme' WHERE path='global/b.md'",
		"UPDATE entry_attachments SET tenant_id='acme' WHERE note_id=(SELECT id FROM notes WHERE tenant_id='acme')",
		"UPDATE attachment_derived SET tenant_id='acme' WHERE attachment_id IN (SELECT attachment_id FROM entry_attachments WHERE tenant_id='acme')",
		"INSERT INTO schema_version(version) VALUES(29)",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	b, err := store.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.UpsertNoteEmbeddings(ctx, []storage.EmbeddingRecord{{NoteID: ids[1], Vector: []float32{0, 1}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE note_embeddings_meta SET embedding_indexed_at='2001-01-01' WHERE tenant_id='acme'"); err != nil {
		t.Fatal(err)
	}
	idx := NewIndexer(t.TempDir(), store)
	for _, force := range []bool{false, true} {
		got, err := idx.ListEmbeddingBackfillCandidates(ctx, EmbeddingIndexOptions{Force: force})
		if err != nil || len(got) != 1 || got[0].ID != ids[0] {
			t.Fatalf("candidates force=%v: %+v %v", force, got, err)
		}
	}
	if _, err := idx.embeddingSourceForNote(ctx, ids[1]); err == nil {
		t.Error("foreign source accepted")
	}
	client := &recordingEmbeddingClient{}
	for _, force := range []bool{false, true} {
		result, err := idx.IndexEmbeddingsWithOptions(ctx, client, EmbeddingIndexOptions{Force: force})
		if err != nil || result.Processed != 1 || result.Failed != 0 {
			t.Fatalf("index force=%v: %+v %v", force, result, err)
		}
	}
	if len(client.inputs) != 2 {
		t.Fatalf("source isolation: %v", client.inputs)
	}
	for _, input := range client.inputs {
		if !strings.Contains(input, "A attachment") || strings.Contains(input, "B secret") {
			t.Fatalf("source isolation: %q", input)
		}
	}
	var stamp string
	if err := db.QueryRow("SELECT embedding_indexed_at FROM note_embeddings_meta WHERE tenant_id='acme'").Scan(&stamp); err != nil || stamp != "2001-01-01" {
		t.Fatalf("B metadata changed: %s %v", stamp, err)
	}
	if v, err := b.GetNoteEmbedding(ctx, ids[1], 0); err != nil || len(v) != 2 || v[1] != 1 {
		t.Fatalf("B vector changed: %v %v", v, err)
	}
	h, err := idx.GetEmbeddingHealth()
	if err != nil || h.TotalNotes != 1 || h.NotesWithEmbeddings != 1 || h.StaleEmbeddings != 0 {
		t.Fatalf("health: %+v %v", h, err)
	}
}
