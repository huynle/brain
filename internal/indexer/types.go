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
