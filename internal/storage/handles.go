package storage

import (
	"database/sql"
	"errors"

	"github.com/huynle/brain-api/internal/tenant"
)

// TenantStore names a tenant; it does not authorize access. It borrows the shared
// pool without exposing owner lifecycle, rebinding or identity operations.
// Workload methods validate execution-time schema routing: local-only v28 or
// tenant predicates on privately staged v29. Runtime multi mode remains disabled.
type TenantStore struct {
	db       *sql.DB
	tenantID tenant.ID
}

// ForTenant only validates and wraps the existing shared store. It does not open
// a pool, initialize schema, register roots, or check that the tenant exists.
func (s *StorageLayer) ForTenant(id tenant.ID) (*TenantStore, error) {
	if !id.Valid() {
		return nil, errors.New("invalid tenant ID")
	}
	if s == nil {
		return nil, errors.New("nil storage layer")
	}
	return &TenantStore{db: s.db, tenantID: id}, nil
}

// TenantID returns the immutable binding, not an authorization grant.
func (s *TenantStore) TenantID() tenant.ID { return s.tenantID }
