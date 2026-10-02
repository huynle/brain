package runner

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// v2 on-disk store: sessions live in session_v2 (not session) and messages in
// session_message (not message/part), with content parts inline in the data
// blob. These fixtures mirror the real opencode v2.0.18 schema (verified in
// task 1). The readers must work against v2 while still reading legacy v1.

// newV2FixtureDB creates <dir>/opencode/opencode.db with the v2 tables
// (session_v2 + session_message) and points XDG_DATA_HOME at dir.
func newV2FixtureDB(t *testing.T) *sql.DB {
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
	for _, stmt := range []string{
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, parent_id TEXT, title TEXT, time_created INTEGER, time_updated INTEGER, agent TEXT)`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, type TEXT NOT NULL, seq INTEGER NOT NULL, time_created INTEGER, time_updated INTEGER, data TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func insertV2Session(t *testing.T, db *sql.DB, id, parentID, title string, created int64, agent string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO session_v2 (id, parent_id, title, time_created, time_updated, agent) VALUES (?, ?, ?, ?, ?, ?)`,
		id, parentID, title, created, created, agent,
	); err != nil {
		t.Fatal(err)
	}
}

func insertV2Message(t *testing.T, db *sql.DB, id, sessionID, mtype string, seq, created int64, data string) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO session_message (id, session_id, type, seq, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, sessionID, mtype, seq, created, created, data,
	); err != nil {
		t.Fatal(err)
	}
}

// --- children: v2 store ---

func TestReadSessionChildrenSQLite_V2Store(t *testing.T) {
	db := newV2FixtureDB(t)
	insertV2Session(t, db, "ses_parent", "", "Parent", 1000, "build")
	insertV2Session(t, db, "ses_child_b", "ses_parent", "Child B", 3000, "tdd-dev")
	insertV2Session(t, db, "ses_child_a", "ses_parent", "Child A", 2000, "explore")
	insertV2Session(t, db, "ses_other", "ses_elsewhere", "Unrelated", 2500, "build")

	children, err := readSessionChildrenSQLite("ses_parent")
	if err != nil {
		t.Fatalf("readSessionChildrenSQLite: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("children = %d, want 2: %+v", len(children), children)
	}
	// Ordered by time_created: child_a (2000) then child_b (3000).
	if children[0].SessionID != "ses_child_a" || children[1].SessionID != "ses_child_b" {
		t.Fatalf("children order = %v, want [ses_child_a ses_child_b]", []string{children[0].SessionID, children[1].SessionID})
	}
	if children[0].Agent != "explore" || children[0].Title != "Child A" {
		t.Fatalf("child_a fields = %+v", children[0])
	}
}

// --- children: legacy v1 store still works ---

func TestReadSessionChildrenSQLite_LegacyStillWorks(t *testing.T) {
	db := newSessionFixtureDB(t) // legacy `session` table only
	insertSession(t, db, "ses_parent", "", "Parent", 1000, "build")
	insertSession(t, db, "ses_kid", "ses_parent", "Kid", 2000, "build")

	children, err := readSessionChildrenSQLite("ses_parent")
	if err != nil {
		t.Fatalf("readSessionChildrenSQLite (legacy): %v", err)
	}
	if len(children) != 1 || children[0].SessionID != "ses_kid" {
		t.Fatalf("legacy children = %+v, want [ses_kid]", children)
	}
}

// --- children: mixed store dedups a session present in both tables ---

func TestReadSessionChildrenSQLite_MixedDedup(t *testing.T) {
	db := newV2FixtureDB(t)
	// Add the legacy session table too, and put the SAME child in both.
	if _, err := db.Exec(`CREATE TABLE session (id TEXT PRIMARY KEY, parent_id TEXT, title TEXT, time_created INTEGER, time_updated INTEGER, agent TEXT)`); err != nil {
		t.Fatal(err)
	}
	insertV2Session(t, db, "ses_kid", "ses_parent", "Kid v2", 2000, "tdd-dev")
	if _, err := db.Exec(`INSERT INTO session (id, parent_id, title, time_created, time_updated, agent) VALUES (?, ?, ?, ?, ?, ?)`,
		"ses_kid", "ses_parent", "Kid v1", 2000, 2000, "build"); err != nil {
		t.Fatal(err)
	}
	children, err := readSessionChildrenSQLite("ses_parent")
	if err != nil {
		t.Fatalf("readSessionChildrenSQLite (mixed): %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("children = %d, want 1 (deduped): %+v", len(children), children)
	}
}

// --- history: v2 store, content inline in the message data blob ---

func TestReadSessionHistorySQLite_V2Store(t *testing.T) {
	db := newV2FixtureDB(t)
	// v2 message data embeds content inline; id/type are columns, not in data.
	insertV2Message(t, db, "msg_u", "ses_1", "user", 4, 1000, `{"time":{"created":1000},"text":"hi"}`)
	insertV2Message(t, db, "msg_a", "ses_1", "assistant", 7, 2000, `{"time":{"created":2000,"completed":3000},"agent":"build","content":[{"type":"text","text":"hello"}]}`)
	insertV2Message(t, db, "msg_i", "ses_1", "idle", 12, 4000, `{"time":{"created":4000},"outcome":"succeeded"}`)

	body, err := readSessionHistorySQLite("ses_1")
	if err != nil {
		t.Fatalf("readSessionHistorySQLite (v2): %v", err)
	}
	var msgs []messageWithParts
	if err := json.Unmarshal(body, &msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	// Ordered by seq: user(4), assistant(7), idle(12).
	var info0 struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	_ = json.Unmarshal(msgs[0].Info, &info0)
	if info0.ID != "msg_u" || info0.Role != "user" {
		t.Fatalf("msg[0] info = %+v, want id=msg_u role=user", info0)
	}
	// Assistant: role mapped from type, parts from inline content.
	var info1 struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	_ = json.Unmarshal(msgs[1].Info, &info1)
	if info1.Role != "assistant" {
		t.Fatalf("msg[1] role = %q, want assistant", info1.Role)
	}
	if len(msgs[1].Parts) != 1 {
		t.Fatalf("assistant parts = %d, want 1 (from inline content)", len(msgs[1].Parts))
	}
	var part struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(msgs[1].Parts[0], &part)
	if part.Type != "text" || part.Text != "hello" {
		t.Fatalf("part = %+v, want text/hello", part)
	}
}

// --- history: legacy v1 store still works ---

func TestReadSessionHistorySQLite_LegacyStillWorks(t *testing.T) {
	db := newFixtureDB(t) // legacy message + part tables
	if _, err := db.Exec(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
		"msg_a", "ses_1", 1000, 1000, `{"id":"msg_a","role":"assistant"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`,
		"prt_a", "msg_a", "ses_1", 1000, 1000, `{"id":"prt_a","type":"text","text":"legacy"}`); err != nil {
		t.Fatal(err)
	}
	body, err := readSessionHistorySQLite("ses_1")
	if err != nil {
		t.Fatalf("readSessionHistorySQLite (legacy): %v", err)
	}
	var msgs []messageWithParts
	if err := json.Unmarshal(body, &msgs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(msgs) != 1 || len(msgs[0].Parts) != 1 {
		t.Fatalf("legacy history = %+v", msgs)
	}
}
