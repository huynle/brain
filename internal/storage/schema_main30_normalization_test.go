package storage

import (
	"context"
	"database/sql"
	"os"
	"testing"
)

const historicalMain30Digest = "2eade101a417bb1982de902e36663a9229ea2e49b3c8d7f2231e1d139ef06b0f"

func historicalMain30Fixture(t *testing.T) *sql.DB {
	t.Helper()
	db := compatibilityDB(t, t.TempDir()+"/historical.db")
	if err := InitSchema(db); err != nil {
		t.Fatal(err)
	}
	ddl := []string{
		`DROP TABLE api_tokens`,
		`CREATE TABLE api_tokens (
  name TEXT PRIMARY KEY,
  token TEXT UNIQUE NOT NULL,
  created_at TEXT DEFAULT (datetime('now')),
  last_used TEXT
, revoked_at TEXT, scope TEXT NOT NULL DEFAULT 'admin:*')`,
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
		`DROP TABLE runners`,
		`CREATE TABLE runners (
  runner_id TEXT PRIMARY KEY,
  hostname TEXT NOT NULL DEFAULT '',
  projects TEXT NOT NULL DEFAULT '[]',
  capabilities TEXT NOT NULL DEFAULT '[]',
  max_parallel INTEGER NOT NULL DEFAULT 1,
  active_tasks INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'online',
  version TEXT NOT NULL DEFAULT '',
  registered_at TEXT NOT NULL DEFAULT (datetime('now')),
  last_heartbeat TEXT NOT NULL DEFAULT (datetime('now'))
, labels TEXT DEFAULT '{}', executors TEXT DEFAULT '[]', feature_ids TEXT DEFAULT '', machine_id TEXT DEFAULT '', dispatch_push INTEGER NOT NULL DEFAULT 0, workspace_roots TEXT DEFAULT '[]', resources TEXT DEFAULT '{}', capacity TEXT DEFAULT '{}', draining INTEGER NOT NULL DEFAULT 0)`,
		`CREATE INDEX idx_runners_last_heartbeat ON runners(last_heartbeat)`,
		`CREATE INDEX idx_runners_status ON runners(status)`,
		`CREATE INDEX idx_opencode_instances_runner ON opencode_instances(runner_id)`,
		`CREATE INDEX idx_opencode_instances_task ON opencode_instances(project_id, task_id)`,
		`CREATE TABLE vec_preflight(id INTEGER PRIMARY KEY, embedding BLOB)`,
		`INSERT INTO vec_preflight VALUES(1,x'CDCCCC3DCDCC4C3E9A99993ECDCCCC3E0000003F9A99193F3333333FCDCC4C3F')`,
		`INSERT INTO api_tokens(name,token,created_at,last_used,revoked_at,scope) VALUES('owner','secret','created','used',NULL,'admin:*')`,
		`INSERT INTO opencode_instances(instance_id,runner_id,hostname,kind,project_id,task_id,title,workdir,port,pid,session_ids,status,executor,started_at,last_seen,feature_id,priority,agent,model) VALUES('instance','runner','host','task','project','task','title','/tmp',42,43,'["session"]','idle','opencode',44,45,'feature','high','agent','model')`,
		`INSERT INTO runners(runner_id,hostname,projects,capabilities,max_parallel,active_tasks,status,version,registered_at,last_heartbeat,labels,executors,feature_ids,machine_id,dispatch_push,workspace_roots,resources,capacity,draining) VALUES('runner','host','["project"]','["control"]',2,0,'offline','','1782258545897','1782258545898','{}','["opencode"]','feature','machine',1,'["/tmp"]','{}','{}',0)`,
		`INSERT INTO event_log(event_type,source,payload,created_at) VALUES('task.completed','runner','{"project_id":"project","task_id":"task"}','created')`,
	}
	for _, statement := range ddl {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("fixture statement failed: %v\n%s", err, statement)
		}
	}
	if path := os.Getenv("BRAIN_HISTORICAL_FIXTURE_COPY"); path != "" {
		if _, err := db.Exec("VACUUM INTO ?", path); err != nil {
			t.Fatalf("copy fixture: %v", err)
		}
	}
	return db
}

func TestNormalizeHistoricalMain30(t *testing.T) {
	db := historicalMain30Fixture(t)
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := classifySchemaSource(context.Background(), tx)
	_ = tx.Rollback()
	if err != nil || profile != "main30-historical-preflight" {
		t.Fatalf("historical source classification = %q, %v", profile, err)
	}

	if err := normalizeHistoricalMain30(context.Background(), db); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	tx, err = db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	profile, err = classifySchemaSource(context.Background(), tx)
	_ = tx.Rollback()
	if err != nil || profile != "main30-devices" {
		t.Fatalf("normalized classification = %q, %v", profile, err)
	}
	for table, want := range map[string]int{"api_tokens": 1, "opencode_instances": 1, "runners": 1, "event_log": 1} {
		var got int
		if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s count = %d, %v; want %d", table, got, err, want)
		}
	}
	var token, scope, created, used string
	if err := db.QueryRow("SELECT token,scope,created_at,last_used FROM api_tokens WHERE name='owner'").Scan(&token, &scope, &created, &used); err != nil || token != "secret" || scope != "admin:*" || created != "created" || used != "used" {
		t.Fatalf("api token values changed: %q %q %q %q: %v", token, scope, created, used, err)
	}
	var sessionIDs, model string
	var port, lastSeen int
	if err := db.QueryRow("SELECT session_ids,model,port,last_seen FROM opencode_instances WHERE instance_id='instance'").Scan(&sessionIDs, &model, &port, &lastSeen); err != nil || sessionIDs != `["session"]` || model != "model" || port != 42 || lastSeen != 45 {
		t.Fatalf("instance values changed: %q %q %d %d: %v", sessionIDs, model, port, lastSeen, err)
	}
	var projects, capabilities string
	var registeredAt, lastHeartbeat int64
	if err := db.QueryRow("SELECT projects,capabilities,registered_at,last_heartbeat FROM runners WHERE runner_id='runner'").Scan(&projects, &capabilities, &registeredAt, &lastHeartbeat); err != nil || projects != `["project"]` || capabilities != `["control"]` || registeredAt != 1782258545897 || lastHeartbeat != 1782258545898 {
		t.Fatalf("runner values changed: %q %q %d %d: %v", projects, capabilities, registeredAt, lastHeartbeat, err)
	}
	var active, version int
	if err := db.QueryRow("SELECT active_tasks,length(version) FROM runners").Scan(&active, &version); err == nil {
		t.Fatal("historical-only runner columns survived normalization")
	}
	var extras int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name IN ('vec_preflight','idx_runners_last_heartbeat')").Scan(&extras); err != nil || extras != 0 {
		t.Fatalf("historical extras remain: %d, %v", extras, err)
	}
}

func TestNormalizeHistoricalMain30RefusesChangedPreflight(t *testing.T) {
	db := historicalMain30Fixture(t)
	if _, err := db.Exec("UPDATE vec_preflight SET embedding=x'00'"); err != nil {
		t.Fatal(err)
	}
	if err := normalizeHistoricalMain30(context.Background(), db); err == nil {
		t.Fatal("changed preflight row accepted")
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM runners").Scan(&count); err != nil || count != 1 {
		t.Fatal("refusal mutated source")
	}
}

func TestNormalizeHistoricalMain30RefusesDatetimeRunnerTimestamps(t *testing.T) {
	db := historicalMain30Fixture(t)
	if _, err := db.Exec("UPDATE runners SET registered_at='2026-09-29 14:30:00'"); err != nil {
		t.Fatal(err)
	}
	if err := normalizeHistoricalMain30(context.Background(), db); err == nil {
		t.Fatal("datetime timestamp accepted for integer normalization")
	}
	var value string
	if err := db.QueryRow("SELECT registered_at FROM runners").Scan(&value); err != nil || value != "2026-09-29 14:30:00" {
		t.Fatal("refusal mutated source")
	}
}

func TestNewWithDBNormalizesHistoricalMain30(t *testing.T) {
	db := historicalMain30Fixture(t)
	store, err := NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	defer store.Close()
	profile, err := schemaProfile(context.Background(), db)
	if err != nil || profile != "main30-devices" {
		t.Fatalf("constructor left profile %q: %v", profile, err)
	}
}
