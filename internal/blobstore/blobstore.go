package blobstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNotFound    = errors.New("blob not found")
	ErrInvalidHash = errors.New("invalid blob hash")
	ErrTooLarge    = errors.New("blob too large")
)

type Store interface {
	Put(r io.Reader) (hash string, size int64, err error)
	Get(hash string) (io.ReadCloser, error)
	Delete(hash string) error
}

type FilesystemStore struct {
	root         string
	maxSizeBytes int64
}

func NewFilesystemStore(root string, maxSizeBytes int64) (*FilesystemStore, error) {
	if root == "" {
		return nil, fmt.Errorf("blobstore init: root is required")
	}
	if maxSizeBytes <= 0 {
		return nil, fmt.Errorf("blobstore init: max size must be positive")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("blobstore init: resolve root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0o700); err != nil {
		return nil, fmt.Errorf("blobstore init: create root: %w", err)
	}
	return &FilesystemStore{root: absRoot, maxSizeBytes: maxSizeBytes}, nil
}

func (s *FilesystemStore) Put(r io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(s.root, ".blob-*")
	if err != nil {
		return "", 0, fmt.Errorf("blobstore put: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	h := sha256.New()
	w := io.MultiWriter(tmp, h)
	size, err := copyWithLimit(w, r, s.maxSizeBytes)
	if err != nil {
		_ = tmp.Close()
		return "", size, fmt.Errorf("blobstore put: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", size, fmt.Errorf("blobstore put: sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", size, fmt.Errorf("blobstore put: close temp file: %w", err)
	}

	hash := hex.EncodeToString(h.Sum(nil))
	finalPath, err := s.pathForHash(hash)
	if err != nil {
		return "", size, fmt.Errorf("blobstore put: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return "", size, fmt.Errorf("blobstore put: create shard directory: %w", err)
	}

	if _, err := os.Stat(finalPath); err == nil {
		return hash, size, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", size, fmt.Errorf("blobstore put: stat final blob: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		if _, statErr := os.Stat(finalPath); statErr == nil {
			return hash, size, nil
		}
		return "", size, fmt.Errorf("blobstore put: rename temp blob: %w", err)
	}
	if err := syncDir(filepath.Dir(finalPath)); err != nil {
		return "", size, fmt.Errorf("blobstore put: sync shard directory: %w", err)
	}

	return hash, size, nil
}

func (s *FilesystemStore) Get(hash string) (io.ReadCloser, error) {
	path, err := s.pathForHash(hash)
	if err != nil {
		return nil, fmt.Errorf("blobstore get: %w", err)
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("blobstore get: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("blobstore get: open blob: %w", err)
	}
	return f, nil
}

func (s *FilesystemStore) Delete(hash string) error {
	path, err := s.pathForHash(hash)
	if err != nil {
		return fmt.Errorf("blobstore delete: %w", err)
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("blobstore delete: %w", ErrNotFound)
		}
		return fmt.Errorf("blobstore delete: remove blob: %w", err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil {
		return fmt.Errorf("blobstore delete: sync shard directory: %w", err)
	}
	return nil
}

func (s *FilesystemStore) pathForHash(hash string) (string, error) {
	if !isValidSHA256Hex(hash) {
		return "", ErrInvalidHash
	}
	path := filepath.Join(s.root, hash[:2], hash[2:4], hash)
	if !isPathWithinRoot(s.root, path) {
		return "", ErrInvalidHash
	}
	return path, nil
}

func isValidSHA256Hex(hash string) bool {
	if len(hash) != sha256.Size*2 {
		return false
	}
	for _, r := range hash {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func copyWithLimit(dst io.Writer, src io.Reader, max int64) (int64, error) {
	limited := io.LimitReader(src, max+1)
	n, err := io.Copy(dst, limited)
	if err != nil {
		return n, err
	}
	if n > max {
		return n, ErrTooLarge
	}
	return n, nil
}

func isPathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
