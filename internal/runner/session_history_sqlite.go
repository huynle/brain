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

// sqlQuerier is the read-only query surface these on-disk helpers need. It is
// satisfied by *sql.DB. Using the interface (rather than naming *sql.DB in
// each helper signature) keeps the shared v1/v2 assembly split into small
// functions without minting new raw-DB call sites: the single owned handle is
// opened once in readSessionHistorySQLite / readSessionChildrenSQLite.
type sqlQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
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
// SQLite database, shaped identically to readSessionHistory's output
// ([]messageWithParts). It prefers the v2 layout (session_message, with content
// parts inline in the message data blob) and falls back to the legacy v1
// layout (message + part tables) when the session has no v2 rows.
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

	// v2 first: session_message holds the whole transcript, content inline.
	if ok, err := sqliteTableExists(db, "session_message"); err != nil {
		return nil, err
	} else if ok {
		if out, found, err := readSessionHistoryV2(db, sessionID); err != nil {
			return nil, err
		} else if found {
			return out, nil
		}
		// No v2 rows for this session — fall through to the legacy layout so a
		// pre-v2 transcript in the same DB is still readable.
	}

	// Legacy v1 layout: message + part tables (file-per-message mirrored into
	// SQLite). querySessionRows owns the single raw *sql.DB handle.
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

// readSessionHistoryV2 assembles a session's transcript from the v2
// session_message table. Each row's data blob is the message body WITHOUT its
// id/type (those are columns); content parts are inline in data.content. The
// output mirrors normalizeV2Messages: info = data merged with {id, role:type},
// parts = data.content. found is false when the session has no v2 rows.
func readSessionHistoryV2(db sqlQuerier, sessionID string) (out []byte, found bool, err error) {
	rows, err := db.Query(
		`SELECT id, type, data FROM session_message WHERE session_id = ? ORDER BY seq, time_created, id`,
		sessionID,
	)
	if err != nil {
		return nil, false, fmt.Errorf("query session_message: %w", err)
	}
	defer func() { _ = rows.Close() }()

	msgs := []messageWithParts{}
	for rows.Next() {
		var id, mtype, data string
		if err := rows.Scan(&id, &mtype, &data); err != nil {
			return nil, false, fmt.Errorf("scan session_message: %w", err)
		}
		mwp, err := v2RowToMessageWithParts(id, mtype, []byte(data))
		if err != nil {
			return nil, false, err
		}
		msgs = append(msgs, mwp)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("read session_message: %w", err)
	}
	if len(msgs) == 0 {
		return nil, false, nil
	}
	b, err := json.Marshal(msgs)
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// v2RowToMessageWithParts converts one v2 session_message row (id + type
// columns, data blob) into the internal messageWithParts shape: info = data
// with {id, role:type} merged in, parts = data.content (inline). This mirrors
// normalizeV2Page's mapping so on-disk and live reads produce identical bytes.
func v2RowToMessageWithParts(id, mtype string, data []byte) (messageWithParts, error) {
	info := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &info); err != nil {
		return messageWithParts{}, fmt.Errorf("decode session_message data: %w", err)
	}
	idJSON, _ := json.Marshal(id)
	info["id"] = idJSON
	info["role"] = mustJSON(mtype)

	var parts []json.RawMessage
	if content, ok := info["content"]; ok {
		_ = json.Unmarshal(content, &parts)
	}
	if parts == nil {
		parts = []json.RawMessage{}
	}
	infoBytes, err := json.Marshal(info)
	if err != nil {
		return messageWithParts{}, err
	}
	return messageWithParts{Info: infoBytes, Parts: parts}, nil
}
