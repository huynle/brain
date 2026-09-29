// Package storagetest supplies local tenant fixtures. Production imports are
// forbidden by TestProductionStorageOwnership; storage implementation tests may
// continue to use raw owners directly.
package storagetest

import (
	"database/sql"
	"testing"

	"github.com/huynle/brain-api/internal/auth"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"golang.org/x/crypto/bcrypt"
)

// New registers owner cleanup before callers register worker cleanup (LIFO).
func New(t testing.TB, path string) (*storage.TenantStore, error) {
	t.Helper()
	owner, err := storage.New(path)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner.ForTenant(tenant.Local)
}

// NewWithDB borrows the caller-owned DB; the caller must drain workers then close.
func NewWithDB(db *sql.DB) (*storage.TenantStore, error) {
	owner, err := storage.NewWithDB(db)
	if err != nil {
		return nil, err
	}
	return owner.ForTenant(tenant.Local)
}

// Registry uses an explicitly retained test owner, never extracts it from a view.
func Registry(t testing.TB, owner *storage.StorageLayer) tenantfs.Repository {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("fixture"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(auth.EnvUsername, "fixture")
	t.Setenv(auth.EnvPasswordHash, string(hash))
	cap, err := auth.NewVerifierFromEnv().AuthenticateOperator("fixture", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	control, err := owner.Control()
	if err != nil {
		t.Fatal(err)
	}
	repo, err := control.TenantRegistry(cap)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

// RegistryWithDB is for fixtures that already retain and own their connection.
func RegistryWithDB(t testing.TB, db *sql.DB) tenantfs.Repository {
	t.Helper()
	owner, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatal(err)
	}
	return Registry(t, owner)
}

// NewWithRegistry explicitly separates workload and privileged fixture handles.
func NewWithRegistry(t testing.TB, path string) (*storage.TenantStore, tenantfs.Repository, error) {
	t.Helper()
	owner, err := storage.New(path)
	if err != nil {
		return nil, nil, err
	}
	t.Cleanup(func() { _ = owner.Close() })
	view, err := owner.ForTenant(tenant.Local)
	if err != nil {
		return nil, nil, err
	}
	return view, Registry(t, owner), nil
}
