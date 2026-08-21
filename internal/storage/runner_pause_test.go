package storage

import (
	"context"
	"testing"
)

func seedPauseTestRunner(t *testing.T, s *StorageLayer, runnerID string) {
	t.Helper()
	row := &RunnerRow{
		RunnerID:      runnerID,
		Hostname:      "host-1",
		Labels:        map[string]string{},
		Executors:     []string{"opencode"},
		Capabilities:  []string{},
		DispatchPush:  true,
		MaxParallel:   2,
		RegisteredAt:  1000,
		LastHeartbeat: 1000,
		Status:        "online",
	}
	if err := s.UpsertRunner(context.Background(), row); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}
}

func TestSetRunnerPaused_RoundTrips(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedPauseTestRunner(t, s, "runner-1")

	found, err := s.SetRunnerPaused(ctx, "runner-1", true)
	if err != nil {
		t.Fatalf("SetRunnerPaused failed: %v", err)
	}
	if !found {
		t.Fatal("SetRunnerPaused found = false, want true")
	}

	got, err := s.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if !got.Paused {
		t.Error("GetRunner Paused = false, want true")
	}

	listed, err := s.ListRunners(ctx)
	if err != nil {
		t.Fatalf("ListRunners failed: %v", err)
	}
	if len(listed) != 1 || !listed[0].Paused {
		t.Errorf("ListRunners = %+v, want one paused runner (the scheduler reads eligibility from here)", listed)
	}

	if _, err := s.SetRunnerPaused(ctx, "runner-1", false); err != nil {
		t.Fatalf("SetRunnerPaused(false) failed: %v", err)
	}
	got, err = s.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if got.Paused {
		t.Error("GetRunner Paused = true after resume, want false")
	}
}

func TestSetRunnerPaused_UnknownRunner(t *testing.T) {
	s := newTestStorage(t)
	found, err := s.SetRunnerPaused(context.Background(), "nobody", true)
	if err != nil {
		t.Fatalf("SetRunnerPaused failed: %v", err)
	}
	if found {
		t.Error("found = true for an unknown runner, want false")
	}
}

// The whole point of persisting the dial is that a runner cannot shake it off.
// UpsertRunner is the registration path, and a headless runner re-registers on
// every start — if that cleared `paused`, restarting the runner would silently
// undo an operator's pause.
func TestUpsertRunner_DoesNotClearPause(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedPauseTestRunner(t, s, "runner-1")

	if _, err := s.SetRunnerPaused(ctx, "runner-1", true); err != nil {
		t.Fatalf("SetRunnerPaused failed: %v", err)
	}

	// Runner restarts and re-registers.
	seedPauseTestRunner(t, s, "runner-1")

	got, err := s.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if !got.Paused {
		t.Error("Paused = false after re-registration, want true (a runner must not resume itself)")
	}
}

// A pause must outlive the runners row. `brain runner stop` deregisters,
// which DELETEs that row — if the dial lived on it, a routine stop/start
// would silently resume a runner an operator had paused.
func TestRunnerPause_SurvivesDeregisterAndReregister(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	seedPauseTestRunner(t, s, "runner-1")

	if _, err := s.SetRunnerPaused(ctx, "runner-1", true); err != nil {
		t.Fatalf("SetRunnerPaused failed: %v", err)
	}

	// `brain runner stop` -> Deregister -> DeleteRunner.
	deleted, err := s.DeleteRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("DeleteRunner failed: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteRunner deleted = false, want true")
	}

	// Operator restarts the runner; it re-registers under the same ID.
	seedPauseTestRunner(t, s, "runner-1")

	got, err := s.GetRunner(ctx, "runner-1")
	if err != nil {
		t.Fatalf("GetRunner failed: %v", err)
	}
	if !got.Paused {
		t.Error("Paused = false after stop/start, want true (a restart must not resume a paused runner)")
	}
}

// Upgrading an existing database must add the pause table without losing rows.
func TestRunnerPauseState_MigrationFromV22(t *testing.T) {
	db := openMemoryDB(t)
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(createSchemaVersionTable); err != nil {
		t.Fatalf("create schema_version: %v", err)
	}
	// A v22 runners table: same DDL minus the paused column.
	if _, err := db.Exec(`CREATE TABLE runners (
	  runner_id TEXT PRIMARY KEY,
	  machine_id TEXT DEFAULT '',
	  hostname TEXT NOT NULL,
	  labels TEXT DEFAULT '{}',
	  executors TEXT DEFAULT '[]',
	  capabilities TEXT DEFAULT '[]',
	  dispatch_push INTEGER NOT NULL DEFAULT 0,
	  workspace_roots TEXT DEFAULT '[]',
	  projects TEXT DEFAULT '[]',
	  resources TEXT DEFAULT '{}',
	  capacity TEXT DEFAULT '{}',
	  draining INTEGER NOT NULL DEFAULT 0,
	  max_parallel INTEGER NOT NULL DEFAULT 1,
	  feature_ids TEXT DEFAULT '',
	  registered_at INTEGER NOT NULL,
	  last_heartbeat INTEGER NOT NULL,
	  status TEXT NOT NULL DEFAULT 'online'
	)`); err != nil {
		t.Fatalf("create legacy runners table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runners (runner_id, hostname, registered_at, last_heartbeat)
		VALUES ('legacy-runner', 'host-1', 1000, 1000)`); err != nil {
		t.Fatalf("seed legacy runner: %v", err)
	}
	if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (22)"); err != nil {
		t.Fatalf("insert v22: %v", err)
	}

	if err := migrateSchema(db); err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	var name string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='runner_pause_state'").Scan(&name); err != nil {
		t.Fatalf("runner_pause_state table missing after migration: %v", err)
	}
	var pausedRows int
	if err := db.QueryRow("SELECT COUNT(*) FROM runner_pause_state").Scan(&pausedRows); err != nil {
		t.Fatalf("count runner_pause_state: %v", err)
	}
	if pausedRows != 0 {
		t.Errorf("runner_pause_state rows = %d after migration, want 0 (migration must not pause anyone)", pausedRows)
	}
	var legacy string
	if err := db.QueryRow("SELECT runner_id FROM runners WHERE runner_id = 'legacy-runner'").Scan(&legacy); err != nil {
		t.Fatalf("pre-existing runner lost in migration: %v", err)
	}

	// Migration is re-runnable.
	if err := migrateSchema(db); err != nil {
		t.Fatalf("second migrateSchema failed: %v", err)
	}
}
