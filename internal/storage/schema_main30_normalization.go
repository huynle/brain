package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"
)

const reviewedVecPreflightHex = "CDCCCC3DCDCC4C3E9A99993ECDCCCC3E0000003F9A99193F3333333FCDCC4C3F"

func normalizeHistoricalMain30(ctx context.Context, db *sql.DB) (err error) {
	if ctx == nil || db == nil {
		return fmt.Errorf("historical main30 normalization requires context and database")
	}
	profile, err := schemaProfile(ctx, db)
	if err != nil {
		return err
	}
	if profile != "main30-historical-preflight" {
		return nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, restore := conn.ExecContext(cleanup, "PRAGMA foreign_keys=ON")
		var fk int
		if restore == nil {
			restore = conn.QueryRowContext(cleanup, "PRAGMA foreign_keys").Scan(&fk)
			if restore == nil && fk != 1 {
				restore = fmt.Errorf("foreign keys not restored")
			}
		}
		if restore != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		err = errors.Join(err, restore, conn.Close())
	}()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	var fk int
	if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 0 {
		return errors.Join(err, fmt.Errorf("foreign keys must be disabled before normalization"))
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "UPDATE schema_version SET version=version WHERE 0"); err != nil {
		return err
	}
	if profile, err = classifySchemaSource(ctx, tx); err != nil || profile != "main30-historical-preflight" {
		return fmt.Errorf("historical source changed before normalization: %q: %w", profile, err)
	}
	if err = validateHistoricalMain30Rows(ctx, tx); err != nil {
		return err
	}

	statements := []string{
		"ALTER TABLE api_tokens RENAME TO api_tokens_historical",
		createAPITokensTable,
		"INSERT INTO api_tokens(name,token,scope,created_at,last_used,revoked_at) SELECT name,token,scope,created_at,last_used,revoked_at FROM api_tokens_historical",
		"DROP TABLE api_tokens_historical",
		"ALTER TABLE opencode_instances RENAME TO opencode_instances_historical",
		createOpencodeInstancesTable,
		"INSERT INTO opencode_instances(instance_id,runner_id,hostname,kind,project_id,task_id,feature_id,priority,title,workdir,port,pid,session_ids,status,executor,agent,model,started_at,last_seen) SELECT instance_id,runner_id,hostname,kind,project_id,task_id,feature_id,priority,title,workdir,port,pid,session_ids,status,executor,agent,model,started_at,last_seen FROM opencode_instances_historical",
		"DROP TABLE opencode_instances_historical",
		"ALTER TABLE runners RENAME TO runners_historical",
		createRunnersTable,
		"INSERT INTO runners(runner_id,machine_id,hostname,labels,executors,capabilities,dispatch_push,workspace_roots,projects,resources,capacity,draining,max_parallel,feature_ids,registered_at,last_heartbeat,status) SELECT runner_id,machine_id,hostname,labels,executors,capabilities,dispatch_push,workspace_roots,projects,resources,capacity,draining,max_parallel,feature_ids,CAST(registered_at AS INTEGER),CAST(last_heartbeat AS INTEGER),status FROM runners_historical",
		"DROP TABLE runners_historical",
		"DROP TABLE vec_preflight",
		"CREATE INDEX idx_runners_status ON runners(status)",
		"CREATE INDEX idx_opencode_instances_runner ON opencode_instances(runner_id)",
		"CREATE INDEX idx_opencode_instances_task ON opencode_instances(project_id, task_id)",
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("normalize historical main30: %w", err)
		}
	}
	if profile, err = classifySchemaSource(ctx, tx); err != nil || profile != "main30-devices" {
		return fmt.Errorf("normalized catalog is not canonical main30: %q: %w", profile, err)
	}
	var integrity string
	if err := tx.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return fmt.Errorf("normalized integrity check: %q: %w", integrity, err)
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	hasFKError := rows.Next()
	iterationErr := rows.Err()
	_ = rows.Close()
	if iterationErr != nil {
		return iterationErr
	}
	if hasFKError {
		return fmt.Errorf("normalized foreign key check failed")
	}
	return tx.Commit()
}

func validateHistoricalMain30Rows(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) error {
	var count int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM vec_preflight WHERE id=1 AND hex(embedding)=?", reviewedVecPreflightHex).Scan(&count); err != nil || count != 1 {
		return errors.Join(err, fmt.Errorf("unreviewed vec_preflight contents"))
	}
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM vec_preflight").Scan(&count); err != nil || count != 1 {
		return errors.Join(err, fmt.Errorf("unreviewed vec_preflight row count"))
	}
	const numericText = "registered_at NOT GLOB '*[^0-9]*' AND registered_at != '' AND last_heartbeat NOT GLOB '*[^0-9]*' AND last_heartbeat != ''"
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM runners WHERE active_tasks != 0 OR version != '' OR NOT ("+numericText+")").Scan(&count); err != nil || count != 0 {
		return errors.Join(err, fmt.Errorf("historical runners contain non-inert or nonnumeric fields"))
	}
	return nil
}

func schemaProfile(ctx context.Context, db *sql.DB) (string, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	return classifySchemaSource(ctx, tx)
}
