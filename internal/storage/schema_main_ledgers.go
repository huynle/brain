package storage

// Genuine single-mode DDL from pinned main cd22b4bd. These definitions are
// distinct from the dormant successor's ownership/registry foreign keys.
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
const createExecutionBudgets = `CREATE TABLE IF NOT EXISTS execution_budgets (tenant_id TEXT NOT NULL,project TEXT NOT NULL,id TEXT NOT NULL,timezone TEXT NOT NULL,unit TEXT NOT NULL,limit_units INTEGER NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(tenant_id,project,id))`
const createBudgetReservations = `CREATE TABLE IF NOT EXISTS budget_reservations (tenant_id TEXT NOT NULL,project TEXT NOT NULL,budget_id TEXT NOT NULL,id TEXT NOT NULL,parent_id TEXT NOT NULL,window TEXT NOT NULL,units INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(tenant_id,project,budget_id,id),FOREIGN KEY(tenant_id,project,budget_id) REFERENCES execution_budgets(tenant_id,project,id))`
const createSupervisorCheckpoints = `CREATE TABLE IF NOT EXISTS supervisor_checkpoints (tenant_id TEXT NOT NULL, project TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(tenant_id,project,id))`
const createSupervisorCheckpointVersions = `CREATE TABLE IF NOT EXISTS supervisor_checkpoint_versions (tenant_id TEXT NOT NULL,project TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(tenant_id,project,id,revision))`
const createSupervisorOperations = `CREATE TABLE IF NOT EXISTS supervisor_operations (
 tenant_id TEXT NOT NULL, actor TEXT NOT NULL, id TEXT NOT NULL, operation TEXT NOT NULL,
 digest TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,actor,id))`
