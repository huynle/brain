package storage

// Exact dormant successor workload subset, independently reviewed against main
// cd22b4bd. Never mechanically prepend tenant_id: all seven keys already own it.
// Registry FKs are added to each table; history gains its composite current-row
// FK. Parent reservation is optional empty TEXT, so its same-budget relationship
// remains transaction-validated (not a nullable reinterpretation/backfill).
// Paths, projects, actors and entry IDs are historical payloads, not live-note or
// credential FKs. No cascades that erase checkpoints/receipts are introduced.
// No initializer calls these definitions; phase 4 must compose/copy/validate them
// atomically with sync, content, FTS, roots and provenance before publication.
func successorLedgerDefinitions() map[string]string {
	return map[string]string{
		"bulk_jobs": `CREATE TABLE bulk_jobs (
 tenant_id TEXT NOT NULL REFERENCES tenants(id), id TEXT NOT NULL, request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL, request_json TEXT NOT NULL, operation TEXT NOT NULL,
 state TEXT NOT NULL, submitted_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,request_id)
)`,
		"bulk_job_items": `CREATE TABLE bulk_job_items (
 tenant_id TEXT NOT NULL REFERENCES tenants(id), job_id TEXT NOT NULL, sequence INTEGER NOT NULL,
 path TEXT NOT NULL, entry_id TEXT NOT NULL, title TEXT NOT NULL, fingerprint TEXT NOT NULL,
 state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', destination TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,job_id,sequence), UNIQUE(tenant_id,job_id,path),
 FOREIGN KEY(tenant_id,job_id) REFERENCES bulk_jobs(tenant_id,id) ON DELETE CASCADE
)`,
		"bulk_items_pending":             `CREATE INDEX bulk_items_pending ON bulk_job_items(tenant_id,job_id,state,sequence)`,
		"execution_budgets":              `CREATE TABLE execution_budgets (tenant_id TEXT NOT NULL REFERENCES tenants(id),project TEXT NOT NULL,id TEXT NOT NULL,timezone TEXT NOT NULL,unit TEXT NOT NULL,limit_units INTEGER NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(tenant_id,project,id))`,
		"budget_reservations":            `CREATE TABLE budget_reservations (tenant_id TEXT NOT NULL REFERENCES tenants(id),project TEXT NOT NULL,budget_id TEXT NOT NULL,id TEXT NOT NULL,parent_id TEXT NOT NULL,window TEXT NOT NULL,units INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(tenant_id,project,budget_id,id),FOREIGN KEY(tenant_id,project,budget_id) REFERENCES execution_budgets(tenant_id,project,id))`,
		"supervisor_checkpoints":         `CREATE TABLE supervisor_checkpoints (tenant_id TEXT NOT NULL REFERENCES tenants(id), project TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(tenant_id,project,id))`,
		"supervisor_checkpoint_versions": `CREATE TABLE supervisor_checkpoint_versions (tenant_id TEXT NOT NULL REFERENCES tenants(id),project TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(tenant_id,project,id,revision),FOREIGN KEY(tenant_id,project,id) REFERENCES supervisor_checkpoints(tenant_id,project,id))`,
		"supervisor_operations": `CREATE TABLE supervisor_operations (
 tenant_id TEXT NOT NULL REFERENCES tenants(id), actor TEXT NOT NULL, id TEXT NOT NULL, operation TEXT NOT NULL,
 digest TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,actor,id))`,
	}
}
