package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

const createBulkJobsTable = `CREATE TABLE IF NOT EXISTS bulk_jobs (
 tenant_id TEXT NOT NULL, id TEXT NOT NULL, request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL, request_json TEXT NOT NULL, operation TEXT NOT NULL,
 state TEXT NOT NULL, submitted_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,request_id)
)`
const createBulkJobItemsTable = `CREATE TABLE IF NOT EXISTS bulk_job_items (
 tenant_id TEXT NOT NULL, job_id TEXT NOT NULL, sequence INTEGER NOT NULL,
 path TEXT NOT NULL, entry_id TEXT NOT NULL, title TEXT NOT NULL, fingerprint TEXT NOT NULL,
 state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', destination TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,job_id,sequence), UNIQUE(tenant_id,job_id,path),
 FOREIGN KEY(tenant_id,job_id) REFERENCES bulk_jobs(tenant_id,id) ON DELETE CASCADE
)`

const createBulkJobItemsIndex = `CREATE INDEX IF NOT EXISTS bulk_items_pending ON bulk_job_items(tenant_id,job_id,state,sequence)`

func (s *TenantStore) bulkScope(ctx context.Context) (string, error) {
	if _, _, err := s.listQuery(nil); err != nil {
		return "", err
	}
	id, ok := tenant.From(ctx)
	if !ok || id != s.tenantID {
		return "", fmt.Errorf("bulk job tenant scope mismatch")
	}
	return id.String(), nil
}

func (s *TenantStore) InsertBulkJob(ctx context.Context, job *types.BulkJob, items []types.BulkJobItem) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(job.Request)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO bulk_jobs VALUES(?,?,?,?,?,?,?,?,?,?)`, scope, job.ID, job.RequestID, job.RequestHash, string(raw), job.Operation, job.State, job.SubmittedBy, job.CreatedAt, job.UpdatedAt)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO bulk_job_items(tenant_id,job_id,sequence,path,entry_id,title,fingerprint,state,error) VALUES(?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, item := range items {
		if _, err = stmt.ExecContext(ctx, scope, job.ID, item.Sequence, item.Path, item.EntryID, item.Title, item.Fingerprint, item.State, item.Error); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const bulkJobSelect = `SELECT j.id,j.request_id,j.request_hash,j.request_json,j.operation,j.state,j.submitted_by,j.created_at,j.updated_at,
 COUNT(i.sequence),COALESCE(SUM(i.state='pending'),0),COALESCE(SUM(i.state='running'),0),COALESCE(SUM(i.state='succeeded'),0),COALESCE(SUM(i.state='failed'),0),COALESCE(SUM(i.state='uncertain'),0),COALESCE(SUM(i.state='skipped'),0)
 FROM bulk_jobs j LEFT JOIN bulk_job_items i ON i.tenant_id=j.tenant_id AND i.job_id=j.id WHERE j.tenant_id=?`

func (s *TenantStore) readBulkJobs(ctx context.Context, clause string, args ...interface{}) ([]types.BulkJob, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, err
	}
	args = append([]interface{}{scope}, args...)
	rows, err := s.db.QueryContext(ctx, bulkJobSelect+clause, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	jobs := []types.BulkJob{}
	for rows.Next() {
		var j types.BulkJob
		var raw string
		if err = rows.Scan(&j.ID, &j.RequestID, &j.RequestHash, &raw, &j.Operation, &j.State, &j.SubmittedBy, &j.CreatedAt, &j.UpdatedAt, &j.Total, &j.Pending, &j.Running, &j.Succeeded, &j.Failed, &j.Uncertain, &j.Skipped); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &j.Request); err != nil {
			return nil, err
		}
		j.Label = j.Request.Label
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}
func (s *TenantStore) ListBulkJobs(ctx context.Context) ([]types.BulkJob, error) {
	return s.readBulkJobs(ctx, ` AND j.id IN (SELECT id FROM bulk_jobs WHERE tenant_id=? ORDER BY CASE WHEN state IN ('queued','running','paused') THEN 0 ELSE 1 END,created_at DESC LIMIT 100) GROUP BY j.id ORDER BY CASE WHEN j.state IN ('queued','running','paused') THEN 0 ELSE 1 END,j.created_at DESC`, s.tenantID.String())
}
func (s *TenantStore) GetBulkJob(ctx context.Context, id string) (*types.BulkJob, error) {
	jobs, err := s.readBulkJobs(ctx, ` AND j.id=? GROUP BY j.id`, id)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}
func (s *TenantStore) BulkJobByRequest(ctx context.Context, id string) (*types.BulkJob, error) {
	jobs, err := s.readBulkJobs(ctx, ` AND j.request_id=? GROUP BY j.id`, id)
	if err != nil {
		return nil, err
	}
	if len(jobs) == 0 {
		return nil, nil
	}
	return &jobs[0], nil
}
func (s *TenantStore) BulkJobItems(ctx context.Context, id string, offset, limit int) ([]types.BulkJobItem, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT sequence,path,entry_id,title,fingerprint,state,attempts,error,destination FROM bulk_job_items WHERE tenant_id=? AND job_id=? ORDER BY CASE WHEN state='uncertain' THEN 0 WHEN state='failed' THEN 1 ELSE 2 END,sequence LIMIT ? OFFSET ?`, scope, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []types.BulkJobItem{}
	for rows.Next() {
		var i types.BulkJobItem
		if err = rows.Scan(&i.Sequence, &i.Path, &i.EntryID, &i.Title, &i.Fingerprint, &i.State, &i.Attempts, &i.Error, &i.Destination); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// Only pending work is claimable. Restart recovery NEVER replays an in-flight mutation.
func (s *TenantStore) ClaimBulkJobItem(ctx context.Context, id string) (*types.BulkJobItem, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, err
	}
	var i types.BulkJobItem
	err = s.db.QueryRowContext(ctx, `UPDATE bulk_job_items SET state='running',attempts=attempts+1 WHERE tenant_id=? AND job_id=? AND sequence=(SELECT sequence FROM bulk_job_items WHERE tenant_id=? AND job_id=? AND state='pending' ORDER BY sequence LIMIT 1) AND EXISTS(SELECT 1 FROM bulk_jobs WHERE tenant_id=? AND id=? AND state='running') RETURNING sequence,path,entry_id,title,fingerprint,state,attempts,error,destination`, scope, id, scope, id, scope, id).Scan(&i.Sequence, &i.Path, &i.EntryID, &i.Title, &i.Fingerprint, &i.State, &i.Attempts, &i.Error, &i.Destination)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &i, err
}
func (s *TenantStore) FinishBulkJobItem(ctx context.Context, id string, i types.BulkJobItem) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE bulk_job_items SET state=?,error=?,destination=? WHERE tenant_id=? AND job_id=? AND sequence=? AND state='running'`, i.State, i.Error, i.Destination, scope, id, i.Sequence)
	return err
}
func (s *TenantStore) TransitionBulkJob(ctx context.Context, id, from, to string) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE bulk_jobs SET state=?,updated_at=? WHERE tenant_id=? AND id=? AND state=?`, to, time.Now().UTC().Format(time.RFC3339Nano), scope, id, from)
	return err
}
func (s *TenantStore) RetryBulkJob(ctx context.Context, id string) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE bulk_job_items SET state='pending',error='' WHERE tenant_id=? AND job_id=? AND state='failed' AND EXISTS(SELECT 1 FROM bulk_jobs WHERE tenant_id=? AND id=? AND state='needs_attention')`, scope, id, scope, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bulk_jobs SET state='queued' WHERE tenant_id=? AND id=? AND state='needs_attention'`, scope, id); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *TenantStore) RecoverBulkJobs(ctx context.Context) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE bulk_job_items SET state='uncertain',error='Server stopped during this entry. Inspect its current state before taking further action; it was not replayed.' WHERE tenant_id=? AND state='running'`, scope); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE bulk_jobs SET state='queued' WHERE tenant_id=? AND state='running'`, scope); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *TenantStore) NextRunnableBulkJob(ctx context.Context) (*types.BulkJob, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, err
	}
	var j types.BulkJob
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT id,request_json,operation,state FROM bulk_jobs WHERE tenant_id=? AND state IN ('queued','running') ORDER BY created_at,id LIMIT 1`, scope).Scan(&j.ID, &raw, &j.Operation, &j.State)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &j.Request); err != nil {
		return nil, err
	}
	return &j, nil
}

// Quarantine any unacknowledged write before declaring a job finished. This
// also handles an outcome-commit failure without requiring a server restart.
func (s *TenantStore) QuarantineBulkJobItems(ctx context.Context, id string) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE bulk_job_items SET state='uncertain',error='Write outcome was not persisted. Inspect this entry before taking further action; it was not replayed.' WHERE tenant_id=? AND job_id=? AND state='running'`, scope, id)
	return err
}
