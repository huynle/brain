package blobstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFilesystemStorePutGetDeleteRoundTrip(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	payload := []byte("attachment bytes")
	hash, size, err := store.Put(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	wantHash := sha256Hex(payload)
	if hash != wantHash {
		t.Fatalf("Put() hash = %q, want %q", hash, wantHash)
	}
	if size != int64(len(payload)) {
		t.Fatalf("Put() size = %d, want %d", size, len(payload))
	}

	gotReader, err := store.Get(hash)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	defer gotReader.Close()

	got, err := io.ReadAll(gotReader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("Get() bytes = %q, want %q", got, payload)
	}

	if err := store.Delete(hash); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	_, err = store.Get(hash)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete error = %v, want ErrNotFound", err)
	}
}

func TestFilesystemStoreDuplicateBytesAreIdempotent(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, 1024)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	payload := []byte("same bytes")
	hash1, size1, err := store.Put(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("first Put() error = %v", err)
	}
	hash2, size2, err := store.Put(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("second Put() error = %v", err)
	}

	if hash1 != hash2 || size1 != size2 {
		t.Fatalf("duplicate Put() = (%q,%d), want (%q,%d)", hash2, size2, hash1, size1)
	}

	files := regularFilesUnder(t, root)
	if len(files) != 1 {
		t.Fatalf("regular files under root = %v, want exactly one blob", files)
	}
}

func TestFilesystemStoreMissingBlob(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	missingHash := strings.Repeat("a", 64)
	_, err = store.Get(missingHash)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if err := store.Delete(missingHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete() error = %v, want ErrNotFound", err)
	}
}

func TestFilesystemStoreConcurrentDuplicatePutsAreIdempotent(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, 1024)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	payload := []byte("written concurrently")
	wantHash := sha256Hex(payload)

	var wg sync.WaitGroup
	errCh := make(chan error, 16)
	for i := 0; i < cap(errCh); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hash, size, err := store.Put(bytes.NewReader(payload))
			if err != nil {
				errCh <- err
				return
			}
			if hash != wantHash || size != int64(len(payload)) {
				errCh <- errors.New("unexpected hash or size")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent Put() error = %v", err)
		}
	}

	files := regularFilesUnder(t, root)
	if len(files) != 1 {
		t.Fatalf("regular files under root = %v, want exactly one blob", files)
	}
}

func TestFilesystemStoreEnforcesSizeLimitAndCleansTempFiles(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, 4)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	_, _, err = store.Put(bytes.NewReader([]byte("12345")))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Put() error = %v, want ErrTooLarge", err)
	}

	files := regularFilesUnder(t, root)
	if len(files) != 0 {
		t.Fatalf("regular files under root = %v, want no temp/blob files", files)
	}
}

func TestFilesystemStoreRejectsInvalidHashesAndTraversalInputs(t *testing.T) {
	store, err := NewFilesystemStore(t.TempDir(), 1024)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	tests := []string{
		"",
		"abc",
		strings.Repeat("A", 64),
		strings.Repeat("g", 64),
		"../" + strings.Repeat("a", 61),
		strings.Repeat("a", 63) + "/",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := store.Get(input); !errors.Is(err, ErrInvalidHash) {
				t.Fatalf("Get(%q) error = %v, want ErrInvalidHash", input, err)
			}
			if err := store.Delete(input); !errors.Is(err, ErrInvalidHash) {
				t.Fatalf("Delete(%q) error = %v, want ErrInvalidHash", input, err)
			}
		})
	}
}

func TestFilesystemStoreUsesShardedHashPath(t *testing.T) {
	root := t.TempDir()
	store, err := NewFilesystemStore(root, 1024)
	if err != nil {
		t.Fatalf("NewFilesystemStore() error = %v", err)
	}

	payload := []byte("sharded")
	hash, _, err := store.Put(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if len(hash) != 64 {
		t.Fatalf("Put() hash length = %d, want 64 (%q)", len(hash), hash)
	}

	wantPath := filepath.Join(root, hash[:2], hash[2:4], hash)
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected blob at sharded hash path %s: %v", wantPath, err)
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func regularFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s) error = %v", root, err)
	}
	return files
}
