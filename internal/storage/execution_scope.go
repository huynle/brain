package storage

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

// No staging admission. A successor must pass exact shared catalog validation
// and bound-tenant readiness, not a global migration/file rehearsal.
// Main's already-keyed ledgers remain local-only on exact archived profiles.
// This preflight is not authorization or a concurrent schema-migration fence.
func (s *TenantStore) executionScope(ctx context.Context, group string) (string, error) {
	if s == nil || s.db == nil || !s.tenantID.Valid() || ctx == nil {
		return "", fmt.Errorf("execution ledger requires a valid bound store and context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	id, ok := tenant.From(ctx)
	if !ok || id != s.tenantID {
		return "", fmt.Errorf("execution ledger tenant scope mismatch")
	}
	if group != "bulk" && group != "budget" && group != "supervisor" {
		return "", fmt.Errorf("unknown execution ledger group")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM main.schema_version").Scan(&version); err != nil {
		return "", err
	}
	if version == successorSchemaVersion {
		if err = validateSuccessorReceiver(ctx, tx, s.tenantID); err != nil {
			return "", err
		}
		return id.String(), nil
	}
	if id != tenant.Local || version < 29 || version > 30 {
		return "", fmt.Errorf("execution ledger schema unavailable")
	}
	profile, err := classifySchemaSource(ctx, tx)
	if err != nil {
		return "", err
	}
	if profile == "main29" && group == "bulk" || profile == "main30-pre-sync" || profile == "main30-initial-sync" || profile == "main30-devices" {
		return id.String(), nil
	}
	return "", fmt.Errorf("execution ledger missing from source profile %s", profile)
}
