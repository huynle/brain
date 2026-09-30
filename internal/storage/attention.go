package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

// attention_items is a per-user, tenant-scoped notification inbox. Every query
// carries tenant_id AND recipient so one user never sees or mutates another's
// items. dedup_key is unique per (tenant, recipient) so repeated producers are
// idempotent. revision guards state transitions against concurrent writers.
const createAttentionItemsTable = `CREATE TABLE IF NOT EXISTS attention_items (
 tenant_id TEXT NOT NULL, id TEXT NOT NULL, recipient TEXT NOT NULL,
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
 resolved_at TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL,
 PRIMARY KEY(tenant_id,id)
)`

const createAttentionRecipientIndex = `CREATE INDEX IF NOT EXISTS idx_attention_recipient ON attention_items(tenant_id,recipient,state,created_at)`

// A partial unique index makes (tenant, recipient, dedup_key) idempotent while
// leaving items with no dedup key unconstrained.
const createAttentionDedupIndex = `CREATE UNIQUE INDEX IF NOT EXISTS idx_attention_dedup ON attention_items(tenant_id,recipient,dedup_key) WHERE dedup_key <> ''`

// attentionScope validates the tenant binding exactly like bulkScope: empty or
// mismatched tenant context is an error, never an omitted predicate.
func (s *TenantStore) attentionScope(ctx context.Context) (string, error) {
	if _, _, err := s.listQuery(nil); err != nil {
		return "", err
	}
	id, ok := tenant.From(ctx)
	if !ok || id != s.tenantID {
		return "", fmt.Errorf("attention tenant scope mismatch")
	}
	return id.String(), nil
}

// CreateAttention inserts an item. When dedup_key is set and an item already
// exists for (tenant, recipient, dedup_key), it is a no-op returning false.
func (s *TenantStore) CreateAttention(ctx context.Context, a *types.Attention, now time.Time) (bool, error) {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return false, err
	}
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
	// ON CONFLICT on the dedup index makes concurrent producers idempotent.
	// A conflict returns 0 rows affected; that is "already present", not error.
	q := `INSERT INTO attention_items
 (tenant_id,id,recipient,kind,severity,title,body,project,task_id,feature_id,session_id,runner_id,instance_id,source_type,source_id,dedup_key,state,actions_json,created_at,updated_at,read_at,snoozed_until,resolved_at,revision)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'','','',1)`
	if a.DedupKey != "" {
		q += ` ON CONFLICT(tenant_id,recipient,dedup_key) WHERE dedup_key <> '' DO NOTHING`
	}
	res, err := s.db.ExecContext(ctx, q,
		scope, a.ID, a.Recipient, a.Kind, a.Severity, a.Title, a.Body,
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
func (s *TenantStore) GetAttention(ctx context.Context, recipient, id string) (*types.Attention, error) {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, attentionSelect+` WHERE tenant_id=? AND recipient=? AND id=?`, scope, recipient, id)
	a, err := scanAttention(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

// ListAttention returns a recipient's items, newest first, filtered. Snoozed
// items are hidden unless explicitly requested or explicitly filtered for.
func (s *TenantStore) ListAttention(ctx context.Context, f types.AttentionListFilter) ([]types.Attention, error) {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return nil, err
	}
	if f.Recipient == "" {
		return nil, fmt.Errorf("attention list requires a recipient")
	}
	var sb strings.Builder
	sb.WriteString(attentionSelect + ` WHERE tenant_id=? AND recipient=?`)
	args := []interface{}{scope, f.Recipient}
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
func (s *TenantStore) TransitionAttention(ctx context.Context, recipient, id, state, snoozedUntil string, expectedRevision int, now time.Time) (bool, error) {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return false, err
	}
	if !types.ValidAttentionState(state) {
		return false, fmt.Errorf("invalid attention state %q", state)
	}
	ts := now.UTC().Format(time.RFC3339)
	// Timestamp columns are set on the transitions that own them; other
	// transitions leave them untouched via COALESCE-style CASE.
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
 WHERE tenant_id=? AND recipient=? AND id=? AND revision=?`,
		state, ts, readAt, readAt, resolvedAt, resolvedAt, state, snooze,
		scope, recipient, id, expectedRevision)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// AttentionCounts summarises a recipient's inbox for the bell badge. Unread
// counts only unread items; critical counts unread critical items.
func (s *TenantStore) AttentionCounts(ctx context.Context, recipient string) (types.AttentionCounts, error) {
	scope, err := s.attentionScope(ctx)
	if err != nil {
		return types.AttentionCounts{}, err
	}
	var c types.AttentionCounts
	err = s.db.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(CASE WHEN state='unread' THEN 1 ELSE 0 END),0),
 COALESCE(SUM(CASE WHEN state='unread' AND severity='critical' THEN 1 ELSE 0 END),0),
 COUNT(*)
 FROM attention_items WHERE tenant_id=? AND recipient=?`, scope, recipient).
		Scan(&c.Unread, &c.Critical, &c.Total)
	return c, err
}

const attentionSelect = `SELECT id,recipient,kind,severity,title,body,project,task_id,feature_id,session_id,runner_id,instance_id,source_type,source_id,dedup_key,state,actions_json,created_at,updated_at,read_at,snoozed_until,resolved_at,revision FROM attention_items`

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
