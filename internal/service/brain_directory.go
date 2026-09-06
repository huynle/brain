package service

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/internal/brainpath"
)

// readBrainDirectory preflights every child before callers read or mutate any
// of them. Validating just the directory misses escaping file symlinks. Return
// lexical paths so callers retain unlink semantics and logical index keys.
// Like brainpath, this assumes the filesystem is not changed during use.
func readBrainDirectory(root, name string) (string, []os.DirEntry, error) {
	dir, err := brainpath.ResolveForWrite(root, name)
	if err != nil {
		return "", nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir, nil, err
	}
	valid := entries[:0]
	for _, entry := range entries {
		if _, err := brainpath.ResolveForWrite(root, filepath.Join(name, entry.Name())); err != nil {
			if errors.Is(err, brainpath.ErrContainment) {
				return "", nil, err
			}
			// Preserve best-effort scans without reading an unvalidated child.
			continue
		}
		valid = append(valid, entry)
	}
	return dir, valid, nil
}
