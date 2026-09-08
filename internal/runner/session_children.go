package runner

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

// Discover a parent OpenCode session's child (subagent) sessions.
//
// A subagent runs in its own OpenCode session whose parent_id points at the
// hosting session. This linkage is what lets the dashboard drill into a
// subagent's transcript recursively. The transcript itself is already readable
// by session id (readSessionHistory / readSessionHistorySQLite); these readers
// only enumerate the parent->child edges.
//
// SQLite is the primary source on OpenCode >=1.x (the session table's
// parent_id column, indexed by session_parent_idx). The filesystem fallback
// reads the per-session JSON under storage/session/global for dead instances
// or older layouts.

// SessionChild is one child (subagent) session of a parent session.
type SessionChild struct {
	SessionID string `json:"session_id"`
	ParentID  string `json:"parent_id"`
	Title     string `json:"title,omitempty"`
	Created   int64  `json:"created,omitempty"`
	Agent     string `json:"agent,omitempty"`
}

// readSessionChildrenSQLite lists a session's child sessions from OpenCode's
// SQLite database. A parent with zero children returns an empty slice (not an
// error); only validation, DB-open, and query failures produce an error.
func readSessionChildrenSQLite(sessionID string) ([]SessionChild, error) {
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

	rows, err := db.Query(
		`SELECT id, parent_id, title, time_created, agent FROM session WHERE parent_id = ? ORDER BY time_created, id`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("query session children: %w", err)
	}
	defer func() { _ = rows.Close() }()

	children := []SessionChild{}
	for rows.Next() {
		var (
			id       string
			parentID sql.NullString
			title    sql.NullString
			created  sql.NullInt64
			agent    sql.NullString
		)
		// title/agent may be NULL on some rows or older schemas, and
		// time_created is defensively scanned as nullable too; coalesce so a
		// NULL never breaks the scan.
		if err := rows.Scan(&id, &parentID, &title, &created, &agent); err != nil {
			return nil, fmt.Errorf("scan session children: %w", err)
		}
		children = append(children, SessionChild{
			SessionID: id,
			ParentID:  parentID.String,
			Title:     title.String,
			Created:   created.Int64,
			Agent:     agent.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read session children: %w", err)
	}
	return children, nil
}

// readSessionChildren lists a session's child sessions from OpenCode's on-disk
// per-session JSON (storage/session/global/<id>.json), for dead instances or
// pre-SQLite layouts. Files that don't decode into the expected object shape
// are skipped rather than failing the whole call. A present dir with zero
// matching children returns an empty slice; a missing dir returns an error.
func readSessionChildren(sessionID string) ([]SessionChild, error) {
	if strings.ContainsAny(sessionID, "/\\") || sessionID == "" {
		return nil, fmt.Errorf("invalid session id %q", sessionID)
	}
	storage, err := opencodeStorageDir()
	if err != nil {
		return nil, err
	}

	globalDir := filepath.Join(storage, "session", "global")
	entries, err := os.ReadDir(globalDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session storage not found on this runner")
		}
		return nil, fmt.Errorf("read session storage: %w", err)
	}

	children := []SessionChild{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(globalDir, e.Name()))
		if err != nil {
			continue
		}
		// OpenCode FS session JSON is an object with camelCase parentID and a
		// nested time. Files with a different top-level shape (e.g. a stale
		// "[]") fail to decode into this struct and are skipped.
		var meta struct {
			ID       string `json:"id"`
			ParentID string `json:"parentID"`
			Title    string `json:"title"`
			Time     struct {
				Created int64 `json:"created"`
			} `json:"time"`
		}
		if err := json.Unmarshal(raw, &meta); err != nil {
			continue
		}
		if meta.ParentID != sessionID {
			continue
		}
		id := meta.ID
		if id == "" {
			id = strings.TrimSuffix(e.Name(), ".json")
		}
		children = append(children, SessionChild{
			SessionID: id,
			ParentID:  meta.ParentID,
			Title:     meta.Title,
			Created:   meta.Time.Created,
			// Agent is not present in FS session JSON; leave empty.
		})
	}

	sort.SliceStable(children, func(i, j int) bool {
		if children[i].Created != children[j].Created {
			return children[i].Created < children[j].Created
		}
		return children[i].SessionID < children[j].SessionID
	})
	return children, nil
}

// sessionChildNode is one node in the child-session tree returned over the
// bridge. It embeds SessionChild (the edge to the parent) and carries any
// nested descendants when the caller asked for a recursive walk. Children is
// omitted for a flat (non-recursive) listing and for leaf nodes so the JSON
// shape stays minimal.
type sessionChildNode struct {
	SessionChild
	Children []sessionChildNode `json:"children,omitempty"`
}

// childrenOf enumerates a session's direct children from persisted linkage,
// preferring SQLite (the primary source on OpenCode >=1.x) and falling back
// to the on-disk per-session JSON. A successful reader that returns an empty
// slice is a leaf, NOT an error. Only when BOTH readers fail is an error
// returned — the SQLite one, which is the better-shaped of the two.
//
// There is deliberately no live-port probe here: children come from persisted
// parent_id linkage, so the discovery works for dead instances too and never
// needs a running OpenCode server.
func (bc *BridgeClient) childrenOf(sessionID string) ([]SessionChild, error) {
	children, sqliteErr := readSessionChildrenSQLite(sessionID)
	if sqliteErr == nil {
		return children, nil
	}
	// SQLite failed (e.g. DB missing) — fall back to the on-disk JSON.
	if children, fsErr := readSessionChildren(sessionID); fsErr == nil {
		return children, nil
	}
	// Both readers failed; surface the better-shaped SQLite error.
	return nil, sqliteErr
}

// fetchSessionChildren returns a session's child (subagent) sessions as JSON.
// With recursive=false (or depth<=0) it returns the direct children as a flat
// array; with recursive=true it returns a nested tree walked up to depth
// levels. The empty result marshals as [] (never null), matching the history
// handler's convention.
func (bc *BridgeClient) fetchSessionChildren(sessionID string, recursive bool, depth int) ([]byte, error) {
	tree, err := buildChildrenTree(sessionID, recursive, depth, bc.childrenOf)
	if err != nil {
		return nil, err
	}
	return json.Marshal(tree)
}

// buildChildrenTree walks the child-session graph starting at root. childrenOf
// supplies a node's direct children (its contract: a leaf returns an empty
// slice, not an error). When recursive is false, or depth<=0 in the flat case,
// only the direct children are returned with no nested Children. When
// recursive is true the tree is descended up to depth levels; depth<=0 is
// treated as a small default cap so "recursive" still yields a real tree.
//
// A visited-set of the ancestor path guards against cyclic/pathological
// linkage: a child whose id already appears on the path back to the root is
// pruned rather than followed, so the walk always terminates.
//
// The returned slice is always non-nil so it marshals as [] for a leaf.
func buildChildrenTree(root string, recursive bool, depth int, childrenOf func(string) ([]SessionChild, error)) ([]sessionChildNode, error) {
	maxDepth := 1
	if recursive {
		if depth <= 0 {
			// recursive with no bound: cap the descent so a deep/broad tree
			// can't blow up unboundedly. The ADR default is a small cap.
			maxDepth = 5
		} else {
			maxDepth = depth
		}
	}

	var walk func(id string, level int, path map[string]bool) ([]sessionChildNode, error)
	walk = func(id string, level int, path map[string]bool) ([]sessionChildNode, error) {
		kids, err := childrenOf(id)
		if err != nil {
			return nil, err
		}
		nodes := make([]sessionChildNode, 0, len(kids))
		for _, k := range kids {
			// Cycle guard: never follow an edge back into an ancestor.
			if path[k.SessionID] {
				continue
			}
			node := sessionChildNode{SessionChild: k}
			if recursive && level+1 < maxDepth {
				path[k.SessionID] = true
				grand, err := walk(k.SessionID, level+1, path)
				delete(path, k.SessionID)
				if err != nil {
					return nil, err
				}
				if len(grand) > 0 {
					node.Children = grand
				}
			}
			nodes = append(nodes, node)
		}
		return nodes, nil
	}

	return walk(root, 0, map[string]bool{root: true})
}
