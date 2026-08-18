package runner

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// newFixtureDB creates <dir>/opencode/opencode.db with the OpenCode >=1.x
// schema and points XDG_DATA_HOME at dir so the reader finds it.
func newFixtureDB(t *testing.T) *sql.DB {
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
		`CREATE TABLE message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER, time_updated INTEGER, data TEXT NOT NULL)`,
		`CREATE TABLE part (id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL, time_created INTEGER, time_updated INTEGER, data TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestReadSessionHistorySQLite(t *testing.T) {
	db := newFixtureDB(t)

	// Insert out of chronological order: msg_b is older than msg_a, and each
	// message's second part is older than its first-inserted one, so correct
	// output order proves the ORDER BY.
	msgs := []struct {
		id      string
		created int64
		data    string
	}{
		{"msg_a", 2000, `{"id":"msg_a","role":"assistant"}`},
		{"msg_b", 1000, `{"id":"msg_b","role":"user"}`},
	}
	for _, m := range msgs {
		if _, err := db.Exec(
			`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
			m.id, "ses_1", m.created, m.created, m.data,
		); err != nil {
			t.Fatal(err)
		}
	}
	parts := []struct {
		id      string
		msgID   string
		created int64
		data    string
	}{
		{"prt_a2", "msg_a", 2200, `{"id":"prt_a2","type":"text"}`},
		{"prt_a1", "msg_a", 2100, `{"id":"prt_a1","type":"step-start"}`},
		{"prt_b2", "msg_b", 1200, `{"id":"prt_b2","type":"text"}`},
		{"prt_b1", "msg_b", 1100, `{"id":"prt_b1","type":"step-start"}`},
	}
	for _, p := range parts {
		if _, err := db.Exec(
			`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`,
			p.id, p.msgID, "ses_1", p.created, p.created, p.data,
		); err != nil {
			t.Fatal(err)
		}
	}
	// A second session that must not leak into ses_1's transcript.
	if _, err := db.Exec(
		`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
		"msg_other", "ses_2", 500, 500, `{"id":"msg_other"}`,
	); err != nil {
		t.Fatal(err)
	}

	body, err := readSessionHistorySQLite("ses_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var got []messageWithParts
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("output is not a JSON array of {info,parts}: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}

	infoID := func(raw json.RawMessage) string {
		var v struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("unmarshal info/part: %v", err)
		}
		return v.ID
	}
	if id := infoID(got[0].Info); id != "msg_b" {
		t.Errorf("first message = %q, want msg_b (oldest first)", id)
	}
	if id := infoID(got[1].Info); id != "msg_a" {
		t.Errorf("second message = %q, want msg_a", id)
	}
	wantParts := [][]string{{"prt_b1", "prt_b2"}, {"prt_a1", "prt_a2"}}
	for i, msg := range got {
		if len(msg.Parts) != 2 {
			t.Fatalf("message %d: expected 2 parts, got %d", i, len(msg.Parts))
		}
		for j, p := range msg.Parts {
			if id := infoID(p); id != wantParts[i][j] {
				t.Errorf("message %d part %d = %q, want %q", i, j, id, wantParts[i][j])
			}
		}
	}
}

func TestReadSessionHistorySQLiteNoParts(t *testing.T) {
	db := newFixtureDB(t)
	if _, err := db.Exec(
		`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?)`,
		"msg_1", "ses_1", 100, 100, `{"id":"msg_1"}`,
	); err != nil {
		t.Fatal(err)
	}

	body, err := readSessionHistorySQLite("ses_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// parts must serialize as [] not null, matching the legacy reader.
	var got []struct {
		Parts json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0].Parts) != "[]" {
		t.Errorf("expected parts to be [], got %s", body)
	}
}

func TestReadSessionHistorySQLiteUnknownSession(t *testing.T) {
	newFixtureDB(t)
	if _, err := readSessionHistorySQLite("ses_missing"); err == nil {
		t.Fatal("expected error for unknown session")
	}
}

func TestReadSessionHistorySQLiteInvalidSessionID(t *testing.T) {
	for _, id := range []string{"", "a/b", `a\b`} {
		if _, err := readSessionHistorySQLite(id); err == nil {
			t.Errorf("expected error for session id %q", id)
		}
	}
}

func TestReadSessionHistorySQLiteMissingDB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	if _, err := readSessionHistorySQLite("ses_1"); err == nil {
		t.Fatal("expected error when db is missing")
	}
	if _, err := os.Stat(filepath.Join(dir, "opencode", "opencode.db")); !os.IsNotExist(err) {
		t.Fatalf("db file should not have been created, stat err = %v", err)
	}
}

func TestSQLiteReadOnlyDSNDoesNotCreateDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "opencode.db")

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err == nil {
		if _, qerr := db.Query(`SELECT 1`); qerr == nil {
			t.Error("expected read-only open of a missing db to fail")
		}
		_ = db.Close()
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("mode=ro open created the db file, stat err = %v", err)
	}
}
