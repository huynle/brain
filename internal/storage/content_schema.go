package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

// contentScope is resolved at execution time, never at binding time. Runtime
// constructors still support only v28. v29 is exercised by the dormant migration.
// Remove the local-only v28 route at final atomic activation, together with all
// legacyLocalContent bridges; never retain it as a v29 error recovery path.
type contentScope struct {
	owner string
}

func (s *TenantStore) contentScope(ctx context.Context) (contentScope, error) {
	if ctx == nil || s == nil || !s.tenantID.Valid() || s.StorageLayer == nil || s.db == nil {
		return contentScope{}, errors.New("invalid tenant content handle or context")
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM main.schema_version").Scan(&version); err != nil {
		return contentScope{}, fmt.Errorf("tenant content schema: %w", err)
	}
	switch version {
	case 28:
		if s.tenantID != tenant.Local {
			return contentScope{}, errors.New("v28 content requires local tenant")
		}
		return contentScope{}, nil
	case 29:
		return contentScope{owner: s.tenantID.String()}, nil
	default:
		return contentScope{}, fmt.Errorf("unsupported tenant content schema %d", version)
	}
}

func (scope contentScope) where(predicate string, args ...interface{}) (string, []interface{}) {
	if scope.owner != "" {
		predicate += " AND tenant_id = ?"
		args = append(args, scope.owner)
	}
	return predicate, args
}

// legacyLocalContent is a temporary compile bridge for unmoved attachment and
// trigger receivers. It cannot bind local to a v29 database.
// Remove its callers as those planes migrate; do not add raw CRUD methods.
func legacyLocalContent(ctx context.Context, s *StorageLayer) (*TenantStore, error) {
	h, err := s.ForTenant(tenant.Local)
	if err != nil {
		return nil, err
	}
	scope, err := h.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		return nil, errors.New("unmigrated content caller requires v28")
	}
	return h, nil
}

func legacyNoteByPath(ctx context.Context, s *StorageLayer, path string) (*NoteRow, error) {
	h, err := legacyLocalContent(ctx, s)
	if err != nil {
		return nil, err
	}
	return h.GetNoteByPath(ctx, path)
}
