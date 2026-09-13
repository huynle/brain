package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// installClaimedPath is a reserved, presence-only entry_meta row, not a note.
// Never clear it when credentials are revoked, deleted, or expire.
const installClaimedPath = "brain:system/install_claimed"

// BootstrapClosedError means this installation has already been claimed.
// Callers can distinguish refusal from storage failures with errors.As.
type BootstrapClosedError struct{}

func (*BootstrapClosedError) Error() string { return "bootstrap closed" }

// BootstrapToken atomically claims an unconfigured installation and inserts an
// admin:* token. passwordConfigured must reflect the caller's configured password
// hash; storage deliberately does not read environment or server configuration.
// A credential-based refusal commits the claim; an insertion failure rolls it back.
func (s identityStore) bootstrapToken(ctx context.Context, name, token string, passwordConfigured bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Write FIRST: SQLite serializes writers across independent handles/processes.
	// A read before this write would risk a stale snapshot / SQLITE_BUSY upgrade.
	result, err := insertInstallClaim(ctx, tx)
	if err != nil {
		return err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("bootstrap claim result: %w", err)
	}
	if inserted == 0 {
		return &BootstrapClosedError{}
	}
	var credentials bool
	if err := tx.QueryRowContext(ctx, "SELECT "+activeInstallCredentials, time.Now().Unix()).Scan(&credentials); err != nil {
		return fmt.Errorf("check bootstrap credentials: %w", err)
	}
	if credentials || passwordConfigured {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit bootstrap closure: %w", err)
		}
		return &BootstrapClosedError{}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO api_tokens (name, token, scope) VALUES (?, ?, 'admin:*')", name, token); err != nil {
		return fmt.Errorf("insert bootstrap token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bootstrap: %w", err)
	}
	return nil
}

// MarkInstallClaimed permanently and idempotently closes bootstrap. Server startup
// should call this when a password hash is configured, before serving requests.
func (s identityStore) markInstallClaimed(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := insertInstallClaim(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// Reserve the writer before reading the version. Routing and claim writes share
// the caller's transaction; never query the pool while its connection is held.
func installClaimTable(ctx context.Context, tx *sql.Tx) (string, error) {
	if _, err := tx.ExecContext(ctx, "UPDATE main.schema_version SET version=version WHERE 0"); err != nil {
		return "", err
	}
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM main.schema_version").Scan(&version); err != nil {
		return "", err
	}
	switch version {
	case 28:
		return "entry_meta", nil
	case 29:
		return "operator_install_claim", nil
	case successorSchemaVersion:
		if err := validateSuccessorSchema(ctx, tx, true); err != nil {
			return "", err
		}
		return "operator_install_claim", nil
	default:
		return "", fmt.Errorf("unsupported identity schema %d", version)
	}
}

func insertInstallClaim(ctx context.Context, tx *sql.Tx) (sql.Result, error) {
	table, err := installClaimTable(ctx, tx)
	if err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (path) VALUES (?) ON CONFLICT(path) DO NOTHING`, installClaimedPath)
	if err != nil {
		return nil, fmt.Errorf("mark installation claimed: %w", err)
	}
	return result, nil
}

// OAuth validation accepts the expiry second itself (now <= expires_at).
const activeInstallCredentials = `EXISTS(SELECT 1 FROM api_tokens WHERE revoked_at IS NULL)
	OR EXISTS(SELECT 1 FROM oauth_access_tokens WHERE expires_at >= ?)`

// Backfill on every storage open, including databases already at current schema.
// INSERT ... SELECT is one write statement, with no read-to-write upgrade race.
func (s identityStore) backfillInstallClaim(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	table, err := installClaimTable(ctx, tx)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO `+table+` (path) SELECT ? WHERE `+activeInstallCredentials+`
		ON CONFLICT(path) DO NOTHING`, installClaimedPath, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("backfill installation claim: %w", err)
	}
	return tx.Commit()
}
