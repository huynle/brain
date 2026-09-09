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
func (s *StorageLayer) BootstrapToken(ctx context.Context, name, token string, passwordConfigured bool) error {
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
func (s *StorageLayer) MarkInstallClaimed(ctx context.Context) error {
	_, err := insertInstallClaim(ctx, s.db)
	return err
}

type installClaimWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertInstallClaim(ctx context.Context, writer installClaimWriter) (sql.Result, error) {
	result, err := writer.ExecContext(ctx, `INSERT INTO entry_meta (path) VALUES (?) ON CONFLICT(path) DO NOTHING`, installClaimedPath)
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
func (s *StorageLayer) backfillInstallClaim(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO entry_meta (path) SELECT ? WHERE `+activeInstallCredentials+`
		ON CONFLICT(path) DO NOTHING`, installClaimedPath, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("backfill installation claim: %w", err)
	}
	return nil
}
