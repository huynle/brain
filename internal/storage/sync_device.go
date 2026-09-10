package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

// A compare-and-swap prevents concurrent reports or agents losing commands.
func (s *TenantStore) SyncDevices(ctx context.Context) ([]types.SyncDevice, error) {
	if s == nil || s.tenantID.String() != tenant.LocalID {
		return nil, errors.New("sync devices require local tenant scope")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT data FROM entry_sync_devices ORDER BY id")
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
func (s *TenantStore) SaveSyncDevice(ctx context.Context, before *types.SyncDevice, after types.SyncDevice) error {
	if s == nil || s.tenantID.String() != tenant.LocalID {
		return errors.New("sync devices require local tenant scope")
	}
	raw, err := json.Marshal(after)
	if err != nil {
		return err
	}
	if before == nil {
		_, err = s.db.ExecContext(ctx, "INSERT INTO entry_sync_devices(id,data) VALUES (?,?)", after.ID, string(raw))
		return err
	}
	old, err := json.Marshal(before)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, "UPDATE entry_sync_devices SET data=? WHERE id=? AND data=?", string(raw), after.ID, string(old))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("device changed; retry")
	}
	return err
}

func (s *TenantStore) SyncNote(ctx context.Context, path string) (*NoteRow, error) {
	if s == nil || s.tenantID.String() != tenant.LocalID {
		return nil, errors.New("sync devices require local tenant scope")
	}
	n, err := scanNoteRow(s.db.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE path=?", path))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return n, err
}
