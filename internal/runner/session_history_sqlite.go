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
	defer db.Close()

	type msgRow struct {
		id   string
		data string
	}
	msgRows, err := db.Query(
		`SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created, id`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	msgs := []msgRow{}
	for msgRows.Next() {
		var m msgRow
		if err := msgRows.Scan(&m.id, &m.data); err != nil {
			msgRows.Close()
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := msgRows.Err(); err != nil {
		msgRows.Close()
		return nil, fmt.Errorf("read messages: %w", err)
	}
	msgRows.Close()
	if len(msgs) == 0 {
		return nil, fmt.Errorf("session %s not found on this runner", sessionID)
	}

	partsByMsg := make(map[string][]json.RawMessage)
	partRows, err := db.Query(
		`SELECT message_id, data FROM part WHERE session_id = ? ORDER BY time_created, id`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query parts: %w", err)
	}
	for partRows.Next() {
		var msgID, data string
		if err := partRows.Scan(&msgID, &data); err != nil {
			partRows.Close()
			return nil, fmt.Errorf("scan part: %w", err)
		}
		partsByMsg[msgID] = append(partsByMsg[msgID], json.RawMessage(data))
	}
	if err := partRows.Err(); err != nil {
		partRows.Close()
		return nil, fmt.Errorf("read parts: %w", err)
	}
	partRows.Close()

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
