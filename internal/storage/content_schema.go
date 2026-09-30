package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

// contentScope is resolved at execution time, never at binding time. Public
// constructors admit reviewed single-mode sources and initialize runtime30.
// Private29/tenant31 remain dormant; neither may fall back to single-mode SQL.
type contentScope struct {
	owner string
}

func (s *TenantStore) contentScope(ctx context.Context) (contentScope, error) {
	if ctx == nil || s == nil || !s.tenantID.Valid() || s.db == nil {
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
		var owned int
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM main.sqlite_schema WHERE type='table' AND name='tenants'").Scan(&owned); err != nil {
			return contentScope{}, err
		}
		if owned != 0 {
			return contentScope{owner: s.tenantID.String()}, nil
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return contentScope{}, err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := classifySchemaSource(ctx, tx); err != nil {
			return contentScope{}, err
		}
		fallthrough
	case 30:
		if s.tenantID != tenant.Local {
			return contentScope{}, errors.New("main content requires local tenant")
		}
		// Public owner construction performs exact, read-only source admission.
		// Like legacy28, this route only permits the single-install local view.
		return contentScope{}, nil
	case successorSchemaVersion:
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return contentScope{}, err
		}
		defer func() { _ = tx.Rollback() }()
		if err = validateSuccessorReceiver(ctx, tx, s.tenantID); err != nil {
			return contentScope{}, err
		}
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
