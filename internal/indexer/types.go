// Package indexer synchronizes markdown files on disk with the SQLite database.
// It supports full rebuild, incremental updates, single-file operations,
// and file-system watching with debounced indexing.
package indexer

import "time"

// IndexResult holds statistics from an indexing operation.
type IndexResult struct {
	Added    int
	Updated  int
	Deleted  int
	Skipped  int
	Errors   []IndexError
	Duration time.Duration
}

// IndexError records a per-file error during indexing.
type IndexError struct {
	Path  string
	Error string
}

// IndexHealth reports the health of the index relative to disk.
type IndexHealth struct {
	TotalFiles   int
	TotalIndexed int
	StaleCount   int
}

// EmbeddingIndexResult holds statistics from an embedding indexing operation.
type EmbeddingIndexResult struct {
	Processed int           // Notes successfully processed
	Skipped   int           // Notes skipped (empty body)
	Failed    int           // Notes that failed to generate embeddings
	Duration  time.Duration // Total time taken
}

// EmbeddingBackfillCandidate describes a note that needs embedding generation.
type EmbeddingBackfillCandidate struct {
	ID      int64
	Path    string
	Title   string
	Project *string
	Type    *string
}

// EmbeddingHealth reports the health of the embedding index.
type EmbeddingHealth struct {
	TotalNotes             int // Total notes in database
	NotesWithEmbeddings    int // Notes that have embeddings
	NotesWithoutEmbeddings int // Notes without any embeddings
	StaleEmbeddings        int // Notes with outdated embeddings
}
