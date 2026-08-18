package runner

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

// Read OpenCode >=1.x session transcripts from its SQLite database.
//
// OpenCode 1.x replaced the file-per-message storage layout with a single
// SQLite database at <dataHome>/opencode/opencode.db. The message and part
// tables hold, in their data columns, the exact JSON objects the HTTP API
// returns, so the transcript can be reassembled into the same
// GET /session/:id/message shape as readSessionHistory produces.

// sessionRow is one (key, JSON data) row from the message or part table.
type sessionRow struct {
	id   string
	data string
}

// querySessionRows runs a two-column (id, data) query for one session and
// drains it, owning the rows' lifecycle so callers can't leak them.
func querySessionRows(db *sql.DB, query, sessionID, what string) ([]sessionRow, error) {
	rows, err := db.Query(query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", what, err)
	}
	defer func() { _ = rows.Close() }()

	out := []sessionRow{}
	for rows.Next() {
		var r sessionRow
		if err := rows.Scan(&r.id, &r.data); err != nil {
			return nil, fmt.Errorf("scan %s: %w", what, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", what, err)
	}
	return out, nil
}

// opencodeDBPath returns the path to OpenCode's SQLite database.
func opencodeDBPath() (string, error) {
	dataDir, err := opencodeDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "opencode.db"), nil
}

// readSessionHistorySQLite assembles a session's transcript from OpenCode's
// SQLite database, shaped identically to readSessionHistory's output.
func readSessionHistorySQLite(sessionID string) ([]byte, error) {
	if strings.ContainsAny(sessionID, "/\\") || sessionID == "" {
		return nil, fmt.Errorf("invalid session id %q", sessionID)
	}
	dbPath, err := opencodeDBPath()
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("opencode db: %w", err)
	}
	// mode=ro so a concurrent OpenCode writer is never blocked and nothing is
	// ever created or mutated on this path.
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open opencode db: %w", err)
	}
	defer func() { _ = db.Close() }()

	msgs, err := querySessionRows(db,
		`SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created, id`,
		sessionID, "messages")
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("session %s not found on this runner", sessionID)
	}

	parts, err := querySessionRows(db,
		`SELECT message_id, data FROM part WHERE session_id = ? ORDER BY time_created, id`,
		sessionID, "parts")
	if err != nil {
		return nil, err
	}
	partsByMsg := make(map[string][]json.RawMessage)
	for _, p := range parts {
		partsByMsg[p.id] = append(partsByMsg[p.id], json.RawMessage(p.data))
	}

	out := make([]messageWithParts, 0, len(msgs))
	for _, m := range msgs {
		parts := partsByMsg[m.id]
		if parts == nil {
			parts = []json.RawMessage{}
		}
		out = append(out, messageWithParts{Info: json.RawMessage(m.data), Parts: parts})
	}
	return json.Marshal(out)
}
