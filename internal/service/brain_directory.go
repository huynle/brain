package service

import (
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/internal/brainpath"
)

// readBrainDirectory preflights every child before callers read or mutate any
// of them. Validating just the directory misses escaping file symlinks. Return
// lexical paths so callers retain unlink semantics and logical index keys.
// Like brainpath, this assumes the filesystem is not changed during use.
// On error, a nonempty directory identifies an os.ReadDir failure only;
// admission failures (including child ENOENT) always return an empty directory.
func readBrainDirectory(root, name string, guards ...func(string) error) (string, []os.DirEntry, error) {
	dir, err := brainpath.ResolveForWrite(root, name)
	if err != nil {
		return "", nil, err
	}
	if err := checkFilesystemGuards(dir, guards); err != nil {
		return "", nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir, nil, err
	}
	valid := entries[:0]
	for _, entry := range entries {
		if _, err := brainpath.ResolveForWrite(root, filepath.Join(name, entry.Name())); err != nil {
			return "", nil, err
		}
		if err := checkFilesystemGuards(filepath.Join(dir, entry.Name()), guards); err != nil {
			return "", nil, err
		}
		valid = append(valid, entry)
	}
	return dir, valid, nil
}
