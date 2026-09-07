package service

import (
	"context"
	"errors"
	"path/filepath"

	"github.com/huynle/brain-api/internal/brainpath"
	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// Distinguish filesystem admission (including registry/I/O failures) from a
// best-effort write failure. Durable metadata must never bypass admission.
type filesystemAdmissionError struct{ error }

func (e filesystemAdmissionError) Unwrap() error { return e.error }

// Only an explicit exclusion is skippable. Registry and filesystem failures
// must not masquerade as an absent task or an empty successful listing.
func excludedFilesystemPath(err error) bool {
	return errors.Is(err, tenantfs.ErrDenied) || errors.Is(err, brainpath.ErrContainment)
}

func filesystemPolicy(idx *indexer.Indexer) *tenantfs.Root {
	if idx == nil {
		return nil
	}
	return idx.FilesystemPolicy()
}

// resolveFilesystemPath preserves unbound constructor compatibility. Server
// composition always supplies a bound indexer before any service or scan starts.
// These checks require a trusted, stable host filesystem, not atomic capabilities.
func resolveFilesystemPath(ctx context.Context, idx *indexer.Indexer, base, name string, write bool) (string, error) {
	if p := filesystemPolicy(idx); p != nil {
		if write {
			return p.ResolveForWrite(ctx, filepath.FromSlash(name))
		}
		return p.Resolve(ctx, filepath.FromSlash(name))
	}
	if write {
		return brainpath.ResolveForWrite(base, filepath.FromSlash(name))
	}
	return brainpath.Resolve(base, filepath.FromSlash(name))
}

func (s *BrainServiceImpl) filesystemPath(ctx context.Context, name string, write bool) (string, error) {
	return resolveFilesystemPath(ctx, s.indexer, s.config.BrainDir, name, write)
}

func (s *BrainServiceImpl) admittedRow(ctx context.Context, rowPath string) error {
	_, err := s.filesystemPath(ctx, rowPath, true)
	return err
}

// absoluteFilesystemGuard adapts legacy directory helpers without giving them
// provisioning authority. It is also used on each child, not just its directory.
func absoluteFilesystemGuard(ctx context.Context, idx *indexer.Indexer, base string) func(string) error {
	return func(path string) error {
		name, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		_, err = resolveFilesystemPath(ctx, idx, base, name, true)
		return err
	}
}

func checkFilesystemGuards(path string, guards []func(string) error) error {
	for _, guard := range guards {
		if err := guard(path); err != nil {
			return err
		}
	}
	return nil
}
