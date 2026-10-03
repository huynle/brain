package storage

import "testing"

func TestSchemaProvenanceProductionMain30MigrationLineage(t *testing.T) {
	db := compatibilityDB(t, t.TempDir()+"/production-main30.db")
	if err := InitSchema(db); err != nil {
		t.Fatal(err)
	}
	ddl := []string{
		`DROP TABLE api_tokens`,
		`CREATE TABLE api_tokens (
  name TEXT PRIMARY KEY,
  token TEXT UNIQUE NOT NULL,
  created_at TEXT DEFAULT (datetime('now')),
  last_used TEXT,
  revoked_at TEXT
, scope TEXT NOT NULL DEFAULT 'admin:*')`,
		`DROP TABLE opencode_instances`,
		`CREATE TABLE opencode_instances (
  instance_id TEXT PRIMARY KEY,
  runner_id TEXT NOT NULL,
  hostname TEXT DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'task',
  project_id TEXT DEFAULT '',
  task_id TEXT DEFAULT '',
  title TEXT DEFAULT '',
  workdir TEXT DEFAULT '',
  port INTEGER DEFAULT 0,
  pid INTEGER DEFAULT 0,
  session_ids TEXT DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'starting',
  executor TEXT DEFAULT 'opencode',
  started_at INTEGER NOT NULL DEFAULT 0,
  last_seen INTEGER NOT NULL DEFAULT 0
, feature_id TEXT DEFAULT '', priority TEXT DEFAULT '', agent TEXT DEFAULT '', model TEXT DEFAULT '')`,
		`CREATE INDEX idx_opencode_instances_runner ON opencode_instances(runner_id)`,
		`CREATE INDEX idx_opencode_instances_task ON opencode_instances(project_id, task_id)`,
		`DROP TABLE runners`,
		`CREATE TABLE runners (
  runner_id TEXT PRIMARY KEY,
  hostname TEXT NOT NULL,
  labels TEXT DEFAULT '{}',
  executors TEXT DEFAULT '[]',
  capabilities TEXT DEFAULT '[]',
  max_parallel INTEGER NOT NULL DEFAULT 1,
  feature_ids TEXT DEFAULT '',
  registered_at INTEGER NOT NULL,
  last_heartbeat INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'online'
, machine_id TEXT DEFAULT '', dispatch_push INTEGER NOT NULL DEFAULT 0, workspace_roots TEXT DEFAULT '[]', projects TEXT DEFAULT '[]', resources TEXT DEFAULT '{}', capacity TEXT DEFAULT '{}', draining INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX idx_runners_status ON runners(status)`,
	}
	for _, statement := range ddl {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("production lineage fixture: %v\n%s", err, statement)
		}
	}
	assertSourceClassification(t, db, "main30-devices")
}
