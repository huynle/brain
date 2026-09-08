package storage

import (
	"errors"

	"github.com/huynle/brain-api/internal/tenant"
)

// TenantStore names a tenant; it does not authorize access. Notes/list/search enforce
// execution-time schema routing and v29 tenant predicates; other content planes
// are still unmigrated.
// TEMPORARY until P4.10: embedding promotes ALL StorageLayer methods, including
// DB, Close, ValidateToken and unscoped queries. Multi mode must remain disabled.
type TenantStore struct {
	*StorageLayer
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
	return &TenantStore{StorageLayer: s, tenantID: id}, nil
}

// TenantID returns the immutable binding, not an authorization grant.
func (s *TenantStore) TenantID() tenant.ID { return s.tenantID }
