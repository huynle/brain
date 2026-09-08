package storage

import (
	"errors"

	"github.com/huynle/brain-api/internal/tenant"
)

// TenantStore names a tenant; it does not authorize access. Notes/list/search,
// graph/links/tags, index maintenance, embeddings/events and attachment
// metadata/references/derivations enforce execution-time schema routing and v29
// tenant predicates. Other planes remain
// unmigrated; SQL attachment ownership does not bind or authorize a physical CAS.
// TEMPORARY until P4.10: embedding promotes ALL StorageLayer methods, including
// Close, ValidateToken and unscoped queries. The raw DB accessor is removed;
// multi mode must remain disabled until the remaining receiver moves/un-embedding.
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
