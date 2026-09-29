package storage

import (
	"database/sql"
	"testing"

	"github.com/huynle/brain-api/internal/tenantfs"
)

// Narrow identity-suite fixture: calls exercise the real guarded public
// adapters. The DB is retained only for independent SQL assertions/fixtures.
type identityTestFixture struct {
	*ControlStore
	*TokenAdmin
	db *sql.DB
}

func newIdentityTestStorage(t *testing.T) *identityTestFixture {
	t.Helper()
	s := newTestStorage(t)
	return &identityTestFixture{controlHandle(t, s), adminHandle(t, s), s.db}
}

func adminHandle(t *testing.T, s *StorageLayer) *TokenAdmin {
	t.Helper()
	a, err := controlHandle(t, s).TokenAdmin(operatorCapability(t))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func registryHandle(t testing.TB, s *StorageLayer) tenantfs.Repository {
	t.Helper()
	r, err := controlHandle(t, s).TenantRegistry(operatorCapability(t))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
