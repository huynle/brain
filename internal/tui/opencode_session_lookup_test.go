package tui

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/huynle/brain-api/internal/types"
)

func TestLookupOpenCodeSessionIDForTaskMatchesPromptNearTaskTime(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	for _, stmt := range []string{
		`CREATE TABLE session (id text PRIMARY KEY, project_id text NOT NULL, parent_id text, slug text NOT NULL, directory text NOT NULL, title text NOT NULL, version text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL)`,
		`CREATE TABLE message (id text PRIMARY KEY, session_id text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`,
		`CREATE TABLE part (id text PRIMARY KEY, message_id text NOT NULL, session_id text NOT NULL, time_created integer NOT NULL, time_updated integer NOT NULL, data text NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec schema: %v", err)
		}
	}

	insertSessionPart(t, db, "ses_old", 1781190000000, "You are the Dream Consolidator old")
	insertSessionPart(t, db, "ses_match", 1781192326966, "You are the Dream Consolidator — an automated agent that periodically reads all knowledge in project {{.Project}}")
	insertSessionPart(t, db, "ses_other", 1781192327000, "Unrelated prompt")

	sessionID, err := lookupOpenCodeSessionIDForTask(types.BrainEntry{
		Created:      "2026-06-11T15:38:39Z",
		Modified:     "2026-06-11T15:41:54Z",
		DirectPrompt: "You are the Dream Consolidator — an automated agent that periodically reads all knowledge in project {{.Project}} and synthesizes it",
	}, dbPath)
	if err != nil {
		t.Fatalf("lookup session: %v", err)
	}
	if sessionID != "ses_match" {
		t.Fatalf("sessionID = %q, want ses_match", sessionID)
	}
}

func insertSessionPart(t *testing.T, db *sql.DB, sessionID string, created int64, text string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO session (id, project_id, slug, directory, title, version, time_created, time_updated) VALUES (?, 'global', 'slug', '/', 'title', '1', ?, ?)`, sessionID, created, created)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	messageID := sessionID + "_msg"
	_, err = db.Exec(`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, '{}')`, messageID, sessionID, created, created)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	data, err := json.Marshal(map[string]string{"type": "text", "text": text})
	if err != nil {
		t.Fatalf("marshal part: %v", err)
	}
	_, err = db.Exec(`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?)`, sessionID+"_part", messageID, sessionID, created, created, string(data))
	if err != nil {
		t.Fatalf("insert part: %v", err)
	}
}
