package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

// One latest change per path, including tombstones. REPLACE allocates a new
// monotonic sequence. Triggers commit with the indexed data, including writes
// from the file watcher and runtime metadata writers. No timestamp polling.
const entrySyncSchema = `
CREATE TABLE IF NOT EXISTS entry_sync_devices (id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS entry_sync_identity (id INTEGER PRIMARY KEY CHECK(id=1), epoch TEXT NOT NULL);
INSERT OR IGNORE INTO entry_sync_identity VALUES (1, lower(hex(randomblob(16))));
CREATE TABLE IF NOT EXISTS entry_sync_changes (seq INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE);
CREATE TABLE IF NOT EXISTS entry_sync_operations (id TEXT PRIMARY KEY, hash TEXT NOT NULL, status INTEGER NOT NULL DEFAULT 0, body TEXT NOT NULL DEFAULT '');
CREATE TRIGGER IF NOT EXISTS entry_sync_insert AFTER INSERT ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (new.path);
END;
CREATE TRIGGER IF NOT EXISTS entry_sync_update AFTER UPDATE ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (old.path);
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (new.path);
END;
CREATE TRIGGER IF NOT EXISTS entry_sync_delete AFTER DELETE ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (old.path);
END;
INSERT OR IGNORE INTO entry_sync_changes(path) SELECT path FROM notes;
`

func initEntrySync(db *sql.DB) error {
	_, err := db.Exec(entrySyncSchema)
	return err
}

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

// ReadEntryChanges reads both the change positions and their current payloads
// in ONE snapshot. A later update moves the path forward and is picked up on a
// later page; a client can never advance past a payload it has not received.
func (s *TenantStore) ReadEntryChanges(ctx context.Context, epoch string, after int64, limit int) (*EntrySyncPage, error) {
	if s == nil || s.tenantID.String() != tenant.LocalID {
		return nil, errors.New("entry sync requires local tenant scope")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	p := &EntrySyncPage{Cursor: after, Rows: []EntrySyncRow{}}
	if err = tx.QueryRowContext(ctx, `SELECT epoch FROM entry_sync_identity WHERE id=1`).Scan(&p.Epoch); err != nil {
		return nil, err
	}
	var high int64
	if err = tx.QueryRowContext(ctx, `SELECT coalesce(max(seq),0) FROM entry_sync_changes`).Scan(&high); err != nil {
		return nil, err
	}
	if (epoch != "" && epoch != p.Epoch) || after > high {
		return nil, ErrSyncReset
	}
	rows, err := tx.QueryContext(ctx, `SELECT seq,path FROM entry_sync_changes WHERE seq>? ORDER BY seq LIMIT ?`, after, limit+1)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r EntrySyncRow
		if err = rows.Scan(&r.Sequence, &r.Path); err != nil {
			rows.Close()
			return nil, err
		}
		p.Rows = append(p.Rows, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(p.Rows) > limit {
		p.More = true
		p.Rows = p.Rows[:limit]
	}
	for i := range p.Rows {
		r := &p.Rows[i]
		r.Note, err = scanNoteRow(tx.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE path=?", r.Path))
		if errors.Is(err, sql.ErrNoRows) {
			r.Note = nil
		} else if err != nil {
			return nil, err
		}
		p.Cursor = r.Sequence
	}
	return p, tx.Commit()
}

var ErrSyncReset = errors.New("sync database changed; bootstrap required")

type SyncReceipt struct {
	Status int
	Body   string
}

// A durable reservation precedes side effects. A process crash leaves a
// reservation requiring review; it must NEVER automatically replay a create.
func (s *TenantStore) ReserveSyncOperation(ctx context.Context, id, hash string) (*SyncReceipt, error) {
	if s == nil || s.tenantID.String() != tenant.LocalID {
		return nil, errors.New("entry sync requires local tenant scope")
	}
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO entry_sync_operations(id,hash) VALUES (?,?)`, id, hash)
	if err != nil {
		return nil, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 1 {
		return nil, nil
	}
	var stored string
	r := &SyncReceipt{}
	err = s.db.QueryRowContext(ctx, `SELECT hash,status,body FROM entry_sync_operations WHERE id=?`, id).Scan(&stored, &r.Status, &r.Body)
	if err != nil {
		return nil, err
	}
	if stored != hash {
		return nil, fmt.Errorf("operation ID reused for different content")
	}
	return r, nil
}
func (s *TenantStore) CompleteSyncOperation(ctx context.Context, id string, status int, body string) error {
	if s == nil || s.tenantID.String() != tenant.LocalID {
		return errors.New("entry sync requires local tenant scope")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE entry_sync_operations SET status=?,body=? WHERE id=?`, status, body, id)
	return err
}

// ReadSelectedEntries reads only the requested working set in one snapshot.
// An empty selection returns the epoch, never a scan of the library.
func (s *TenantStore) ReadSelectedEntries(ctx context.Context, paths []string) (*EntrySyncPage, error) {
	if s == nil || s.tenantID.String() != tenant.LocalID {
		return nil, errors.New("entry sync requires local tenant scope")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	p := &EntrySyncPage{Rows: []EntrySyncRow{}}
	if err = tx.QueryRowContext(ctx, "SELECT epoch FROM entry_sync_identity WHERE id=1").Scan(&p.Epoch); err != nil {
		return nil, err
	}
	for _, path := range paths {
		n, err := scanNoteRow(tx.QueryRowContext(ctx, "SELECT "+noteColumns+" FROM notes WHERE path=?", path))
		if errors.Is(err, sql.ErrNoRows) {
			n = nil
		} else if err != nil {
			return nil, err
		}
		p.Rows = append(p.Rows, EntrySyncRow{Path: path, Note: n})
	}
	return p, tx.Commit()
}
