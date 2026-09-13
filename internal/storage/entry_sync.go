package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

type EntrySyncRow struct {
	Sequence int64
	Path     string
	Note     *NoteRow
}
type EntrySyncPage struct {
	Epoch  string
	Cursor int64
	More   bool
	Rows   []EntrySyncRow
}
type SyncReceipt struct {
	Status int
	Body   string
}

var ErrSyncReset = errors.New("sync database changed; bootstrap required")

// Shared snapshot preflight for all sync operations. Main sync was unowned;
// successor sync is owned. No runtime initialization or live-note history repair.
func (s *TenantStore) syncScope(ctx context.Context, tx *sql.Tx, devices bool) (contentScope, error) {
	if s == nil || s.db == nil || !s.tenantID.Valid() || ctx == nil || tx == nil {
		return contentScope{}, fmt.Errorf("invalid sync handle or context")
	}
	id, ok := tenant.From(ctx)
	if !ok || id != s.tenantID {
		return contentScope{}, fmt.Errorf("sync tenant scope mismatch")
	}
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM main.schema_version").Scan(&version); err != nil {
		return contentScope{}, err
	}
	if version == 31 {
		if err := validateSuccessorSchema(ctx, tx, true); err != nil {
			return contentScope{}, err
		}
		return contentScope{owner: id.String()}, nil
	}
	if id != tenant.Local {
		return contentScope{}, fmt.Errorf("main sync requires local tenant")
	}
	profile, err := classifySchemaSource(ctx, tx)
	if err != nil {
		return contentScope{}, err
	}
	if profile == "main30-devices" || profile == "main30-initial-sync" && !devices {
		return contentScope{}, nil
	}
	return contentScope{}, fmt.Errorf("sync unavailable in %s", profile)
}

// Validate binding before BeginTx as nil/cancelled handles must fail, not panic.
// This helper never admits a schema; syncScope validates inside the snapshot.
func beginSync(ctx context.Context, s *TenantStore) (*sql.Tx, error) {
	if ctx == nil || s == nil || s.db == nil || !s.tenantID.Valid() {
		return nil, fmt.Errorf("invalid sync handle or context")
	}
	id, ok := tenant.From(ctx)
	if !ok || id != s.tenantID {
		return nil, fmt.Errorf("sync tenant scope mismatch")
	}
	return s.db.BeginTx(ctx, nil)
}

func (s *TenantStore) ReadEntryChanges(ctx context.Context, epoch string, after int64, limit int) (*EntrySyncPage, error) {
	tx, err := beginSync(ctx, s)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := s.syncScope(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	if after < 0 || limit < 1 || limit > 10000 {
		return nil, fmt.Errorf("invalid sync page bounds")
	}
	p := &EntrySyncPage{Cursor: after, Rows: []EntrySyncRow{}}
	pred, args := scope.where("id=1")
	if err = tx.QueryRowContext(ctx, "SELECT epoch FROM entry_sync_identity WHERE "+pred, args...).Scan(&p.Epoch); err != nil {
		return nil, err
	}
	pred, args = scope.where("1=1")
	var high int64
	if err = tx.QueryRowContext(ctx, "SELECT coalesce(max(seq),0) FROM entry_sync_changes WHERE "+pred, args...).Scan(&high); err != nil {
		return nil, err
	}
	if epoch != "" && epoch != p.Epoch || after > high {
		return nil, ErrSyncReset
	}
	pred, args = scope.where("seq>?", after)
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, "SELECT seq,path FROM entry_sync_changes WHERE "+pred+" ORDER BY seq LIMIT ?", args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r EntrySyncRow
		if err = rows.Scan(&r.Sequence, &r.Path); err != nil {
			_ = rows.Close()
			return nil, err
		}
		p.Rows = append(p.Rows, r)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if len(p.Rows) > limit {
		p.More = true
		p.Rows = p.Rows[:limit]
	}
	for i := range p.Rows {
		r := &p.Rows[i]
		pred, args = scope.where("path=?", r.Path)
		r.Note, err = scanNoteRow(tx.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE "+pred, args...))
		if errors.Is(err, sql.ErrNoRows) {
			r.Note = nil
		} else if err != nil {
			return nil, err
		}
		p.Cursor = r.Sequence
	}
	return p, tx.Commit()
}

func (s *TenantStore) ReadSelectedEntries(ctx context.Context, paths []string) (*EntrySyncPage, error) {
	tx, err := beginSync(ctx, s)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := s.syncScope(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	p := &EntrySyncPage{Rows: []EntrySyncRow{}}
	pred, args := scope.where("id=1")
	if err = tx.QueryRowContext(ctx, "SELECT epoch FROM entry_sync_identity WHERE "+pred, args...).Scan(&p.Epoch); err != nil {
		return nil, err
	}
	for _, path := range paths {
		pred, args = scope.where("path=?", path)
		n, e := scanNoteRow(tx.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE "+pred, args...))
		if errors.Is(e, sql.ErrNoRows) {
			n = nil
		} else if e != nil {
			return nil, e
		}
		p.Rows = append(p.Rows, EntrySyncRow{Path: path, Note: n})
	}
	return p, tx.Commit()
}

// Reservation status zero is durable uncertainty, never permission to replay.
func (s *TenantStore) ReserveSyncOperation(ctx context.Context, id, hash string) (*SyncReceipt, error) {
	tx, err := beginSync(ctx, s)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := s.syncScope(ctx, tx, false)
	if err != nil {
		return nil, err
	}
	cols, values, args := "id,hash", "?,?", []any{id, hash}
	if scope.owner != "" {
		cols += ",tenant_id"
		values += ",?"
		args = append(args, scope.owner)
	}
	result, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO entry_sync_operations("+cols+") VALUES("+values+")", args...)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 1 {
		return nil, tx.Commit()
	}
	var stored string
	r := &SyncReceipt{}
	pred, args := scope.where("id=?", id)
	if err = tx.QueryRowContext(ctx, "SELECT hash,status,body FROM entry_sync_operations WHERE "+pred, args...).Scan(&stored, &r.Status, &r.Body); err != nil {
		return nil, err
	}
	if stored != hash {
		return nil, fmt.Errorf("operation ID reused for different content")
	}
	return r, tx.Commit()
}

func (s *TenantStore) CompleteSyncOperation(ctx context.Context, id string, status int, body string) error {
	tx, err := beginSync(ctx, s)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := s.syncScope(ctx, tx, false)
	if err != nil {
		return err
	}
	pred, args := scope.where("id=?", status, body, id)
	if _, err = tx.ExecContext(ctx, "UPDATE entry_sync_operations SET status=?,body=? WHERE "+pred, args...); err != nil {
		return err
	}
	return tx.Commit()
}
