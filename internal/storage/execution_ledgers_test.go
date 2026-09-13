package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

// Interface assertion keeps RED executable before the imported receivers exist.
// These are real TenantStore calls, never a mock or alternate implementation.
type executionLedgerAPI interface {
	InsertBulkJob(context.Context, *types.BulkJob, []types.BulkJobItem) error
	ListBulkJobs(context.Context) ([]types.BulkJob, error)
	GetBulkJob(context.Context, string) (*types.BulkJob, error)
	BulkJobByRequest(context.Context, string) (*types.BulkJob, error)
	BulkJobItems(context.Context, string, int, int) ([]types.BulkJobItem, error)
	ClaimBulkJobItem(context.Context, string) (*types.BulkJobItem, error)
	FinishBulkJobItem(context.Context, string, types.BulkJobItem) error
	TransitionBulkJob(context.Context, string, string, string) error
	RetryBulkJob(context.Context, string) error
	RecoverBulkJobs(context.Context) error
	NextRunnableBulkJob(context.Context) (*types.BulkJob, error)
	QuarantineBulkJobItems(context.Context, string) error
	ConfigureExecutionBudget(context.Context, types.ExecutionBudget, int) (bool, error)
	ExecutionBudget(context.Context, string, string, time.Time) (*types.ExecutionBudget, int64, string, error)
	ReserveBudget(context.Context, string, string, string, string, int64, time.Time) (*types.BudgetReservation, bool, error)
	SettleBudget(context.Context, string, string, string, string) error
	SupervisorCheckpoints(context.Context, string, string) ([]types.SupervisorCheckpoint, error)
	SupervisorCheckpoint(context.Context, string, string) (*types.SupervisorCheckpoint, error)
	CompareSupervisorCheckpoint(context.Context, int, *types.SupervisorCheckpoint) (bool, error)
	SupervisorCheckpointVersions(context.Context, string, string) ([]types.SupervisorCheckpoint, error)
	BeginSupervisorOperation(context.Context, string, string, string, string) (bool, error)
	SupervisorOperation(context.Context, string, string) (*types.SupervisorOperation, string, error)
	FinishSupervisorOperation(context.Context, string, string, string, string) error
}

func ledgerAPI(t *testing.T, db *sql.DB, id tenant.ID) executionLedgerAPI {
	t.Helper()
	s := &TenantStore{db: db, tenantID: id}
	api, ok := any(s).(executionLedgerAPI)
	if !ok {
		t.Fatal("TenantStore is missing the seven-ledger behavioral surface")
	}
	return api
}

// Independent explicit partial staging fixture. NO published version, notes,
// roots, credentials or FTS. This is NOT a valid full successor and must never
// be admitted by New/InitSchema or the complete provenance classifier.
var ledgerFixtureDDL = []string{
	`CREATE TABLE schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT DEFAULT (datetime('now'))
)`,
	`CREATE TABLE tenants(id TEXT PRIMARY KEY NOT NULL CHECK(length(id)>0),name TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('active','suspended','deleting','deleted')),created_at TEXT NOT NULL,root_override TEXT)`,
	`CREATE TABLE bulk_jobs (
 tenant_id TEXT NOT NULL REFERENCES tenants(id), id TEXT NOT NULL, request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL, request_json TEXT NOT NULL, operation TEXT NOT NULL,
 state TEXT NOT NULL, submitted_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,request_id)
)`,
	`CREATE TABLE bulk_job_items (
 tenant_id TEXT NOT NULL REFERENCES tenants(id), job_id TEXT NOT NULL, sequence INTEGER NOT NULL,
 path TEXT NOT NULL, entry_id TEXT NOT NULL, title TEXT NOT NULL, fingerprint TEXT NOT NULL,
 state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', destination TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,job_id,sequence), UNIQUE(tenant_id,job_id,path),
 FOREIGN KEY(tenant_id,job_id) REFERENCES bulk_jobs(tenant_id,id) ON DELETE CASCADE
)`,
	`CREATE INDEX bulk_items_pending ON bulk_job_items(tenant_id,job_id,state,sequence)`,
	`CREATE TABLE execution_budgets (tenant_id TEXT NOT NULL REFERENCES tenants(id),project TEXT NOT NULL,id TEXT NOT NULL,timezone TEXT NOT NULL,unit TEXT NOT NULL,limit_units INTEGER NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(tenant_id,project,id))`,
	`CREATE TABLE budget_reservations (tenant_id TEXT NOT NULL REFERENCES tenants(id),project TEXT NOT NULL,budget_id TEXT NOT NULL,id TEXT NOT NULL,parent_id TEXT NOT NULL,window TEXT NOT NULL,units INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(tenant_id,project,budget_id,id),FOREIGN KEY(tenant_id,project,budget_id) REFERENCES execution_budgets(tenant_id,project,id))`,
	`CREATE TABLE supervisor_checkpoints (tenant_id TEXT NOT NULL REFERENCES tenants(id), project TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(tenant_id,project,id))`,
	`CREATE TABLE supervisor_checkpoint_versions (tenant_id TEXT NOT NULL REFERENCES tenants(id),project TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(tenant_id,project,id,revision),FOREIGN KEY(tenant_id,project,id) REFERENCES supervisor_checkpoints(tenant_id,project,id))`,
	`CREATE TABLE supervisor_operations (
 tenant_id TEXT NOT NULL REFERENCES tenants(id), actor TEXT NOT NULL, id TEXT NOT NULL, operation TEXT NOT NULL,
 digest TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,actor,id))`,
}

var ledgerTables = []string{"bulk_jobs", "bulk_job_items", "execution_budgets", "budget_reservations", "supervisor_checkpoints", "supervisor_checkpoint_versions", "supervisor_operations"}

func ledgerFixture(t *testing.T) (*sql.DB, executionLedgerAPI, executionLedgerAPI, context.Context, context.Context) {
	t.Helper()
	db := compatibilityDB(t, filepath.Join(t.TempDir(), "partial.db"))
	db.SetMaxOpenConns(1)
	_, err := db.Exec("PRAGMA foreign_keys=ON")
	collisionMust(t, err)
	for _, ddl := range ledgerFixtureDDL {
		_, err = db.Exec(ddl)
		collisionMust(t, err)
	}
	a, err := tenant.Parse("a")
	collisionMust(t, err)
	b, err := tenant.Parse("b")
	collisionMust(t, err)
	_, err = db.Exec(`INSERT INTO tenants VALUES('a','A','active','now',NULL),('b','B','active','now',NULL)`)
	collisionMust(t, err)
	return db, ledgerAPI(t, db, a), ledgerAPI(t, db, b), tenant.Into(context.Background(), a), tenant.Into(context.Background(), b)
}

// SQL oracle reads every column, independent of the receivers under test.
func ledgerSnapshot(t *testing.T, db *sql.DB, owner string) string {
	t.Helper()
	var snapshot string
	for _, table := range ledgerTables {
		rows, err := db.Query("SELECT * FROM "+table+" WHERE tenant_id=? ORDER BY 1,2,3,4", owner)
		collisionMust(t, err)
		cols, err := rows.Columns()
		collisionMust(t, err)
		snapshot += table + ":"
		for rows.Next() {
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			collisionMust(t, rows.Scan(dest...))
			snapshot += fmt.Sprintf("%#v", values)
		}
		collisionMust(t, rows.Err())
		collisionMust(t, rows.Close())
	}
	return snapshot
}

func seedLedgers(t *testing.T, s executionLedgerAPI, ctx context.Context, label string) {
	t.Helper()
	j := &types.BulkJob{ID: "same", RequestID: "request", RequestHash: label, Operation: "archive", State: "queued", SubmittedBy: label, CreatedAt: "2026-09-12T00:00:00Z", Request: types.BulkJobRequest{Label: label}}
	collisionMust(t, s.InsertBulkJob(ctx, j, []types.BulkJobItem{{Sequence: 1, Path: "path", Title: label, Fingerprint: label, State: "pending"}, {Sequence: 2, Path: "second", State: "pending"}, {Sequence: 3, Path: "third", State: "failed"}}))
	ok, err := s.ConfigureExecutionBudget(ctx, types.ExecutionBudget{ID: "same", Project: "p", Timezone: "America/Denver", Unit: "pages", Limit: 5}, 0)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	_, ok, err = s.ReserveBudget(ctx, "p", "same", "root", "", 1, time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC))
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = s.CompareSupervisorCheckpoint(ctx, 0, &types.SupervisorCheckpoint{ID: "same", Project: "p", Revision: 1, Artifact: label, State: "pending"})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = s.BeginSupervisorOperation(ctx, "actor", "same", "prompt", label)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
}

func TestExecutionLedgersBulkIsolation(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	before := ledgerSnapshot(t, db, "b")
	for _, probe := range []struct {
		s     executionLedgerAPI
		ctx   context.Context
		label string
	}{{a, ca, "A"}, {b, cb, "B"}} {
		jobs, err := probe.s.ListBulkJobs(probe.ctx)
		if err != nil || len(jobs) != 1 || jobs[0].Label != probe.label || jobs[0].Total != 3 {
			t.Fatal(jobs, err)
		}
		job, err := probe.s.BulkJobByRequest(probe.ctx, "request")
		if err != nil || job.RequestHash != probe.label {
			t.Fatal(job, err)
		}
		job, err = probe.s.GetBulkJob(probe.ctx, "missing")
		if err != nil || job != nil {
			t.Fatal(job, err)
		}
		job, err = probe.s.NextRunnableBulkJob(probe.ctx)
		if err != nil || job.Request.Label != probe.label {
			t.Fatal(job, err)
		}
	}
	item, err := a.ClaimBulkJobItem(ca, "same")
	if err != nil || item != nil {
		t.Fatal("queued item claimed", item, err)
	}
	collisionMust(t, a.TransitionBulkJob(ca, "same", "queued", "running"))
	item, err = a.ClaimBulkJobItem(ca, "same")
	if err != nil || item == nil || item.Title != "A" || item.Attempts != 1 {
		t.Fatal(item, err)
	}
	collisionMust(t, a.RecoverBulkJobs(ca))
	collisionMust(t, a.TransitionBulkJob(ca, "same", "queued", "running"))
	item, err = a.ClaimBulkJobItem(ca, "same")
	if err != nil || item == nil || item.Sequence != 2 {
		t.Fatal("replayed uncertain item", item, err)
	}
	collisionMust(t, a.QuarantineBulkJobItems(ca, "same"))
	collisionMust(t, a.TransitionBulkJob(ca, "same", "running", "needs_attention"))
	collisionMust(t, a.RetryBulkJob(ca, "same"))
	collisionMust(t, a.TransitionBulkJob(ca, "same", "queued", "running"))
	item, err = a.ClaimBulkJobItem(ca, "same")
	if err != nil || item == nil || item.Sequence != 3 {
		t.Fatal("retry must claim only failed item", item, err)
	}
	item.State = "succeeded"
	item.Destination = "destination"
	collisionMust(t, a.FinishBulkJobItem(ca, "same", *item))
	item, err = a.ClaimBulkJobItem(ca, "same")
	if err != nil || item != nil {
		t.Fatal("uncertain replay", item, err)
	}
	items, err := a.BulkJobItems(ca, "same", -1, 999)
	if err != nil || len(items) != 3 || items[0].State != "uncertain" || items[2].Destination != "destination" {
		t.Fatal(items, err)
	}
	job, err := a.GetBulkJob(ca, "same")
	if err != nil || job.Uncertain != 2 || job.Succeeded != 1 || job.Pending != 0 {
		t.Fatal(job, err)
	}
	if before != ledgerSnapshot(t, db, "b") {
		t.Fatal("foreign ledger mutation")
	}
	// Duplicate second item fails after parent/first item insert; all must roll back.
	ours := ledgerSnapshot(t, db, "a")
	err = a.InsertBulkJob(ca, &types.BulkJob{ID: "rollback", RequestID: "rollback"}, []types.BulkJobItem{{Sequence: 1, Path: "x"}, {Sequence: 2, Path: "x"}})
	if err == nil || ours != ledgerSnapshot(t, db, "a") || before != ledgerSnapshot(t, db, "b") {
		t.Fatal("bulk insert not atomic", err)
	}
}

func TestExecutionLedgersBudgets(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	before := ledgerSnapshot(t, db, "b")
	now := time.Date(2026, 9, 12, 7, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	admitted := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprint("child-", i)
			_, ok, err := a.ReserveBudget(ca, "p", "same", id, "root", 1, now)
			if err == nil && ok {
				admitted <- id
			}
		}(i)
	}
	wg.Wait()
	close(admitted)
	count := 0
	for id := range admitted {
		count++
		collisionMust(t, a.SettleBudget(ca, "p", "same", id, "committed"))
		collisionMust(t, a.SettleBudget(ca, "p", "same", id, "committed"))
		if a.SettleBudget(ca, "p", "same", id, "cancelled") == nil {
			t.Fatal("committed work refunded")
		}
	}
	if count != 4 {
		t.Fatalf("admitted %d children, want 4", count)
	}
	prior, ok, err := a.ReserveBudget(ca, "p", "same", "root", "", 1, now.Add(24*time.Hour))
	if err != nil || ok || prior.Window != "2026-09-12" {
		t.Fatal(prior, ok, err)
	}
	_, _, err = a.ReserveBudget(ca, "p", "same", "root", "", 2, now)
	if err == nil {
		t.Fatal("different work reused reservation")
	}
	if before != ledgerSnapshot(t, db, "b") {
		t.Fatal("concurrent reservations changed foreign budget")
	}
	_, _, err = b.ReserveBudget(cb, "p", "same", "foreign-only", "", 1, now)
	collisionMust(t, err)
	before = ledgerSnapshot(t, db, "b")
	_, _, err = a.ReserveBudget(ca, "p", "same", "bad-parent", "foreign-only", 1, now.Add(24*time.Hour))
	if err == nil {
		t.Fatal("foreign parent accepted")
	}
	budget, used, window, err := a.ExecutionBudget(ca, "p", "same", now)
	if err != nil || used != 5 || window != "2026-09-12" {
		t.Fatal(budget, used, window, err)
	}
	_, used, _, err = a.ExecutionBudget(ca, "p", "same", now.Add(24*time.Hour))
	if err != nil || used != 0 {
		t.Fatal(used, err)
	}
	budget.Limit = 6
	ok, err = a.ConfigureExecutionBudget(ca, *budget, 1)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	ok, err = a.ConfigureExecutionBudget(ca, *budget, 1)
	if err != nil || ok {
		t.Fatal("stale CAS", ok, err)
	}
	budget.Timezone = "UTC"
	ok, err = a.ConfigureExecutionBudget(ca, *budget, 2)
	if err != nil || ok {
		t.Fatal("timezone reset", ok, err)
	}
	budget.Timezone = "America/Denver"
	budget.Unit = "tokens"
	ok, err = a.ConfigureExecutionBudget(ca, *budget, 2)
	if err != nil || ok {
		t.Fatal("unit reset", ok, err)
	}
	collisionMust(t, a.SettleBudget(ca, "p", "same", "root", "cancelled"))
	if a.SettleBudget(ca, "p", "same", "missing", "cancelled") == nil {
		t.Fatal("missing settlement accepted")
	}
	if before != ledgerSnapshot(t, db, "b") {
		t.Fatal("foreign budget changed")
	}
}

func TestExecutionLedgersCheckpointsAndReceipts(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	before := ledgerSnapshot(t, db, "b")
	value := types.SupervisorCheckpoint{ID: "same", Project: "p", Artifact: "A", Revision: 2, State: "answered", Answer: "yes"}
	ok, err := a.CompareSupervisorCheckpoint(ca, 1, &value)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	value.Revision = 3
	value.Answer = ""
	value.State = "pending"
	ok, err = a.CompareSupervisorCheckpoint(ca, 1, &value)
	if err != nil || ok {
		t.Fatal("stale CAS", ok, err)
	}
	ok, err = a.CompareSupervisorCheckpoint(ca, 2, &value)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	versions, err := a.SupervisorCheckpointVersions(ca, "p", "same")
	if err != nil || len(versions) != 3 || versions[1].Answer != "yes" || versions[0].Answer != "" {
		t.Fatal(versions, err)
	}
	rows, err := a.SupervisorCheckpoints(ca, "p", "")
	if err != nil || len(rows) != 1 || rows[0].Artifact != "A" {
		t.Fatal(rows, err)
	}
	rows, err = a.SupervisorCheckpoints(ca, "p", "same")
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	hidden, err := a.SupervisorCheckpoint(ca, "other", "same")
	if err != nil || hidden != nil {
		t.Fatal(hidden, err)
	}
	// Collision in the history PK forces a failure AFTER current-row CAS.
	ours := ledgerSnapshot(t, db, "a")
	value.Revision = 2
	ok, err = a.CompareSupervisorCheckpoint(ca, 3, &value)
	if err == nil || ok || ours != ledgerSnapshot(t, db, "a") {
		t.Fatal("history failure did not roll back current", ok, err)
	}
	var wg sync.WaitGroup
	wins := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := a.BeginSupervisorOperation(ca, "actor", "contended", "prompt", "digest")
			if err != nil {
				t.Error(err)
			}
			wins <- ok
		}()
	}
	wg.Wait()
	close(wins)
	count := 0
	for ok := range wins {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatal("multiple receipt owners", count)
	}
	collisionMust(t, a.FinishSupervisorOperation(ca, "actor", "same", "delivered", "accepted, not completed"))
	ok, err = a.BeginSupervisorOperation(ca, "actor", "same", "prompt", "different")
	if err != nil || ok {
		t.Fatal("existing receipt replay", ok, err)
	}
	r, digest, err := a.SupervisorOperation(ca, "actor", "same")
	if err != nil || r.State != "delivered" || digest != "A" {
		t.Fatal(r, digest, err)
	}
	r, _, err = a.SupervisorOperation(ca, "other", "same")
	if err != nil || r != nil {
		t.Fatal("foreign actor receipt", r, err)
	}
	ok, err = a.BeginSupervisorOperation(ca, "other", "same", "prompt", "other")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	_, err = db.Exec(`UPDATE supervisor_operations SET created_at='2000-01-01T00:00:00Z' WHERE tenant_id='a' AND actor='other'`)
	collisionMust(t, err)
	r, _, err = a.SupervisorOperation(ca, "other", "same")
	if err != nil || r.State != "outcome_unknown" {
		t.Fatal(r, err)
	}
	ok, err = a.BeginSupervisorOperation(ca, "other", "same", "prompt", "other")
	if err != nil || ok {
		t.Fatal("unknown outcome replay", ok, err)
	}
	if before != ledgerSnapshot(t, db, "b") {
		t.Fatal("foreign checkpoint/receipt changed")
	}
}

// Every public entry point must reject a mismatched context before SQL, including
// missing rows, empty results and no-op transitions. Reflection enumerates the
// typed interface; values are only used on denial paths.
func assertLedgerDenied(t *testing.T, api executionLedgerAPI, ctx context.Context) {
	t.Helper()
	v := reflect.ValueOf(api)
	typ := reflect.TypeOf((*executionLedgerAPI)(nil)).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		args := []reflect.Value{reflect.ValueOf(&ctx).Elem()}
		for j := 1; j < m.Type.NumIn(); j++ {
			args = append(args, reflect.Zero(m.Type.In(j)))
		}
		out := v.MethodByName(m.Name).Call(args)
		if out[len(out)-1].IsNil() {
			t.Errorf("%s accepted denied scope", m.Name)
		}
	}
}

func TestExecutionLedgersScopeGuards(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	before := ledgerSnapshot(t, db, "a") + ledgerSnapshot(t, db, "b")
	assertLedgerDenied(t, a, cb)
	assertLedgerDenied(t, a, context.Background())
	assertLedgerDenied(t, a, nil)
	cancelled, cancel := context.WithCancel(ca)
	cancel()
	assertLedgerDenied(t, a, cancelled)
	if before != ledgerSnapshot(t, db, "a")+ledgerSnapshot(t, db, "b") {
		t.Fatal("scope denial mutated ledgers")
	}
	for _, mutation := range []string{"INSERT INTO schema_version(version) VALUES(31)", "CREATE TABLE unexpected(id TEXT)", "DROP TABLE supervisor_checkpoint_versions", "CREATE TEMP TABLE bulk_jobs(id TEXT)"} {
		t.Run(mutation, func(t *testing.T) {
			db, a, _, ca, _ := ledgerFixture(t)
			_, err := db.Exec(mutation)
			collisionMust(t, err)
			assertLedgerDenied(t, a, ca)
		})
	}
	// Constructors must not treat this narrow test catalog as a valid successor.
	if err := InitSchema(db); err == nil {
		t.Fatal("partial ledger catalog admitted")
	}
	tx, err := db.Begin()
	collisionMust(t, err)
	_, err = classifySchemaSource(context.Background(), tx)
	_ = tx.Rollback()
	if err == nil {
		t.Fatal("partial catalog classified as complete")
	}
}

func TestExecutionLedgersMainProfiles(t *testing.T) {
	for _, source := range provenanceSources {
		t.Run(source.profile+source.revision[:8], func(t *testing.T) {
			db := archivedSchemaFixture(t, source.revision)
			local := ledgerAPI(t, db, tenant.Local)
			ctx := tenant.Into(context.Background(), tenant.Local)
			foreign, err := tenant.Parse("foreign")
			collisionMust(t, err)
			assertLedgerDenied(t, ledgerAPI(t, db, foreign), tenant.Into(context.Background(), foreign))
			if source.profile == "main28" {
				assertLedgerDenied(t, local, ctx)
				return
			}
			collisionMust(t, local.InsertBulkJob(ctx, &types.BulkJob{ID: "main", RequestID: "main", State: "queued"}, nil))
			if source.profile == "main29" {
				if _, err := local.BeginSupervisorOperation(ctx, "actor", "id", "prompt", "digest"); err == nil {
					t.Fatal("missing supervisor schema admitted")
				}
				return
			}
			seedLedgers(t, local, ctx, "main")
		})
	}
}

func TestExecutionLedgersOwnershipConstraints(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	for _, statement := range []string{
		`INSERT INTO supervisor_operations VALUES('missing','actor','id','prompt','hash','running','now','now','')`,
		`INSERT INTO bulk_job_items(tenant_id,job_id,sequence,path,entry_id,title,fingerprint,state) VALUES('a','foreign',1,'path','','','','pending')`,
		`INSERT INTO budget_reservations VALUES('a','other','same','id','','today',1,'reserved')`,
		`INSERT INTO supervisor_checkpoint_versions VALUES('a','other','same',1,'{}')`,
	} {
		if _, err := db.Exec(statement); err == nil {
			t.Fatal("invalid ownership accepted", statement)
		}
	}
	before := ledgerSnapshot(t, db, "b")
	_, err := db.Exec(`DELETE FROM bulk_jobs WHERE tenant_id='a' AND id='same'`)
	collisionMust(t, err)
	items, err := a.BulkJobItems(ca, "same", 0, 100)
	if err != nil || len(items) != 0 {
		t.Fatal("item cascade", items, err)
	}
	if before != ledgerSnapshot(t, db, "b") {
		t.Fatal("foreign cascade")
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	collisionMust(t, err)
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
	collisionMust(t, rows.Err())
}

func TestExecutionLedgersReopen(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	collisionMust(t, a.TransitionBulkJob(ca, "same", "queued", "running"))
	item, err := a.ClaimBulkJobItem(ca, "same")
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	collisionMust(t, a.SettleBudget(ca, "p", "same", "root", "committed"))
	_, err = db.Exec(`UPDATE supervisor_operations SET created_at='2000-01-01T00:00:00Z' WHERE tenant_id='a'`)
	collisionMust(t, err)
	beforeA, beforeB := ledgerSnapshot(t, db, "a"), ledgerSnapshot(t, db, "b")
	var seq int
	var name, path string
	collisionMust(t, db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path))
	collisionMust(t, db.Close())
	reopened := compatibilityDB(t, path)
	reopened.SetMaxOpenConns(1)
	_, err = reopened.Exec("PRAGMA foreign_keys=ON")
	collisionMust(t, err)
	id, err := tenant.Parse("a")
	collisionMust(t, err)
	a = ledgerAPI(t, reopened, id)
	if beforeA != ledgerSnapshot(t, reopened, "a") || beforeB != ledgerSnapshot(t, reopened, "b") {
		t.Fatal("reopen changed persisted ledgers")
	}
	r, _, err := a.SupervisorOperation(ca, "actor", "same")
	if err != nil || r.State != "outcome_unknown" {
		t.Fatal(r, err)
	}
	ok, err := a.BeginSupervisorOperation(ca, "actor", "same", "prompt", "A")
	if err != nil || ok {
		t.Fatal("reopen replayed operation", ok, err)
	}
	collisionMust(t, a.RecoverBulkJobs(ca))
	collisionMust(t, a.RecoverBulkJobs(ca))
	items, err := a.BulkJobItems(ca, "same", 0, 100)
	if err != nil || len(items) != 3 || items[0].State != "uncertain" || items[0].Attempts != 1 || items[0].Fingerprint != "A" {
		t.Fatal(items, err)
	}
	_, ok, err = a.ReserveBudget(ca, "p", "same", "root", "", 1, time.Now())
	if err != nil || ok {
		t.Fatal("reopen charged twice", ok, err)
	}
	if a.SettleBudget(ca, "p", "same", "root", "cancelled") == nil {
		t.Fatal("reopen refunded committed work")
	}
	if beforeB != ledgerSnapshot(t, reopened, "b") {
		t.Fatal("recovery changed foreign snapshot")
	}
	if got := compatibilityRows(t, reopened, "PRAGMA integrity_check"); !reflect.DeepEqual(got, []string{"ok"}) {
		t.Fatal(got)
	}
	if got := compatibilityRows(t, reopened, "PRAGMA foreign_key_check"); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestExecutionLedgersExactTableInventory(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	definitions := successorLedgerDefinitions()
	if len(definitions) != 8 {
		t.Fatalf("want seven tables + one index, got %d", len(definitions))
	}
	for _, table := range ledgerTables {
		var ddl string
		collisionMust(t, db.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", table).Scan(&ddl))
		if ddl != definitions[table] {
			t.Fatalf("DDL differs from independent contract: %s", table)
		}
		for _, owner := range []string{"a", "b"} {
			var n int
			collisionMust(t, db.QueryRow("SELECT count(*) FROM "+table+" WHERE tenant_id=?", owner).Scan(&n))
			if n == 0 {
				t.Fatalf("vacuous %s/%s", owner, table)
			}
		}
		var fks int
		collisionMust(t, db.QueryRow("SELECT count(*) FROM pragma_foreign_key_list(?) WHERE \"table\"='tenants' AND \"from\"='tenant_id' AND \"to\"='id'", table).Scan(&fks))
		if fks != 1 {
			t.Fatalf("missing direct tenant ownership for %s", table)
		}
	}
	actual := compatibilityRows(t, db, `SELECT name FROM sqlite_schema WHERE type='table' AND name NOT IN ('tenants','schema_version') ORDER BY name`)
	want := []string{"budget_reservations", "bulk_job_items", "bulk_jobs", "execution_budgets", "supervisor_checkpoint_versions", "supervisor_checkpoints", "supervisor_operations"}
	if !reflect.DeepEqual(actual, want) {
		t.Fatal("partial table inventory drift", actual)
	}
}

func TestExecutionLedgersLimitsAndForeignReads(t *testing.T) {
	db, a, b, ca, cb := ledgerFixture(t)
	seedLedgers(t, a, ca, "A")
	seedLedgers(t, b, cb, "B")
	// SQL fixture loading is outside the runtime-under-test and keeps the exact
	// catalog intact; foreign corpus must not consume the bound tenant's limits.
	for i := 0; i < 105; i++ {
		id := fmt.Sprintf("id-%03d", i)
		collisionMust(t, b.InsertBulkJob(cb, &types.BulkJob{ID: id, RequestID: id, State: "queued", Request: types.BulkJobRequest{Label: "B"}}, nil))
		ok, err := b.CompareSupervisorCheckpoint(cb, 0, &types.SupervisorCheckpoint{ID: id, Project: "p", Revision: 1, Artifact: "B"})
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	beforeA, beforeB := ledgerSnapshot(t, db, "a"), ledgerSnapshot(t, db, "b")
	jobs, err := a.ListBulkJobs(ca)
	if err != nil || len(jobs) != 1 || jobs[0].Label != "A" {
		t.Fatal(jobs, err)
	}
	jobs, err = b.ListBulkJobs(cb)
	if err != nil || len(jobs) != 100 {
		t.Fatal(len(jobs), err)
	}
	checkpoints, err := a.SupervisorCheckpoints(ca, "p", "")
	if err != nil || len(checkpoints) != 1 || checkpoints[0].Artifact != "A" {
		t.Fatal(checkpoints, err)
	}
	checkpoints, err = b.SupervisorCheckpoints(cb, "p", "")
	if err != nil || len(checkpoints) != 101 {
		t.Fatal(len(checkpoints), err)
	}
	job, err := a.GetBulkJob(ca, "id-000")
	if err != nil || job != nil {
		t.Fatal("foreign-only job", job, err)
	}
	job, err = a.BulkJobByRequest(ca, "id-000")
	if err != nil || job != nil {
		t.Fatal("foreign request", job, err)
	}
	checkpoint, err := a.SupervisorCheckpoint(ca, "p", "id-000")
	if err != nil || checkpoint != nil {
		t.Fatal("foreign checkpoint", checkpoint, err)
	}
	history, err := a.SupervisorCheckpointVersions(ca, "p", "id-000")
	if err != nil || len(history) != 0 {
		t.Fatal(history, err)
	}
	items, err := a.BulkJobItems(ca, "same", 1, 1)
	if err != nil || len(items) != 1 || items[0].Sequence != 1 {
		t.Fatal(items, err)
	}
	if beforeA != ledgerSnapshot(t, db, "a") || beforeB != ledgerSnapshot(t, db, "b") {
		t.Fatal("reads mutated ledger state")
	}
}

func TestExecutionLedgersInvalidHandles(t *testing.T) {
	db, _, _, ctx, _ := ledgerFixture(t)
	for name, s := range map[string]*TenantStore{"nil": nil, "zero": {}, "invalid-binding": {db: db}} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Errorf("invalid handle panicked instead of returning scope error: %v", recovered)
				}
			}()
			assertLedgerDenied(t, s, ctx)
		})
	}
}

func TestExecutionLedgersBulkMutationIsolation(t *testing.T) {
	for _, action := range []string{"finish", "recover", "quarantine", "retry", "transition", "claim"} {
		t.Run(action, func(t *testing.T) {
			db, a, b, ca, cb := ledgerFixture(t)
			seedLedgers(t, a, ca, "A")
			seedLedgers(t, b, cb, "B")
			// Make foreign rows equally eligible for EACH state-conditional write.
			for _, p := range []struct {
				s   executionLedgerAPI
				ctx context.Context
			}{{a, ca}, {b, cb}} {
				collisionMust(t, p.s.TransitionBulkJob(p.ctx, "same", "queued", "running"))
				item, err := p.s.ClaimBulkJobItem(p.ctx, "same")
				if err != nil || item == nil {
					t.Fatal(item, err)
				}
				if action == "retry" {
					collisionMust(t, p.s.TransitionBulkJob(p.ctx, "same", "running", "needs_attention"))
				}
			}
			ours, foreign := ledgerSnapshot(t, db, "a"), ledgerSnapshot(t, db, "b")
			switch action {
			case "finish":
				collisionMust(t, a.FinishBulkJobItem(ca, "same", types.BulkJobItem{Sequence: 1, State: "succeeded", Destination: "done"}))
			case "recover":
				collisionMust(t, a.RecoverBulkJobs(ca))
			case "quarantine":
				collisionMust(t, a.QuarantineBulkJobItems(ca, "same"))
			case "retry":
				collisionMust(t, a.RetryBulkJob(ca, "same"))
			case "transition":
				collisionMust(t, a.TransitionBulkJob(ca, "same", "running", "paused"))
			case "claim":
				item, err := a.ClaimBulkJobItem(ca, "same")
				if err != nil || item == nil || item.Sequence != 2 {
					t.Fatal(item, err)
				}
			}
			if ours == ledgerSnapshot(t, db, "a") {
				t.Fatal("vacuous own mutation")
			}
			if foreign != ledgerSnapshot(t, db, "b") {
				t.Fatal("eligible foreign rows changed")
			}
		})
	}
}

func TestExecutionLedgersBootstrapRefusal(t *testing.T) {
	for _, entry := range []string{"InitSchema", "New", "NewWithDB"} {
		t.Run(entry, func(t *testing.T) {
			db, a, b, ca, cb := ledgerFixture(t)
			seedLedgers(t, a, ca, "A")
			seedLedgers(t, b, cb, "B")
			before := ledgerSnapshot(t, db, "a") + ledgerSnapshot(t, db, "b")
			catalog := compatibilityRows(t, db, "SELECT type||name||coalesce(sql,'') FROM sqlite_schema ORDER BY type,name")
			journal := compatibilityRows(t, db, "PRAGMA journal_mode")
			var err error
			switch entry {
			case "InitSchema":
				err = InitSchema(db)
			case "NewWithDB":
				var owner *StorageLayer
				owner, err = NewWithDB(db)
				if owner != nil {
					_ = owner.Close()
					t.Fatal("returned partial owner")
				}
			case "New":
				var seq int
				var name, path string
				collisionMust(t, db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path))
				var owner *StorageLayer
				owner, err = New(path)
				if owner != nil {
					_ = owner.Close()
					t.Fatal("returned partial owner")
				}
			}
			if err == nil {
				t.Fatal("partial catalog admitted")
			}
			if before != ledgerSnapshot(t, db, "a")+ledgerSnapshot(t, db, "b") || !reflect.DeepEqual(catalog, compatibilityRows(t, db, "SELECT type||name||coalesce(sql,'') FROM sqlite_schema ORDER BY type,name")) || !reflect.DeepEqual(journal, compatibilityRows(t, db, "PRAGMA journal_mode")) {
				t.Fatal("startup refusal mutated database")
			}
		})
	}
}
