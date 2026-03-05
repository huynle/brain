package indexer

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/pkg/markdown"
)

// Indexer synchronizes markdown files on disk with the SQLite database.
type Indexer struct {
	brainDir string
	storage  *storage.StorageLayer
}

// NewIndexer creates a new Indexer for the given brain directory and storage layer.
func NewIndexer(brainDir string, store *storage.StorageLayer) *Indexer {
	return &Indexer{
		brainDir: brainDir,
		storage:  store,
	}
}

// RebuildAll performs a full rebuild: deletes all existing data and re-indexes
// every .md file on disk.
func (idx *Indexer) RebuildAll() (*IndexResult, error) {
	start := time.Now()
	ctx := context.Background()
	var indexErrors []IndexError

	// 1. Discover files on disk
	files, err := globMarkdownFiles(idx.brainDir)
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}

	// 2. Parse all files, collecting results and errors
	var parsed []*markdown.ParsedFile
	for _, file := range files {
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

	// 3. Count existing notes before clearing (for deleted stat)
	var existingCount int
	err = idx.storage.DB().QueryRow("SELECT COUNT(*) FROM notes").Scan(&existingCount)
	if err != nil {
		return nil, fmt.Errorf("count existing notes: %w", err)
	}

	// 4. Delete all existing notes (CASCADE cleans links and tags)
	_, err = idx.storage.DB().Exec("DELETE FROM notes")
	if err != nil {
		return nil, fmt.Errorf("delete all notes: %w", err)
	}

	// 5. Insert each parsed file
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
		Deleted:  existingCount,
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
	diskFiles, err := globMarkdownFiles(idx.brainDir)
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}
	diskSet := make(map[string]bool, len(diskFiles))
	for _, f := range diskFiles {
		diskSet[f] = true
	}

	// 2. Get all existing notes from DB (path + checksum)
	rows, err := idx.storage.DB().Query("SELECT path, checksum FROM notes")
	if err != nil {
		return nil, fmt.Errorf("query existing notes: %w", err)
	}
	defer rows.Close()

	dbMap := make(map[string]*string) // path → checksum (nullable)
	for rows.Next() {
		var path string
		var checksum *string
		if err := rows.Scan(&path, &checksum); err != nil {
			return nil, fmt.Errorf("scan note row: %w", err)
		}
		dbMap[path] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate note rows: %w", err)
	}

	// 3. Process each file on disk
	for _, file := range diskFiles {
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

	// 4. Delete DB entries with no corresponding file on disk
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
	_, err := idx.storage.DeleteNote(ctx, relativePath)
	if err != nil {
		return fmt.Errorf("delete note %q: %w", relativePath, err)
	}
	return nil
}

// GetHealth returns health statistics about the index.
func (idx *Indexer) GetHealth() (*IndexHealth, error) {
	// Count disk files
	diskFiles, err := globMarkdownFiles(idx.brainDir)
	if err != nil {
		return nil, fmt.Errorf("glob markdown files: %w", err)
	}
	diskSet := make(map[string]bool, len(diskFiles))
	for _, f := range diskFiles {
		diskSet[f] = true
	}

	// Count DB entries
	var totalIndexed int
	if err := idx.storage.DB().QueryRow("SELECT COUNT(*) FROM notes").Scan(&totalIndexed); err != nil {
		return nil, fmt.Errorf("count indexed notes: %w", err)
	}

	// Count stale entries (in DB but not on disk)
	rows, err := idx.storage.DB().Query("SELECT path FROM notes")
	if err != nil {
		return nil, fmt.Errorf("query note paths: %w", err)
	}
	defer rows.Close()

	var staleCount int
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan path: %w", err)
		}
		if !diskSet[path] {
			staleCount++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate paths: %w", err)
	}

	return &IndexHealth{
		TotalFiles:   len(diskFiles),
		TotalIndexed: totalIndexed,
		StaleCount:   staleCount,
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

// globMarkdownFiles walks brainDir and returns relative paths of all .md files,
// excluding the .zk/ directory.
func globMarkdownFiles(brainDir string) ([]string, error) {
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

		// Skip .zk/ directory
		if d.IsDir() && (relPath == ".zk" || strings.HasPrefix(relPath, ".zk"+string(os.PathSeparator))) {
			return filepath.SkipDir
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
