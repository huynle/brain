package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalDatabaseOwnerAuthentication(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "brain.db")
	if err := os.WriteFile(path, []byte("SQLite format 3\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	cap, err := AuthenticateLocalDatabaseOwner(path)
	if err != nil || !cap.Valid() {
		t.Fatalf("OS-authorized database owner rejected: %v", err)
	}
	for _, invalid := range []string{"", dir, filepath.Join(dir, "missing")} {
		cap, err := AuthenticateLocalDatabaseOwner(invalid)
		if cap.Valid() || !errors.Is(err, ErrOperatorRequired) {
			t.Fatalf("invalid owner path %q: %v", invalid, err)
		}
	}
	// No artifact creation, chmod, or content mutation is permitted by authentication.
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "SQLite format 3\x00" {
		t.Fatalf("authentication mutated database: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatal("authentication created a missing database")
	}
}
