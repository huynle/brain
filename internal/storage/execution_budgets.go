package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	_ "time/tzdata"

	"github.com/huynle/brain-api/internal/types"
)

const createExecutionBudgets = `CREATE TABLE IF NOT EXISTS execution_budgets (tenant_id TEXT NOT NULL,project TEXT NOT NULL,id TEXT NOT NULL,timezone TEXT NOT NULL,unit TEXT NOT NULL,limit_units INTEGER NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(tenant_id,project,id))`
const createBudgetReservations = `CREATE TABLE IF NOT EXISTS budget_reservations (tenant_id TEXT NOT NULL,project TEXT NOT NULL,budget_id TEXT NOT NULL,id TEXT NOT NULL,parent_id TEXT NOT NULL,window TEXT NOT NULL,units INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(tenant_id,project,budget_id,id),FOREIGN KEY(tenant_id,project,budget_id) REFERENCES execution_budgets(tenant_id,project,id))`

func (s *TenantStore) ConfigureExecutionBudget(ctx context.Context, b types.ExecutionBudget, expected int) (bool, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return false, err
	}
	if _, err = time.LoadLocation(b.Timezone); err != nil {
		return false, err
	}
	if b.Limit < 0 || b.Limit > 1e9 || b.ID == "" || b.Project == "" || b.Unit == "" {
		return false, fmt.Errorf("invalid budget")
	}
	// Timezone/unit are immutable; changing them must not reset consumed work.
	var result sql.Result
	if expected == 0 {
		result, err = s.db.ExecContext(ctx, `INSERT INTO execution_budgets VALUES(?,?,?,?,?,?,1) ON CONFLICT DO NOTHING`, scope, b.Project, b.ID, b.Timezone, b.Unit, b.Limit)
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE execution_budgets SET limit_units=?,revision=revision+1 WHERE tenant_id=? AND project=? AND id=? AND revision=? AND timezone=? AND unit=?`, b.Limit, scope, b.Project, b.ID, expected, b.Timezone, b.Unit)
	}
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (s *TenantStore) ExecutionBudget(ctx context.Context, project, id string, now time.Time) (*types.ExecutionBudget, int64, string, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, 0, "", err
	}
	var b types.ExecutionBudget
	err = s.db.QueryRowContext(ctx, `SELECT id,project,timezone,unit,limit_units,revision FROM execution_budgets WHERE tenant_id=? AND project=? AND id=?`, scope, project, id).Scan(&b.ID, &b.Project, &b.Timezone, &b.Unit, &b.Limit, &b.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, "", nil
	}
	if err != nil {
		return nil, 0, "", err
	}
	zone, err := time.LoadLocation(b.Timezone)
	if err != nil {
		return nil, 0, "", err
	}
	window := now.In(zone).Format("2006-01-02")
	var used int64
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(units),0) FROM budget_reservations WHERE tenant_id=? AND project=? AND budget_id=? AND window=? AND state IN ('reserved','committed')`, scope, project, id, window).Scan(&used)
	return &b, used, window, err
}
func (s *TenantStore) ReserveBudget(ctx context.Context, project, budgetID, id, parent string, units int64, now time.Time) (*types.BudgetReservation, bool, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, false, err
	}
	if units < 1 || units > 1e9 || id == "" {
		return nil, false, fmt.Errorf("units must be 1..1000000000 and reservation ID is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback() //nolint:errcheck
	// Acquire the writer reservation before reading consumption.
	result, err := tx.ExecContext(ctx, `UPDATE execution_budgets SET revision=revision WHERE tenant_id=? AND project=? AND id=?`, scope, project, budgetID)
	if err != nil {
		return nil, false, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil, false, fmt.Errorf("budget not found")
	}
	var prior types.BudgetReservation
	err = tx.QueryRowContext(ctx, `SELECT id,budget_id,parent_id,window,units,state FROM budget_reservations WHERE tenant_id=? AND project=? AND budget_id=? AND id=?`, scope, project, budgetID, id).Scan(&prior.ID, &prior.BudgetID, &prior.ParentID, &prior.Window, &prior.Units, &prior.State)
	if err == nil {
		if prior.Units != units || prior.ParentID != parent {
			return nil, false, fmt.Errorf("reservation ID belongs to different work")
		}
		return &prior, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	if parent != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM budget_reservations WHERE tenant_id=? AND project=? AND budget_id=? AND id=?`, scope, project, budgetID, parent).Scan(&count); err != nil {
			return nil, false, err
		}
		if count != 1 {
			return nil, false, fmt.Errorf("parent reservation not found in this budget")
		}
	}
	var timezone string
	var limit int64
	if err = tx.QueryRowContext(ctx, `SELECT timezone,limit_units FROM execution_budgets WHERE tenant_id=? AND project=? AND id=?`, scope, project, budgetID).Scan(&timezone, &limit); err != nil {
		return nil, false, err
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, false, err
	}
	window := now.In(zone).Format("2006-01-02")
	var used int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(units),0) FROM budget_reservations WHERE tenant_id=? AND project=? AND budget_id=? AND window=? AND state IN ('reserved','committed')`, scope, project, budgetID, window).Scan(&used); err != nil {
		return nil, false, err
	}
	if used+units > limit {
		return nil, false, fmt.Errorf("budget exhausted: %d consumed/reserved, %d requested, %d limit", used, units, limit)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO budget_reservations VALUES(?,?,?,?,?,?,?,'reserved')`, scope, project, budgetID, id, parent, window, units)
	if err != nil {
		return nil, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, false, err
	}
	return &types.BudgetReservation{ID: id, BudgetID: budgetID, ParentID: parent, Window: window, Units: units, State: "reserved"}, true, nil
}

// Settlement never erases usage after work has been committed. Interrupted work
// stays charged; cancellation is allowed only while the reservation is unused.
func (s *TenantStore) SettleBudget(ctx context.Context, project, budgetID, id, state string) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	if state != "committed" && state != "cancelled" {
		return fmt.Errorf("state must be committed or cancelled")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE budget_reservations SET state=? WHERE tenant_id=? AND project=? AND budget_id=? AND id=? AND state='reserved'`, state, scope, project, budgetID, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	var current string
	if err := s.db.QueryRowContext(ctx, `SELECT state FROM budget_reservations WHERE tenant_id=? AND project=? AND budget_id=? AND id=?`, scope, project, budgetID, id).Scan(&current); err != nil {
		return err
	}
	if current != state {
		return fmt.Errorf("reservation is already %s", current)
	}
	return nil
}
