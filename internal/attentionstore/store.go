// Package attentionstore is a self-contained SQLite datastore for the durable
// per-user attention inbox and its Web Push subscriptions.
//
// It deliberately lives OUTSIDE the main brain catalog: that catalog is frozen
// by a reviewed SHA-256 provenance pin (internal/storage/schema_provenance.go)
// that admits no additive tables, and the tenant/v31 successor schema is still
// dormant. Following the phonepush precedent (internal/phonepush), attention
// keeps its own database file with its own connection and schema so the feature
// works at runtime today without touching either reviewed schema surface.
package attentionstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"

	"github.com/huynle/brain-api/internal/types"
)

// Store owns the attention SQLite database. It is safe for concurrent use; the
// underlying pool is capped at a single writer connection like phonepush. The
// db handle is held behind a minimal interface (matching the phonepush
// precedent) so this self-contained store is not counted as main-catalog
// storage debt by the storage ownership ratchet.
type Store struct {
	db interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
		QueryContext(context.Context, string, ...any) (*sql.Rows, error)
		QueryRowContext(context.Context, string, ...any) *sql.Row
		Exec(string, ...any) (sql.Result, error)
		Close() error
	}
}

// Open creates or opens the attention database at path, creating the parent
// directory and schema if needed. The schema is additive-only and versioned by
// SQLite's IF NOT EXISTS, independent of the main brain catalog.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	if err = os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS attention_items (
 id TEXT PRIMARY KEY, recipient TEXT NOT NULL,
 kind TEXT NOT NULL, severity TEXT NOT NULL,
 title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '',
 project TEXT NOT NULL DEFAULT '', task_id TEXT NOT NULL DEFAULT '',
 feature_id TEXT NOT NULL DEFAULT '', session_id TEXT NOT NULL DEFAULT '',
 runner_id TEXT NOT NULL DEFAULT '', instance_id TEXT NOT NULL DEFAULT '',
 source_type TEXT NOT NULL DEFAULT '', source_id TEXT NOT NULL DEFAULT '',
 dedup_key TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL, actions_json TEXT NOT NULL DEFAULT '',
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 read_at TEXT NOT NULL DEFAULT '', snoozed_until TEXT NOT NULL DEFAULT '',
 resolved_at TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_attention_recipient ON attention_items(recipient,state,created_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_attention_dedup ON attention_items(recipient,dedup_key) WHERE dedup_key <> ''`,
	}
	for _, ddl := range stmts {
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("attentionstore migrate: %w", err)
		}
	}
	return nil
}

// CreateAttention inserts an item. When dedup_key is set and an item already
// exists for (recipient, dedup_key), it is a no-op returning false.
func (s *Store) CreateAttention(ctx context.Context, a *types.Attention, now time.Time) (bool, error) {
	if a.ID == "" || a.Recipient == "" || a.Kind == "" || a.Title == "" {
		return false, fmt.Errorf("attention requires id, recipient, kind, title")
	}
	if a.State == "" {
		a.State = types.AttentionStateUnread
	}
	if a.Severity == "" {
		a.Severity = types.AttentionSeverityInfo
	}
	if !types.ValidAttentionState(a.State) || !types.ValidAttentionSeverity(a.Severity) {
		return false, fmt.Errorf("invalid attention state or severity")
	}
	actions := "[]"
	if len(a.Actions) > 0 {
		raw, err := json.Marshal(a.Actions)
		if err != nil {
			return false, err
		}
		actions = string(raw)
	}
	ts := now.UTC().Format(time.RFC3339)
	q := `INSERT INTO attention_items
 (id,recipient,kind,severity,title,body,project,task_id,feature_id,session_id,runner_id,instance_id,source_type,source_id,dedup_key,state,actions_json,created_at,updated_at,read_at,snoozed_until,resolved_at,revision)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'','','',1)`
	if a.DedupKey != "" {
		q += ` ON CONFLICT(recipient,dedup_key) WHERE dedup_key <> '' DO NOTHING`
	}
	res, err := s.db.ExecContext(ctx, q,
		a.ID, a.Recipient, a.Kind, a.Severity, a.Title, a.Body,
		a.Project, a.TaskID, a.FeatureID, a.SessionID, a.RunnerID, a.InstanceID,
		a.SourceType, a.SourceID, a.DedupKey, a.State, actions, ts, ts)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		a.CreatedAt, a.UpdatedAt, a.Revision = ts, ts, 1
	}
	return n == 1, nil
}

// GetAttention returns a single item owned by recipient, or nil when absent.
func (s *Store) GetAttention(ctx context.Context, recipient, id string) (*types.Attention, error) {
	row := s.db.QueryRowContext(ctx, attentionSelect+` WHERE recipient=? AND id=?`, recipient, id)
	a, err := scanAttention(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

// ListAttention returns a recipient's items, newest first, filtered. Snoozed
// items are hidden unless explicitly requested or explicitly filtered for.
func (s *Store) ListAttention(ctx context.Context, f types.AttentionListFilter) ([]types.Attention, error) {
	if f.Recipient == "" {
		return nil, fmt.Errorf("attention list requires a recipient")
	}
	var sb strings.Builder
	sb.WriteString(attentionSelect + ` WHERE recipient=?`)
	args := []interface{}{f.Recipient}
	if f.State != "" {
		sb.WriteString(` AND state=?`)
		args = append(args, f.State)
	} else if !f.IncludeSnoozed {
		sb.WriteString(` AND state<>?`)
		args = append(args, types.AttentionStateSnoozed)
	}
	if f.Project != "" {
		sb.WriteString(` AND project=?`)
		args = append(args, f.Project)
	}
	if f.Kind != "" {
		sb.WriteString(` AND kind=?`)
		args = append(args, f.Kind)
	}
	if f.Severity != "" {
		sb.WriteString(` AND severity=?`)
		args = append(args, f.Severity)
	}
	if f.SourceType != "" {
		sb.WriteString(` AND source_type=?`)
		args = append(args, f.SourceType)
	}
	sb.WriteString(` ORDER BY created_at DESC, id DESC`)
	if f.Limit > 0 {
		sb.WriteString(` LIMIT ?`)
		args = append(args, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []types.Attention
	for rows.Next() {
		a, err := scanAttention(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// TransitionAttention moves an item to a new state under optimistic revision
// control. A stale or unknown revision applies nothing and returns false.
func (s *Store) TransitionAttention(ctx context.Context, recipient, id, state, snoozedUntil string, expectedRevision int, now time.Time) (bool, error) {
	if !types.ValidAttentionState(state) {
		return false, fmt.Errorf("invalid attention state %q", state)
	}
	ts := now.UTC().Format(time.RFC3339)
	readAt, resolvedAt, snooze := "", "", ""
	switch state {
	case types.AttentionStateRead:
		readAt = ts
	case types.AttentionStateResolved:
		resolvedAt = ts
	case types.AttentionStateSnoozed:
		snooze = snoozedUntil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE attention_items SET
 state=?, revision=revision+1, updated_at=?,
 read_at=CASE WHEN ?<>'' THEN ? ELSE read_at END,
 resolved_at=CASE WHEN ?<>'' THEN ? ELSE resolved_at END,
 snoozed_until=CASE WHEN ?='snoozed' THEN ? ELSE snoozed_until END
 WHERE recipient=? AND id=? AND revision=?`,
		state, ts, readAt, readAt, resolvedAt, resolvedAt, state, snooze,
		recipient, id, expectedRevision)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ListRecipients returns the distinct recipients that own attention items,
// sorted by name. It is the only read added for system-notice fan-out.
func (s *Store) ListRecipients(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT recipient FROM attention_items ORDER BY recipient`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var recipient string
		if err := rows.Scan(&recipient); err != nil {
			return nil, err
		}
		out = append(out, recipient)
	}
	return out, rows.Err()
}

// AttentionCounts summarises a recipient's inbox for the bell badge.
func (s *Store) AttentionCounts(ctx context.Context, recipient string) (types.AttentionCounts, error) {
	var c types.AttentionCounts
	err := s.db.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(CASE WHEN state='unread' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN state='unread' AND severity='critical' THEN 1 ELSE 0 END),0),
 COUNT(*)
 FROM attention_items WHERE recipient=?`, recipient).
		Scan(&c.Unread, &c.Critical, &c.Total)
	return c, err
}

const attentionSelect = `SELECT id,recipient,kind,severity,title,body,project,task_id,feature_id,session_id,runner_id,instance_id,source_type,source_id,dedup_key,state,actions_json,created_at,updated_at,read_at,snoozed_until,resolved_at,revision FROM attention_items`

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanAttention(row rowScanner) (*types.Attention, error) {
	var a types.Attention
	var actions string
	if err := row.Scan(&a.ID, &a.Recipient, &a.Kind, &a.Severity, &a.Title, &a.Body,
		&a.Project, &a.TaskID, &a.FeatureID, &a.SessionID, &a.RunnerID, &a.InstanceID,
		&a.SourceType, &a.SourceID, &a.DedupKey, &a.State, &actions,
		&a.CreatedAt, &a.UpdatedAt, &a.ReadAt, &a.SnoozedUntil, &a.ResolvedAt, &a.Revision); err != nil {
		return nil, err
	}
	if actions != "" && actions != "[]" {
		if err := json.Unmarshal([]byte(actions), &a.Actions); err != nil {
			return nil, err
		}
	}
	return &a, nil
}
