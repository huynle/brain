package storage

import (
	"context"
	"errors"

	"github.com/huynle/brain-api/internal/tenant"
)

// TenantStore names a tenant; it does not authorize access or isolate SQL yet.
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

// listQuery requires a tenant unconditionally, independently of optional list
// filters: empty tenant scope is an error, never an omitted constraint.
// P3 permits only local compatibility SQL against the pre-migration schema.
// P4 must add unconditional tenant predicates (including subqueries) after the
// schema migration, before allowing non-local queries. This is not SQL isolation:
// other promoted StorageLayer methods still bypass this boundary.
func (s *TenantStore) listQuery(opts *ListOptions) (string, []interface{}, error) {
	if s == nil || !s.tenantID.Valid() {
		return "", nil, errors.New("invalid tenant ID")
	}
	if s.StorageLayer == nil {
		return "", nil, errors.New("nil storage layer")
	}
	if s.tenantID.String() != tenant.LocalID {
		return "", nil, errors.New("tenant content queries require P4 schema and predicates")
	}
	query, args := buildListQuery(opts)
	return query, args, nil
}

// ListNotes validates the tenant before constructing or executing any SQL.
// Only the reserved local scope is supported until P4; multi mode stays disabled.
func (s *TenantStore) ListNotes(ctx context.Context, opts *ListOptions) ([]*NoteRow, error) {
	query, args, err := s.listQuery(opts)
	if err != nil {
		return nil, err
	}
	return s.StorageLayer.listNotes(ctx, query, args)
}
