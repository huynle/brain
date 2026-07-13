package service

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// These tests pin down the embedding behavior of entry updates:
//   - metadata-only updates (status, priority, …) must NOT call the embedding
//     API at all — the embedded text is unchanged. They only mirror the new
//     filter values onto the existing embedding rows.
//   - content updates must not block the write on the embedding API; the
//     re-embed happens in the background.
//
// Regression context: PATCH /entries/{id} used to re-embed synchronously, so a
// status-only change took as long as an embedding API round-trip. Clients with
// a 5s timeout (brain CLI runner.api_timeout=5000) disconnected, cancelling
// the request context mid-embed — the server logged "context canceled" and
// returned 500 even though the entry write had already persisted.

// gatedEmbeddingClient counts Embed calls and, once armed, blocks each call
// until released (or a safety timeout elapses, so a regression to synchronous
// embedding fails the test instead of hanging the suite).
type gatedEmbeddingClient struct {
	armed      atomic.Bool
	gatedCalls atomic.Int32
	release    chan struct{}
}

func newGatedEmbeddingClient() *gatedEmbeddingClient {
	return &gatedEmbeddingClient{release: make(chan struct{})}
}

func (c *gatedEmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if c.armed.Load() {
		c.gatedCalls.Add(1)
		select {
		case <-c.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	result := make([][]float32, len(inputs))
	for i := range inputs {
		result[i] = []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	}
	return result, nil
}

func embeddingMetaStatus(t *testing.T, svc *BrainServiceImpl, path string) string {
	t.Helper()
	ctx := context.Background()
	row, err := svc.storage.GetNoteByPath(ctx, path)
	if err != nil || row == nil {
		t.Fatalf("GetNoteByPath(%q) = %#v, err=%v", path, row, err)
	}
	var status string
	err = svc.storage.DB().QueryRowContext(ctx,
		"SELECT status FROM note_embeddings_meta WHERE note_id = ? AND chunk_index = 0",
		row.ID,
	).Scan(&status)
	if err != nil {
		t.Fatalf("query embedding meta status: %v", err)
	}
	return status
}

func TestUpdate_MetadataOnlyDoesNotCallEmbeddingAPI(t *testing.T) {
	client := newGatedEmbeddingClient()
	t.Cleanup(func() { close(client.release) })
	svc, store, _ := newTestBrainServiceWithEmbedding(t, client)
	ctx := context.Background()

	entry, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Metadata Only Update",
		Content: "Task body that was embedded at save time.",
		Status:  "pending",
		Project: "default",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Any Embed call from here on blocks and is counted as a violation.
	client.armed.Store(true)

	newStatus := "in_progress"
	if _, err := svc.Update(ctx, entry.ID, types.UpdateEntryRequest{Status: &newStatus}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	svc.WaitForPendingEmbeddings()

	if n := client.gatedCalls.Load(); n != 0 {
		t.Errorf("metadata-only update triggered %d Embed call(s), want 0", n)
	}

	// The new status is mirrored onto the embedding rows for filtered search…
	if got := embeddingMetaStatus(t, svc, entry.Path); got != newStatus {
		t.Errorf("embedding meta status = %q, want %q", got, newStatus)
	}
	// …and the embeddings still count as current.
	row, err := store.GetNoteByPath(ctx, entry.Path)
	if err != nil || row == nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	status, err := store.EmbeddingStatus(ctx, row)
	if err != nil {
		t.Fatalf("EmbeddingStatus failed: %v", err)
	}
	if status != "current" {
		t.Errorf("embedding status = %q, want current", status)
	}
}

func TestUpdateMetadata_DurableFieldsDoNotCallEmbeddingAPI(t *testing.T) {
	client := newGatedEmbeddingClient()
	t.Cleanup(func() { close(client.release) })
	svc, _, _ := newTestBrainServiceWithEmbedding(t, client)
	ctx := context.Background()

	entry, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Durable Metadata Update",
		Content: "Body embedded at save time.",
		Status:  "pending",
		Project: "default",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	client.armed.Store(true)

	// "status" is a durable field, so this routes through syncDurableFieldsToFile.
	if _, err := svc.UpdateMetadata(ctx, entry.ID, map[string]interface{}{"status": "completed"}); err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}
	svc.WaitForPendingEmbeddings()

	if n := client.gatedCalls.Load(); n != 0 {
		t.Errorf("durable metadata update triggered %d Embed call(s), want 0", n)
	}
	if got := embeddingMetaStatus(t, svc, entry.Path); got != "completed" {
		t.Errorf("embedding meta status = %q, want %q", got, "completed")
	}
}

func TestUpdate_ContentChangeReembedsInBackground(t *testing.T) {
	client := newGatedEmbeddingClient()
	svc, store, _ := newTestBrainServiceWithEmbedding(t, client)
	ctx := context.Background()

	entry, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Content Update",
		Content: "Original body.",
		Project: "default",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Embed calls now block until released: if Update re-embedded
	// synchronously it would stall here instead of returning.
	client.armed.Store(true)

	newContent := "Replacement body that needs a fresh embedding."
	start := time.Now()
	if _, err := svc.Update(ctx, entry.ID, types.UpdateEntryRequest{Content: &newContent}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Update blocked on embedding for %v; re-embed should be asynchronous", elapsed)
	}

	// Let the background refresh finish and verify it actually ran.
	close(client.release)
	svc.WaitForPendingEmbeddings()

	if n := client.gatedCalls.Load(); n == 0 {
		t.Error("content update never re-embedded in the background")
	}
	row, err := store.GetNoteByPath(ctx, entry.Path)
	if err != nil || row == nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	status, err := store.EmbeddingStatus(ctx, row)
	if err != nil {
		t.Fatalf("EmbeddingStatus failed: %v", err)
	}
	if status != "current" {
		t.Errorf("embedding status after background refresh = %q, want current", status)
	}
	if row.Body == nil || !strings.Contains(*row.Body, "Replacement body") {
		t.Errorf("note body not updated: %v", row.Body)
	}
}

// TestUpdate_MetadataSyncConvergesDuringBackgroundRefresh pins down the
// concurrency fix for the delete→upsert gap. A body-change refresh holds the
// per-path lock across a slow (gated) embed — during which it has already
// deleted the note's embedding rows. A second, metadata-only update lands in
// that exact window. Because the metadata sync is serialized under the SAME
// per-path lock and reads the note fresh, the embedding filter metadata must
// converge to the entry's latest committed status once everything drains.
//
// Against the earlier design (synchronous, unlocked metadata sync) this fails:
// the sync's UPDATE would match zero rows (already deleted) and be silently
// dropped, then the refresh would upsert its pre-embed status snapshot, leaving
// note_embeddings_meta.status permanently diverged from notes.status.
func TestUpdate_MetadataSyncConvergesDuringBackgroundRefresh(t *testing.T) {
	client := newGatedEmbeddingClient()
	svc, _, _ := newTestBrainServiceWithEmbedding(t, client)
	ctx := context.Background()

	entry, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Concurrent Refresh And Status",
		Content: "original body",
		Status:  "open",
		Project: "default",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Arm the gate so the NEXT embed (the body-change refresh) blocks after it
	// has deleted the existing embedding rows.
	client.armed.Store(true)

	newBody := "changed body requiring re-embed"
	if _, err := svc.Update(ctx, entry.ID, types.UpdateEntryRequest{Content: &newBody}); err != nil {
		t.Fatalf("body Update failed: %v", err)
	}

	// Wait until the refresh goroutine is actually blocked inside Embed(),
	// which is AFTER it committed DeleteNoteEmbeddings — i.e. the meta rows are
	// gone. This is the window the bug exploited.
	deadline := time.Now().Add(5 * time.Second)
	for client.gatedCalls.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("background refresh never entered the gated embed window")
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Metadata-only update lands squarely in the delete→upsert gap.
	done := "completed"
	if _, err := svc.Update(ctx, entry.ID, types.UpdateEntryRequest{Status: &done}); err != nil {
		t.Fatalf("status Update failed: %v", err)
	}

	// Release the embed and let both background jobs drain.
	close(client.release)
	svc.WaitForPendingEmbeddings()

	if got := embeddingMetaStatus(t, svc, entry.Path); got != done {
		t.Errorf("embedding meta status = %q, want %q (must converge to latest committed entry status)", got, done)
	}

	// Sanity: the persisted entry itself is 'completed'.
	row, err := svc.storage.GetNoteByPath(ctx, entry.Path)
	if err != nil || row == nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if row.Status == nil || *row.Status != done {
		t.Errorf("persisted status = %v, want %q", row.Status, done)
	}
}

// TestPatchEntry_MetadataOnly_Returns200Fast is the end-to-end regression
// test: a status-only PATCH /entries/{id} must return 200 well within the
// brain CLI's 5s client timeout even when the embedding backend is completely
// unresponsive, and must not fire any embedding call.
func TestPatchEntry_MetadataOnly_Returns200Fast(t *testing.T) {
	client := newGatedEmbeddingClient()
	t.Cleanup(func() { close(client.release) })
	svc, _, _ := newTestBrainServiceWithEmbedding(t, client)
	ctx := context.Background()

	entry, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "PATCH Timeout Regression",
		Content: "Body embedded at save time.",
		Status:  "pending",
		Project: "default",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	h := api.NewHandler(svc)
	r := chi.NewRouter()
	r.Patch("/entries/*", h.HandleUpdateOrMetadata)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// From here on, any embedding call blocks for 10s — longer than the
	// client timeout below, so a synchronous embed fails this test the same
	// way it broke the brain CLI in production.
	client.armed.Store(true)

	httpClient := &http.Client{Timeout: 5 * time.Second} // mirrors runner.api_timeout=5000
	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/entries/"+entry.ID,
		bytes.NewBufferString(`{"status":"in_progress"}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("PATCH failed after %v: %v", elapsed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH status = %d, want 200", resp.StatusCode)
	}
	if elapsed > 2*time.Second {
		t.Errorf("PATCH took %v; a metadata-only update must not wait on embeddings", elapsed)
	}

	svc.WaitForPendingEmbeddings()
	if n := client.gatedCalls.Load(); n != 0 {
		t.Errorf("metadata-only PATCH triggered %d Embed call(s), want 0", n)
	}

	// The mutation persisted and is mirrored to the embedding metadata.
	row, err := svc.storage.GetNoteByPath(ctx, entry.Path)
	if err != nil || row == nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if row.Status == nil || *row.Status != "in_progress" {
		t.Errorf("persisted status = %v, want in_progress", row.Status)
	}
	if got := embeddingMetaStatus(t, svc, entry.Path); got != "in_progress" {
		t.Errorf("embedding meta status = %q, want in_progress", got)
	}
}
