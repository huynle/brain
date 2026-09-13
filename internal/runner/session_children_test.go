package runner

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// newSessionFixtureDB creates <dir>/opencode/opencode.db with just the
// `session` table (mirroring OpenCode's real columns) and points
// XDG_DATA_HOME at dir so the children reader finds it. It intentionally does
// NOT touch the message/part tables so it stays independent of newFixtureDB.
func newSessionFixtureDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	dbDir := filepath.Join(dir, "opencode")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dbDir, "opencode.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(
		`CREATE TABLE session (id TEXT PRIMARY KEY, parent_id TEXT, title TEXT, time_created INTEGER, time_updated INTEGER, agent TEXT)`,
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func insertSession(t *testing.T, db *sql.DB, id, parentID, title string, created int64, agent string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO session (id, parent_id, title, time_created, time_updated, agent) VALUES (?, ?, ?, ?, ?, ?)`,
		id, parentID, title, created, created, agent,
	); err != nil {
		t.Fatal(err)
	}
}

func TestReadSessionChildrenSQLite(t *testing.T) {
	db := newSessionFixtureDB(t)

	// Parent session.
	insertSession(t, db, "ses_parent", "", "Parent", 1000, "build")
	// Two children of ses_parent, inserted out of created order so correct
	// output proves the ORDER BY time_created.
	insertSession(t, db, "ses_child_b", "ses_parent", "Child B", 2000, "explore")
	insertSession(t, db, "ses_child_a", "ses_parent", "Child A", 1500, "tdd-dev")
	// An unrelated session under a different parent that must not leak.
	insertSession(t, db, "ses_other", "ses_elsewhere", "Other", 1600, "build")

	children, err := readSessionChildrenSQLite("ses_parent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(children), children)
	}

	// Ordered by time_created: child_a (1500) before child_b (2000).
	if children[0].SessionID != "ses_child_a" {
		t.Errorf("first child = %q, want ses_child_a (oldest first)", children[0].SessionID)
	}
	if children[1].SessionID != "ses_child_b" {
		t.Errorf("second child = %q, want ses_child_b", children[1].SessionID)
	}

	got := children[0]
	if got.ParentID != "ses_parent" {
		t.Errorf("child_a ParentID = %q, want ses_parent", got.ParentID)
	}
	if got.Title != "Child A" {
		t.Errorf("child_a Title = %q, want Child A", got.Title)
	}
	if got.Created != 1500 {
		t.Errorf("child_a Created = %d, want 1500", got.Created)
	}
	if got.Agent != "tdd-dev" {
		t.Errorf("child_a Agent = %q, want tdd-dev", got.Agent)
	}

	// The unrelated session must be excluded.
	for _, c := range children {
		if c.SessionID == "ses_other" {
			t.Errorf("unrelated session ses_other leaked into children")
		}
	}
}

func TestReadSessionChildrenSQLite_NullAgentTitle(t *testing.T) {
	db := newSessionFixtureDB(t)
	insertSession(t, db, "ses_parent", "", "Parent", 1000, "build")
	// A child row with NULL agent and NULL title.
	if _, err := db.Exec(
		`INSERT INTO session (id, parent_id, title, time_created, time_updated, agent) VALUES (?, ?, NULL, ?, ?, NULL)`,
		"ses_child", "ses_parent", 1500, 1500,
	); err != nil {
		t.Fatal(err)
	}

	children, err := readSessionChildrenSQLite("ses_parent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].Title != "" {
		t.Errorf("NULL title should coalesce to \"\", got %q", children[0].Title)
	}
	if children[0].Agent != "" {
		t.Errorf("NULL agent should coalesce to \"\", got %q", children[0].Agent)
	}
	if children[0].SessionID != "ses_child" {
		t.Errorf("SessionID = %q, want ses_child", children[0].SessionID)
	}
}

func TestReadSessionChildrenSQLite_NoChildren(t *testing.T) {
	db := newSessionFixtureDB(t)
	insertSession(t, db, "ses_parent", "", "Parent", 1000, "build")

	children, err := readSessionChildrenSQLite("ses_parent")
	if err != nil {
		t.Fatalf("expected nil error for a parent with no children, got %v", err)
	}
	if children == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestReadSessionChildrenSQLite_InvalidSessionID(t *testing.T) {
	newSessionFixtureDB(t)
	for _, id := range []string{"", "a/b", `a\b`} {
		if _, err := readSessionChildrenSQLite(id); err == nil {
			t.Errorf("expected error for session id %q", id)
		}
	}
}

func TestReadSessionChildrenSQLite_MissingDB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	if _, err := readSessionChildrenSQLite("ses_parent"); err == nil {
		t.Fatal("expected error when db is missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode", "opencode.db")); !os.IsNotExist(err) {
		t.Fatalf("db file should not have been created, stat err = %v", err)
	}
}

func TestReadSessionChildren_FS(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	global := filepath.Join(data, "opencode", "storage", "session", "global")

	// Two children of ses_parent, written out of created order.
	writeJSON(t, filepath.Join(global, "ses_child_b.json"),
		`{"id":"ses_child_b","parentID":"ses_parent","title":"Child B","time":{"created":2000,"updated":2000}}`)
	writeJSON(t, filepath.Join(global, "ses_child_a.json"),
		`{"id":"ses_child_a","parentID":"ses_parent","title":"Child A","time":{"created":1500,"updated":1500}}`)
	// A session with a different parent that must be filtered out.
	writeJSON(t, filepath.Join(global, "ses_other.json"),
		`{"id":"ses_other","parentID":"ses_elsewhere","title":"Other","time":{"created":1600,"updated":1600}}`)
	// A root session (no parentID) that must be filtered out.
	writeJSON(t, filepath.Join(global, "ses_root.json"),
		`{"id":"ses_root","title":"Root","time":{"created":900,"updated":900}}`)
	// A malformed file that must be skipped, not error the whole call.
	writeJSON(t, filepath.Join(global, "ses_bad.json"), `[]`)

	children, err := readSessionChildren("ses_parent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d: %+v", len(children), children)
	}
	// Ordered by Created: child_a (1500) before child_b (2000).
	if children[0].SessionID != "ses_child_a" {
		t.Errorf("first child = %q, want ses_child_a", children[0].SessionID)
	}
	if children[1].SessionID != "ses_child_b" {
		t.Errorf("second child = %q, want ses_child_b", children[1].SessionID)
	}
	if children[0].ParentID != "ses_parent" {
		t.Errorf("child_a ParentID = %q, want ses_parent", children[0].ParentID)
	}
	if children[0].Title != "Child A" {
		t.Errorf("child_a Title = %q, want Child A", children[0].Title)
	}
	if children[0].Created != 1500 {
		t.Errorf("child_a Created = %d, want 1500", children[0].Created)
	}
}

func TestReadSessionChildren_FS_NoChildren(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	global := filepath.Join(data, "opencode", "storage", "session", "global")
	// A present dir with a session that has a different parent.
	writeJSON(t, filepath.Join(global, "ses_other.json"),
		`{"id":"ses_other","parentID":"ses_elsewhere","title":"Other","time":{"created":100}}`)

	children, err := readSessionChildren("ses_parent")
	if err != nil {
		t.Fatalf("expected nil error for present dir with zero matching children, got %v", err)
	}
	if children == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(children) != 0 {
		t.Fatalf("expected 0 children, got %d", len(children))
	}
}

func TestReadSessionChildren_FS_MissingDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := readSessionChildren("ses_parent"); err == nil {
		t.Fatal("expected error when session/global dir is missing")
	}
}

func TestReadSessionChildren_FS_InvalidSessionID(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	for _, id := range []string{"", "a/b", `a\b`} {
		if _, err := readSessionChildren(id); err == nil {
			t.Errorf("expected error for session id %q", id)
		}
	}
}
