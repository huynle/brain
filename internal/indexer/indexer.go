package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenantfs"
	"github.com/huynle/brain-api/pkg/markdown"
)

// EmbeddingClient defines the interface for generating embeddings.
// This is defined here to avoid import cycles with internal/service.
type EmbeddingClient interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// Indexer synchronizes markdown files on disk with the SQLite database.
type Indexer struct {
	brainDir string
	storage  *storage.TenantStore
	policy   *tenantfs.Root
}

// NewIndexer creates a new Indexer for the given brain directory and storage layer.
func NewIndexer(brainDir string, store *storage.TenantStore, policy ...*tenantfs.Root) *Indexer {
	idx := &Indexer{
		brainDir: brainDir,
		storage:  store,
	}
	if len(policy) > 0 {
		idx.policy = policy[0]
	}
	return idx
}

// FilesystemPolicy is the immutable tenant binding shared with service writers
// and watchers. Only trusted composition supplies it; it never provisions roots.
func (idx *Indexer) FilesystemPolicy() *tenantfs.Root { return idx.policy }

func (idx *Indexer) admit(name string, missing bool) error {
	if idx.policy == nil {
		return nil
	}
	if missing {
		_, err := idx.policy.ResolveForWrite(context.Background(), name)
		return err
	}
	return idx.policy.AdmitTraversal(context.Background(), name)
}

func (idx *Indexer) markdownFiles() ([]string, error) {
	return globMarkdownFiles(idx.brainDir, idx.policy)
}

// RebuildAll performs a full rebuild: deletes this tenant's notes and re-indexes
// every discovered .md file under projects/ and global/.
func (idx *Indexer) RebuildAll() (*IndexResult, error) {
	start := time.Now()
	ctx := context.Background()
	var indexErrors []IndexError

	// 1. Discover files on disk
	files, err := idx.markdownFiles()
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}

	// 2. Parse all files, collecting results and errors
	var parsed []*markdown.ParsedFile
	for _, file := range files {
		if err := idx.admit(file, false); err != nil {
			return nil, err
		}
		pf, err := markdown.ParseFile(file, idx.brainDir)
		if err != nil {
			indexErrors = append(indexErrors, IndexError{
				Path:  file,
				Error: err.Error(),
			})
			continue
		}
		parsed = append(parsed, pf)
	}

	// 3. Clear this tenant's notes (CASCADE cleans dependent content).
	existingCount, err := idx.storage.DeleteAllNotes(ctx)
	if err != nil {
		return nil, fmt.Errorf("delete all notes: %w", err)
	}

	// 4. Insert each parsed file
	for _, pf := range parsed {
		row := toNoteRow(pf)
		inserted, err := idx.storage.InsertNote(ctx, &row)
		if err != nil {
			return nil, fmt.Errorf("insert note %q: %w", pf.Path, err)
		}
		_ = inserted

		if len(pf.Tags) > 0 {
			if err := idx.storage.SetTags(ctx, pf.Path, pf.Tags); err != nil {
				return nil, fmt.Errorf("set tags for %q: %w", pf.Path, err)
			}
		}

		if len(pf.Links) > 0 {
			if err := idx.storage.SetLinks(ctx, pf.Path, toLinkInputs(pf.Links)); err != nil {
				return nil, fmt.Errorf("set links for %q: %w", pf.Path, err)
			}
		}
	}

	return &IndexResult{
		Added:    len(parsed),
		Updated:  0,
		Deleted:  int(existingCount),
		Skipped:  0,
		Errors:   indexErrors,
		Duration: time.Since(start),
	}, nil
}

// IndexChanged performs an incremental index: compares files on disk with DB,
// only processing changes.
func (idx *Indexer) IndexChanged() (*IndexResult, error) {
	start := time.Now()
	ctx := context.Background()
	var indexErrors []IndexError
	var added, updated, deleted, skipped int

	// 1. Discover files on disk
	diskFiles, err := idx.markdownFiles()
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}
	diskSet := make(map[string]bool, len(diskFiles))
	for _, f := range diskFiles {
		diskSet[f] = true
	}

	// 2. Get all existing notes from DB (path + checksum)
	states, err := idx.storage.ListIndexedNoteStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("query existing notes: %w", err)
	}
	dbMap := make(map[string]*string) // path → checksum (nullable)
	for _, state := range states {
		dbMap[state.Path] = state.Checksum
	}

	// 3. Process each file on disk
	for _, file := range diskFiles {
		if err := idx.admit(file, false); err != nil {
			return nil, err
		}
		pf, err := markdown.ParseFile(file, idx.brainDir)
		if err != nil {
			indexErrors = append(indexErrors, IndexError{
				Path:  file,
				Error: err.Error(),
			})
			continue
		}

		existingChecksum, inDB := dbMap[file]
		if !inDB {
			// New file — not in DB
			row := toNoteRow(pf)
			if _, err := idx.storage.InsertNote(ctx, &row); err != nil {
				return nil, fmt.Errorf("insert note %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetTags(ctx, pf.Path, pf.Tags); err != nil {
				return nil, fmt.Errorf("set tags for %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetLinks(ctx, pf.Path, toLinkInputs(pf.Links)); err != nil {
				return nil, fmt.Errorf("set links for %q: %w", pf.Path, err)
			}
			added++
		} else if existingChecksum == nil || *existingChecksum != pf.Checksum {
			// Modified file — checksum differs
			updates := noteUpdateMap(pf)
			if _, err := idx.storage.UpdateNote(ctx, pf.Path, updates); err != nil {
				return nil, fmt.Errorf("update note %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetTags(ctx, pf.Path, pf.Tags); err != nil {
				return nil, fmt.Errorf("set tags for %q: %w", pf.Path, err)
			}
			if err := idx.storage.SetLinks(ctx, pf.Path, toLinkInputs(pf.Links)); err != nil {
				return nil, fmt.Errorf("set links for %q: %w", pf.Path, err)
			}
			updated++
		} else {
			// Unchanged — skip
			skipped++
		}
	}

	// 4. Delete DB entries absent from scoped discovery, including formerly
	// indexed out-of-scope files that still exist on disk. Disk files are untouched.
	for dbPath := range dbMap {
		if !diskSet[dbPath] {
			if _, err := idx.storage.DeleteNote(ctx, dbPath); err != nil {
				return nil, fmt.Errorf("delete note %q: %w", dbPath, err)
			}
			deleted++
		}
	}

	return &IndexResult{
		Added:    added,
		Updated:  updated,
		Deleted:  deleted,
		Skipped:  skipped,
		Errors:   indexErrors,
		Duration: time.Since(start),
	}, nil
}

// IndexFile indexes a single file by relative path (upsert).
func (idx *Indexer) IndexFile(relativePath string) error {
	ctx := context.Background()
	if idx.policy != nil {
		if _, err := idx.policy.Resolve(ctx, relativePath); err != nil {
			return err
		}
	}

	pf, err := markdown.ParseFile(relativePath, idx.brainDir)
	if err != nil {
		return fmt.Errorf("parse file %q: %w", relativePath, err)
	}

	existing, err := idx.storage.GetNoteByPath(ctx, relativePath)
	if err != nil {
		return fmt.Errorf("check existing note %q: %w", relativePath, err)
	}

	if existing != nil {
		// Update
		updates := noteUpdateMap(pf)
		if _, err := idx.storage.UpdateNote(ctx, relativePath, updates); err != nil {
			return fmt.Errorf("update note %q: %w", relativePath, err)
		}
	} else {
		// Insert
		row := toNoteRow(pf)
		if _, err := idx.storage.InsertNote(ctx, &row); err != nil {
			return fmt.Errorf("insert note %q: %w", relativePath, err)
		}
	}

	if err := idx.storage.SetTags(ctx, relativePath, pf.Tags); err != nil {
		return fmt.Errorf("set tags for %q: %w", relativePath, err)
	}
	if err := idx.storage.SetLinks(ctx, relativePath, toLinkInputs(pf.Links)); err != nil {
		return fmt.Errorf("set links for %q: %w", relativePath, err)
	}

	return nil
}

// RemoveFile removes a single file from the index.
func (idx *Indexer) RemoveFile(relativePath string) error {
	ctx := context.Background()
	if err := idx.admit(relativePath, true); err != nil {
		return err
	}
	_, err := idx.storage.DeleteNote(ctx, relativePath)
	if err != nil {
		return fmt.Errorf("delete note %q: %w", relativePath, err)
	}
	return nil
}

// GetHealth returns health statistics about the index.
func (idx *Indexer) GetHealth() (*IndexHealth, error) {
	// Count disk files
	diskFiles, err := idx.markdownFiles()
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}
	diskSet := make(map[string]bool, len(diskFiles))
	for _, f := range diskFiles {
		diskSet[f] = true
	}

	// Count indexed and stale entries from one tenant-scoped snapshot.
	states, err := idx.storage.ListIndexedNoteStates(context.Background())
	if err != nil {
		return nil, fmt.Errorf("query indexed notes: %w", err)
	}

	var staleCount int
	for _, state := range states {
		if !diskSet[state.Path] {
			staleCount++
		}
	}

	return &IndexHealth{
		TotalFiles:   len(diskFiles),
		TotalIndexed: len(states),
		StaleCount:   staleCount,
	}, nil
}

// IndexEmbeddings generates and stores embeddings for notes that need them.
// It uses staleness checks to avoid re-embedding unchanged notes.
// Embedding generation is best-effort: errors are logged but do not break the process.
func (idx *Indexer) IndexEmbeddings(ctx context.Context, embeddingClient EmbeddingClient) (*EmbeddingIndexResult, error) {
	return idx.IndexEmbeddingsWithOptions(ctx, embeddingClient, EmbeddingIndexOptions{})
}

// EmbeddingIndexOptions filters and controls embedding generation.
type EmbeddingIndexOptions struct {
	Project string
	Path    string
	Force   bool
}

// ListEmbeddingBackfillCandidates returns notes matching an embedding backfill request.
func (idx *Indexer) ListEmbeddingBackfillCandidates(ctx context.Context, opts EmbeddingIndexOptions) ([]EmbeddingBackfillCandidate, error) {
	notes, err := idx.storage.ListEmbeddingNotes(ctx, opts.Project, opts.Path, opts.Force)
	if err != nil {
		return nil, err
	}
	var candidates []EmbeddingBackfillCandidate
	for _, n := range notes {
		candidates = append(candidates, EmbeddingBackfillCandidate{ID: n.ID, Path: n.Path, Title: n.Title, Project: n.ProjectID, Type: n.Type})
	}
	return candidates, nil
}

// IndexEmbeddingsWithOptions generates and stores embeddings for matching notes.
func (idx *Indexer) IndexEmbeddingsWithOptions(ctx context.Context, embeddingClient EmbeddingClient, opts EmbeddingIndexOptions) (*EmbeddingIndexResult, error) {
	if embeddingClient == nil {
		return nil, fmt.Errorf("embedding client is nil")
	}

	start := time.Now()
	var processed, skipped, failed int

	ownedNotes, err := idx.storage.ListEmbeddingNotes(ctx, opts.Project, opts.Path, opts.Force)
	if err != nil {
		return nil, fmt.Errorf("query stale notes: %w", err)
	}

	// Process each note
	for _, note := range ownedNotes {
		if opts.Force {
			if err := idx.storage.DeleteNoteEmbeddings(ctx, note.ID); err != nil {
				slog.Warn("failed to delete existing embeddings for note", "note_id", note.ID, "error", err)
				failed++
				continue
			}
		}
		embeddingSource, err := idx.embeddingSourceForNote(ctx, note.ID)
		if err != nil {
			slog.Warn("failed to build embedding source for note",
				"note_id", note.ID,
				"error", err)
			failed++
			continue
		}

		// Skip notes whose combined embedding source is empty.
		if strings.TrimSpace(embeddingSource) == "" {
			skipped++
			continue
		}

		// Generate chunks
		chunks := markdown.ChunkNote(note.ID, embeddingSource)
		if len(chunks) == 0 {
			skipped++
			continue
		}

		// Extract chunk texts for embedding
		chunkTexts := make([]string, len(chunks))
		for i, chunk := range chunks {
			chunkTexts[i] = chunk.Text
		}

		// Generate embeddings (best-effort)
		embeddings, err := embeddingClient.Embed(ctx, chunkTexts)
		if err != nil {
			slog.Warn("failed to generate embeddings for note",
				"note_id", note.ID,
				"error", err)
			failed++
			continue
		}

		if len(embeddings) != len(chunks) {
			slog.Warn("embedding count mismatch",
				"note_id", note.ID,
				"expected", len(chunks),
				"got", len(embeddings))
			failed++
			continue
		}

		// Build EmbeddingRecord slice
		records := make([]storage.EmbeddingRecord, len(chunks))
		for i, chunk := range chunks {
			records[i] = storage.EmbeddingRecord{
				NoteID:     note.ID,
				ChunkIndex: chunk.ChunkIndex,
				Vector:     embeddings[i],
				ProjectID:  note.ProjectID,
				Type:       note.Type,
				Status:     note.Status,
				FeatureID:  note.FeatureID,
				Priority:   note.Priority,
			}
		}

		// Upsert embeddings (best-effort)
		if err := idx.storage.UpsertNoteEmbeddings(ctx, records); err != nil {
			slog.Warn("failed to upsert embeddings for note",
				"note_id", note.ID,
				"error", err)
			failed++
			continue
		}

		processed++
	}

	return &EmbeddingIndexResult{
		Processed: processed,
		Skipped:   skipped,
		Failed:    failed,
		Duration:  time.Since(start),
	}, nil
}

func (idx *Indexer) embeddingSourceForNote(ctx context.Context, noteID int64) (string, error) {
	return idx.storage.EmbeddingSource(ctx, noteID)
}

// GetEmbeddingHealth returns statistics about the embedding index.
func (idx *Indexer) GetEmbeddingHealth() (*EmbeddingHealth, error) {
	totalNotes, notesWithEmbeddings, staleEmbeddings, err := idx.storage.EmbeddingHealthCounts(context.Background())
	if err != nil {
		return nil, err
	}

	// Count notes without embeddings
	notesWithoutEmbeddings := totalNotes - notesWithEmbeddings

	return &EmbeddingHealth{
		TotalNotes:             totalNotes,
		NotesWithEmbeddings:    notesWithEmbeddings,
		NotesWithoutEmbeddings: notesWithoutEmbeddings,
		StaleEmbeddings:        staleEmbeddings,
	}, nil
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// toNoteRow maps a ParsedFile to a NoteRow for DB insertion.
func toNoteRow(pf *markdown.ParsedFile) storage.NoteRow {
	metadataJSON := "{}"
	if pf.Metadata != nil {
		if b, err := json.Marshal(pf.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	return storage.NoteRow{
		Path:       pf.Path,
		ShortID:    pf.ShortID,
		Title:      pf.Title,
		Lead:       strPtr(pf.Lead),
		Body:       strPtr(pf.Body),
		RawContent: strPtr(pf.RawContent),
		WordCount:  pf.WordCount,
		Checksum:   strPtr(pf.Checksum),
		Metadata:   metadataJSON,
		Type:       pf.Type,
		Status:     pf.Status,
		Priority:   pf.Priority,
		ProjectID:  pf.ProjectID,
		FeatureID:  pf.FeatureID,
		Created:    strPtr(pf.Created),
		Modified:   strPtr(pf.Modified),
	}
}

// noteUpdateMap builds the update map for storage.UpdateNote from a ParsedFile.
func noteUpdateMap(pf *markdown.ParsedFile) map[string]interface{} {
	metadataJSON := "{}"
	if pf.Metadata != nil {
		if b, err := json.Marshal(pf.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	return map[string]interface{}{
		"title":       pf.Title,
		"lead":        strPtr(pf.Lead),
		"body":        strPtr(pf.Body),
		"raw_content": strPtr(pf.RawContent),
		"word_count":  pf.WordCount,
		"checksum":    strPtr(pf.Checksum),
		"metadata":    metadataJSON,
		"type":        pf.Type,
		"status":      pf.Status,
		"priority":    pf.Priority,
		"project_id":  pf.ProjectID,
		"feature_id":  pf.FeatureID,
		"created":     strPtr(pf.Created),
		"modified":    strPtr(pf.Modified),
	}
}

// toLinkInputs maps ExtractedLink slice to LinkInput slice.
func toLinkInputs(links []markdown.ExtractedLink) []storage.LinkInput {
	inputs := make([]storage.LinkInput, len(links))
	for i, link := range links {
		inputs[i] = storage.LinkInput{
			TargetPath: link.Href,
			Title:      link.Title,
			Href:       link.Href,
			Type:       link.Type,
			Snippet:    link.Snippet,
		}
	}
	return inputs
}

// inContentScope checks the exact first component of a slash-separated relative
// path. Callers handle the traversal root separately. This is a discovery policy,
// not a replacement for the parser's path containment checks.
func inContentScope(relativePath string) bool {
	first, _, _ := strings.Cut(relativePath, "/")
	return first == "projects" || first == "global"
}

// globMarkdownFiles returns relative .md paths under projects/ and global/ only.
// WalkDir prunes other top-level directories before reading their contents and
// does not recurse through directory symlinks.
func globMarkdownFiles(brainDir string, policies ...*tenantfs.Root) ([]string, error) {
	var files []string
	err := filepath.WalkDir(brainDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(brainDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		// Admit the root itself, but prune unrelated siblings before resolving
		// tenant policy (an excluded dangling symlink is not content).
		if relPath != "." && !inContentScope(relPath) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if len(policies) > 0 && policies[0] != nil {
			if err := policies[0].AdmitTraversal(context.Background(), relPath); err != nil {
				if relPath == "." || !errors.Is(err, tenantfs.ErrDenied) {
					return err
				}
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		if relPath == "." {
			return nil
		}
		// Only .md files
		if !d.IsDir() && strings.HasSuffix(relPath, ".md") {
			// Normalize to forward slashes for consistency
			files = append(files, filepath.ToSlash(relPath))
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// strPtr returns a pointer to s, or nil if s is empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
