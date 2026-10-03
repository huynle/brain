package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/huynle/brain-api/internal/types"
)

func (s *TenantStore) SyncDevices(ctx context.Context) ([]types.SyncDevice, error) {
	tx, err := beginSync(ctx, s)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := s.syncScope(ctx, tx.Tx, true)
	if err != nil {
		return nil, err
	}
	pred, args := scope.where("1=1")
	rows, err := tx.QueryContext(ctx, "SELECT data FROM entry_sync_devices WHERE "+pred+" ORDER BY id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []types.SyncDevice{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var d types.SyncDevice
		if err = json.Unmarshal([]byte(raw), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Retains main's byte-JSON CAS contract; migration never reserializes stored JSON.
func (s *TenantStore) SaveSyncDevice(ctx context.Context, before *types.SyncDevice, after types.SyncDevice) error {
	tx, err := beginSync(ctx, s)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := s.syncScope(ctx, tx.Tx, true)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	if before == nil {
		cols, values, args := "id,data", "?,?", []any{after.ID, string(raw)}
		if scope.owner != "" {
			cols += ",tenant_id"
			values += ",?"
			args = append(args, scope.owner)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO entry_sync_devices("+cols+") VALUES("+values+")", args...); err != nil {
			return err
		}
	} else {
		old, e := json.Marshal(before)
		if e != nil {
			return e
		}
		pred, args := scope.where("id=? AND data=?", string(raw), after.ID, string(old))
		result, e := tx.ExecContext(ctx, "UPDATE entry_sync_devices SET data=? WHERE "+pred, args...)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return errors.New("device changed; retry")
		}
	}
	return tx.Commit()
}

func (s *TenantStore) SyncNote(ctx context.Context, path string) (*NoteRow, error) {
	tx, err := beginSync(ctx, s)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	scope, err := s.syncScope(ctx, tx.Tx, false)
	if err != nil {
		return nil, err
	}
	pred, args := scope.where("path=?", path)
	n, err := scanNoteRow(tx.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE "+pred, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return n, err
}
