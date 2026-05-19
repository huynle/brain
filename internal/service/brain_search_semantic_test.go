package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// mockEmbeddingClient implements EmbeddingClient for testing.
type mockEmbeddingClient struct {
	embedFunc func(ctx context.Context, inputs []string) ([][]float32, error)
}

func (m *mockEmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, inputs)
	}
	// Default: return a simple mock embedding
	result := make([][]float32, len(inputs))
	for i := range inputs {
		result[i] = []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	}
	return result, nil
}

// newTestBrainServiceWithEmbedding creates a test service with a mock embedding client.
func newTestBrainServiceWithEmbedding(t *testing.T, client EmbeddingClient) (*BrainServiceImpl, *storage.StorageLayer, string) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}

	store, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	brainDir := t.TempDir()
	cfg := &config.Config{BrainDir: brainDir}
	idx := indexer.NewIndexer(brainDir, store)

	svc := NewBrainService(cfg, store, idx, nil, client)
	return svc, store, brainDir
}

// =============================================================================
// Semantic Search Tests
// =============================================================================

func TestSearch_Semantic_WithEmbeddings(t *testing.T) {
	mockClient := &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			// Return a predictable embedding for the query
			return [][]float32{{0.5, 0.5, 0.5, 0.5, 0.5}}, nil
		},
	}

	svc, store, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	// Create test entries
	entry1, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})
	entry2, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Database Schema",
		Content: "PostgreSQL tables.",
	})

	// Get note IDs by querying the storage directly
	note1, _ := store.GetNoteByPath(ctx, entry1.Path)
	note2, _ := store.GetNoteByPath(ctx, entry2.Path)

	planType := "plan"
	_ = store.UpsertNoteEmbeddings(ctx, []storage.EmbeddingRecord{
		{
			NoteID:     note1.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.6, 0.5, 0.4, 0.5, 0.6}, // Similar to query
			Type:       &planType,
		},
		{
			NoteID:     note2.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.1, 0.1, 0.1, 0.1, 0.1}, // Less similar to query
			Type:       &planType,
		},
	})

	// Perform semantic search
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "semantic",
	})

	if err != nil {
		t.Fatalf("semantic search failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected at least 1 result from semantic search")
	}

	// Verify both entries are in the results (order may vary based on similarity)
	foundAuth := false
	foundDB := false
	for _, result := range resp.Results {
		if result.Title == "Authentication Design" {
			foundAuth = true
		}
		if result.Title == "Database Schema" {
			foundDB = true
		}
	}

	if !foundAuth {
		t.Error("expected 'Authentication Design' to be in results")
	}
	if !foundDB {
		t.Error("expected 'Database Schema' to be in results")
	}
}

func TestSaveAndUpdate_ReembedChangedEntry(t *testing.T) {
	var calls int
	mockClient := &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			calls += len(inputs)
			result := make([][]float32, len(inputs))
			for i := range inputs {
				result[i] = []float32{float32(calls), 0, 0}
			}
			return result, nil
		},
	}
	svc, store, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	entry, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Embedding Refresh",
		Content: "First semantic body.",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	note, err := store.GetNoteByPath(ctx, entry.Path)
	if err != nil || note == nil {
		t.Fatalf("expected saved note, got %#v err=%v", note, err)
	}
	status, err := store.EmbeddingStatus(ctx, note)
	if err != nil {
		t.Fatalf("EmbeddingStatus failed: %v", err)
	}
	if status != "current" {
		t.Fatalf("expected embedding status current after save, got %q", status)
	}
	if calls == 0 {
		t.Fatal("expected save to generate embeddings")
	}
	saveCalls := calls

	newContent := "Second semantic body requiring a fresh embedding."
	if _, err := svc.Update(ctx, entry.ID, types.UpdateEntryRequest{Content: &newContent}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	status, err = store.EmbeddingStatus(ctx, note)
	if err != nil {
		t.Fatalf("EmbeddingStatus after update failed: %v", err)
	}
	if status != "current" {
		t.Fatalf("expected embedding status current after update, got %q", status)
	}
	if calls <= saveCalls {
		t.Fatalf("expected update to re-generate embeddings, calls before=%d after=%d", saveCalls, calls)
	}
}

func TestSave_EmbedsOnlySavedEntryWhenOtherNotesMissingEmbeddings(t *testing.T) {
	svc, _, _ := newTestBrainServiceWithEmbedding(t, nil)
	ctx := context.Background()

	preexistingContent := "preexisting semantic body must not be embedded by later save"
	if _, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "scratch",
		Title:   "Missing Embedding Before Enable",
		Content: preexistingContent,
	}); err != nil {
		t.Fatalf("Save preexisting entry failed: %v", err)
	}

	var embeddedInputs []string
	svc.embeddingClient = &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			embeddedInputs = append(embeddedInputs, inputs...)
			result := make([][]float32, len(inputs))
			for i := range inputs {
				result[i] = []float32{0.1, 0.2, 0.3, 0.4, 0.5}
			}
			return result, nil
		},
	}

	savedContent := "new saved entry semantic body should be embedded"
	if _, err := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "scratch",
		Title:   "Saved After Enable",
		Content: savedContent,
	}); err != nil {
		t.Fatalf("Save after enabling embeddings failed: %v", err)
	}

	if len(embeddedInputs) == 0 {
		t.Fatal("expected saved entry to be embedded")
	}
	foundSavedEntry := false
	for _, input := range embeddedInputs {
		if strings.Contains(input, savedContent) {
			foundSavedEntry = true
		}
		if strings.Contains(input, preexistingContent) {
			t.Fatalf("save-time embedding indexed preexisting missing note; inputs=%q", embeddedInputs)
		}
	}
	if !foundSavedEntry {
		t.Fatalf("expected save-time embedding to include saved entry content; inputs=%q", embeddedInputs)
	}
}

func TestSearch_Semantic_FallbackToFTS_NoClient(t *testing.T) {
	// Create service with nil embedding client
	svc, _, _ := newTestBrainServiceWithEmbedding(t, nil)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// Semantic search should fall back to FTS
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "semantic",
	})

	if err != nil {
		t.Fatalf("semantic search with fallback failed: %v", err)
	}

	// Should still return FTS results
	if resp.Total == 0 {
		t.Error("expected FTS fallback to return results")
	}
}

func TestSearch_Semantic_FallbackToFTS_ClientError(t *testing.T) {
	mockClient := &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			return nil, errors.New("embedding service unavailable")
		},
	}

	svc, _, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// Semantic search should fall back to FTS on client error
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "semantic",
	})

	if err != nil {
		t.Fatalf("semantic search with fallback failed: %v", err)
	}

	// Should still return FTS results
	if resp.Total == 0 {
		t.Error("expected FTS fallback to return results")
	}
}

func TestSearch_Semantic_FallbackToFTS_EmptyEmbedding(t *testing.T) {
	mockClient := &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			return [][]float32{}, nil // Empty result
		},
	}

	svc, _, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// Semantic search should fall back to FTS on empty embedding
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "semantic",
	})

	if err != nil {
		t.Fatalf("semantic search with fallback failed: %v", err)
	}

	// Should still return FTS results
	if resp.Total == 0 {
		t.Error("expected FTS fallback to return results")
	}
}

func TestSearch_Semantic_WithFilters(t *testing.T) {
	mockClient := &mockEmbeddingClient{}
	svc, store, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	// Create entries of different types and statuses
	entry1, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "task",
		Title:   "Fix Auth Bug",
		Content: "Authentication issue in login.",
		Status:  "pending",
	})
	entry2, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Auth Plan",
		Content: "Authentication system design.",
		Status:  "completed",
	})

	// Get note IDs
	note1, _ := store.GetNoteByPath(ctx, entry1.Path)
	note2, _ := store.GetNoteByPath(ctx, entry2.Path)

	taskType := "task"
	planType := "plan"
	status1 := "pending"
	status2 := "completed"
	_ = store.UpsertNoteEmbeddings(ctx, []storage.EmbeddingRecord{
		{
			NoteID:     note1.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.5, 0.5, 0.5, 0.5, 0.5},
			Type:       &taskType,
			Status:     &status1,
		},
		{
			NoteID:     note2.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.5, 0.5, 0.5, 0.5, 0.5},
			Type:       &planType,
			Status:     &status2,
		},
	})

	// Search with type filter
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "semantic",
		Type:     "task",
	})

	if err != nil {
		t.Fatalf("semantic search with filters failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected at least 1 result with type filter")
	}

	// All results should be of type "task"
	for _, result := range resp.Results {
		if result.Type != "task" {
			t.Errorf("expected all results to be type 'task', got %q", result.Type)
		}
	}
}

// =============================================================================
// Hybrid Search Tests
// =============================================================================

func TestSearch_Hybrid_CombinesResults(t *testing.T) {
	mockClient := &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			return [][]float32{{0.5, 0.5, 0.5, 0.5, 0.5}}, nil
		},
	}

	svc, store, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	// Create entries: one will match FTS, another will match embeddings better
	entry1, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication System",
		Content: "JWT tokens and OAuth flow.",
	})
	entry2, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Database Design",
		Content: "PostgreSQL tables.",
	})
	entry3, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Security Framework",
		Content: "Security policies.",
	})

	// Get note IDs
	note1, _ := store.GetNoteByPath(ctx, entry1.Path)
	note2, _ := store.GetNoteByPath(ctx, entry2.Path)
	note3, _ := store.GetNoteByPath(ctx, entry3.Path)

	planType := "plan"
	_ = store.UpsertNoteEmbeddings(ctx, []storage.EmbeddingRecord{
		{
			NoteID:     note1.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.6, 0.5, 0.4, 0.5, 0.6}, // Very similar
			Type:       &planType,
		},
		{
			NoteID:     note2.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.1, 0.1, 0.1, 0.1, 0.1}, // Less similar
			Type:       &planType,
		},
		{
			NoteID:     note3.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.55, 0.5, 0.45, 0.5, 0.55}, // Similar
			Type:       &planType,
		},
	})

	// Perform hybrid search
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "hybrid",
	})

	if err != nil {
		t.Fatalf("hybrid search failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected results from hybrid search")
	}

	// Should combine both FTS and semantic results
	// At minimum, should include the entry with "authentication" in content
	foundAuth := false
	for _, result := range resp.Results {
		if result.Title == "Authentication System" {
			foundAuth = true
		}
	}

	if !foundAuth {
		t.Error("expected hybrid search to include FTS-matched result")
	}
}

func TestSearch_Hybrid_Deduplication(t *testing.T) {
	mockClient := &mockEmbeddingClient{}
	svc, store, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	// Create an entry that will match both FTS and embeddings
	entry, _ := svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication System",
		Content: "JWT authentication tokens.",
	})

	// Get note ID
	note, _ := store.GetNoteByPath(ctx, entry.Path)

	planType := "plan"
	_ = store.UpsertNoteEmbeddings(ctx, []storage.EmbeddingRecord{
		{
			NoteID:     note.ID,
			ChunkIndex: 0,
			Vector:     []float32{0.5, 0.5, 0.5, 0.5, 0.5},
			Type:       &planType,
		},
	})

	// Perform hybrid search
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "hybrid",
	})

	if err != nil {
		t.Fatalf("hybrid search failed: %v", err)
	}

	// Should only return the entry once, not duplicated
	if resp.Total != 1 {
		t.Errorf("expected 1 result (deduplicated), got %d", resp.Total)
	}
}

func TestSearch_Hybrid_FallbackToFTS_NoClient(t *testing.T) {
	// Create service with nil embedding client
	svc, _, _ := newTestBrainServiceWithEmbedding(t, nil)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// Hybrid search should fall back to FTS-only
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "hybrid",
	})

	if err != nil {
		t.Fatalf("hybrid search with fallback failed: %v", err)
	}

	// Should still return FTS results
	if resp.Total == 0 {
		t.Error("expected FTS fallback to return results")
	}
}

func TestSearch_Hybrid_FallbackToFTS_ClientError(t *testing.T) {
	mockClient := &mockEmbeddingClient{
		embedFunc: func(ctx context.Context, inputs []string) ([][]float32, error) {
			return nil, errors.New("embedding service unavailable")
		},
	}

	svc, _, _ := newTestBrainServiceWithEmbedding(t, mockClient)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// Hybrid search should fall back to FTS-only on client error
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "hybrid",
	})

	if err != nil {
		t.Fatalf("hybrid search with fallback failed: %v", err)
	}

	// Should still return FTS results
	if resp.Total == 0 {
		t.Error("expected FTS fallback to return results")
	}
}

// =============================================================================
// FTS Strategy Tests (Ensuring Backward Compatibility)
// =============================================================================

func TestSearch_FTS_Strategy(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// Explicit FTS strategy
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "fts",
	})

	if err != nil {
		t.Fatalf("FTS search failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected at least 1 result from FTS search")
	}
}

func TestSearch_DefaultStrategy(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// No strategy specified should default to FTS
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query: "authentication",
	})

	if err != nil {
		t.Fatalf("default search failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected at least 1 result from default search")
	}
}

func TestSearch_UnknownStrategy(t *testing.T) {
	svc, _, _ := newTestBrainService(t)
	ctx := context.Background()

	_, _ = svc.Save(ctx, types.CreateEntryRequest{
		Type:    "plan",
		Title:   "Authentication Design",
		Content: "JWT tokens and OAuth flow.",
	})

	// Unknown strategy should fall back to FTS
	resp, err := svc.Search(ctx, types.SearchRequest{
		Query:    "authentication",
		Strategy: "unknown-strategy",
	})

	if err != nil {
		t.Fatalf("search with unknown strategy failed: %v", err)
	}

	if resp.Total == 0 {
		t.Error("expected at least 1 result from fallback search")
	}
}
