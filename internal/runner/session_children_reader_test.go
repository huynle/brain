package runner

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// These tests cover the *real*-reader paths that the pure-builder tests in
// bridge_children_test.go don't: (bc *BridgeClient).childrenOf's SQLite-then-FS
// fallback precedence, and (bc *BridgeClient).fetchSessionChildren driven by
// those real readers (not a closure). Reverting session_children.go's
// childrenOf/fetchSessionChildren (or the readers they call) would fail these.
//
// childrenOf/fetchSessionChildren touch no TaskRunner state — they only call
// the package-level readers — so a zero-value &BridgeClient{} is a sufficient
// receiver, matching how the readers resolve their paths from XDG_DATA_HOME.

// TestFetchSessionChildren_RealSQLite_RecursiveTree builds a real 3-level tree
// (parent -> child -> grandchild = a subagent-of-a-subagent) in a fixture
// SQLite DB and asserts fetchSessionChildren reconstructs the recursive tree
// with the grandchild nested under the child. This is the "reconstructable
// into a recursive tree, including subagents-of-subagents" requirement, fed by
// the real readSessionChildrenSQLite reader rather than an in-memory map.
func TestFetchSessionChildren_RealSQLite_RecursiveTree(t *testing.T) {
	db := newSessionFixtureDB(t)

	// parent -> child -> grandchild.
	insertSession(t, db, "ses_parent", "", "Parent", 1000, "build")
	insertSession(t, db, "ses_child", "ses_parent", "Child (subagent)", 1500, "explore")
	insertSession(t, db, "ses_grand", "ses_child", "Grandchild (sub-subagent)", 1800, "tdd-dev")
	// A sibling under a different parent that must not leak into the tree.
	insertSession(t, db, "ses_stray", "ses_elsewhere", "Stray", 1600, "build")

	bc := &BridgeClient{}
	raw, err := bc.fetchSessionChildren("ses_parent", true, 5)
	if err != nil {
		t.Fatalf("fetchSessionChildren: %v", err)
	}

	var tree []sessionChildNode
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("unmarshal children tree: %v\nraw=%s", err, raw)
	}

	if len(tree) != 1 {
		t.Fatalf("expected 1 direct child of parent, got %d: %s", len(tree), raw)
	}
	child := tree[0]
	if child.SessionID != "ses_child" {
		t.Fatalf("direct child = %q, want ses_child", child.SessionID)
	}
	if child.ParentID != "ses_parent" {
		t.Errorf("child ParentID = %q, want ses_parent", child.ParentID)
	}
	if child.Agent != "explore" || child.Title != "Child (subagent)" {
		t.Errorf("child fields not carried from SQLite: %+v", child)
	}

	// The subagent-of-a-subagent must be nested under the child.
	if len(child.Children) != 1 {
		t.Fatalf("expected 1 grandchild nested under child, got %d: %s", len(child.Children), raw)
	}
	grand := child.Children[0]
	if grand.SessionID != "ses_grand" {
		t.Errorf("grandchild = %q, want ses_grand", grand.SessionID)
	}
	if grand.ParentID != "ses_child" {
		t.Errorf("grandchild ParentID = %q, want ses_child", grand.ParentID)
	}
	// Grandchild is a leaf: no further nesting.
	if len(grand.Children) != 0 {
		t.Errorf("grandchild should be a leaf, got %+v", grand.Children)
	}
}

// TestFetchSessionChildren_RealSQLite_Leaf reconfirms via a real reader that a
// parent with no children marshals to a non-nil "[]" JSON array — the
// history-handler convention — not "null". The builder-level test already
// covers this; this proves the whole fetchSessionChildren path preserves it
// when childrenOf comes from real SQLite.
func TestFetchSessionChildren_RealSQLite_Leaf(t *testing.T) {
	db := newSessionFixtureDB(t)
	insertSession(t, db, "ses_lonely", "", "Lonely", 1000, "build")

	bc := &BridgeClient{}
	raw, err := bc.fetchSessionChildren("ses_lonely", true, 5)
	if err != nil {
		t.Fatalf("fetchSessionChildren: %v", err)
	}
	if string(raw) != "[]" {
		t.Errorf("leaf must marshal as [] (non-nil array), got %s", raw)
	}

	// And the flat (non-recursive) form is also [] for a leaf.
	rawFlat, err := bc.fetchSessionChildren("ses_lonely", false, 0)
	if err != nil {
		t.Fatalf("fetchSessionChildren flat: %v", err)
	}
	if string(rawFlat) != "[]" {
		t.Errorf("flat leaf must marshal as [], got %s", rawFlat)
	}
}

// TestChildrenOf_SQLitePreferredOverFS asserts the fallback precedence: when
// BOTH SQLite and the on-disk FS have children for the same parent, childrenOf
// returns the SQLite children (SQLite is primary on OpenCode >=1.x). The FS is
// seeded with a DIFFERENT child so we can prove which reader answered.
func TestChildrenOf_SQLitePreferredOverFS(t *testing.T) {
	// newSessionFixtureDB points XDG_DATA_HOME at a temp dir and creates the
	// opencode.db there; the FS reader resolves its global dir under the SAME
	// XDG_DATA_HOME, so seeding both exercises the real precedence.
	db := newSessionFixtureDB(t)
	insertSession(t, db, "ses_parent", "", "Parent", 1000, "build")
	insertSession(t, db, "ses_from_sqlite", "ses_parent", "From SQLite", 1500, "explore")

	// Seed the FS with a child that would only appear if the FS reader won.
	global := filepath.Join(xdgOpencodeGlobalDir(t))
	writeJSON(t, filepath.Join(global, "ses_from_fs.json"),
		`{"id":"ses_from_fs","parentID":"ses_parent","title":"From FS","time":{"created":1500,"updated":1500}}`)

	bc := &BridgeClient{}
	kids, err := bc.childrenOf("ses_parent")
	if err != nil {
		t.Fatalf("childrenOf: %v", err)
	}
	if len(kids) != 1 {
		t.Fatalf("expected exactly 1 child from the preferred reader, got %d: %+v", len(kids), kids)
	}
	if kids[0].SessionID != "ses_from_sqlite" {
		t.Errorf("childrenOf returned %q, want ses_from_sqlite (SQLite must win over FS)", kids[0].SessionID)
	}
}

// TestChildrenOf_FSFallbackWhenSQLiteMissing asserts the fallback: when the
// SQLite DB is absent (readSessionChildrenSQLite errors on the missing file),
// childrenOf falls back to the on-disk per-session JSON and returns the FS
// children. This is the dead-instance / pre-SQLite-layout path.
func TestChildrenOf_FSFallbackWhenSQLiteMissing(t *testing.T) {
	// No newSessionFixtureDB here: point XDG_DATA_HOME at a bare temp dir so
	// opencode.db does NOT exist, forcing the FS fallback.
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	global := filepath.Join(data, "opencode", "storage", "session", "global")

	writeJSON(t, filepath.Join(global, "ses_fs_child.json"),
		`{"id":"ses_fs_child","parentID":"ses_parent","title":"FS Child","time":{"created":1200,"updated":1200}}`)
	// A non-matching session that must be filtered out by the FS reader.
	writeJSON(t, filepath.Join(global, "ses_other.json"),
		`{"id":"ses_other","parentID":"ses_elsewhere","title":"Other","time":{"created":1300,"updated":1300}}`)

	bc := &BridgeClient{}
	kids, err := bc.childrenOf("ses_parent")
	if err != nil {
		t.Fatalf("childrenOf (FS fallback): %v", err)
	}
	if len(kids) != 1 {
		t.Fatalf("expected 1 FS child, got %d: %+v", len(kids), kids)
	}
	if kids[0].SessionID != "ses_fs_child" {
		t.Errorf("FS fallback returned %q, want ses_fs_child", kids[0].SessionID)
	}
	if kids[0].ParentID != "ses_parent" {
		t.Errorf("FS child ParentID = %q, want ses_parent", kids[0].ParentID)
	}
}

// TestChildrenOf_BothReadersFail asserts that when neither reader can answer
// (no SQLite db AND no session/global dir), childrenOf surfaces an error
// rather than an empty slice — the "both failed" branch. A missing dir is an
// error for the FS reader, and a missing db is an error for the SQLite reader.
func TestChildrenOf_BothReadersFail(t *testing.T) {
	// Bare XDG_DATA_HOME: neither opencode.db nor session/global exists.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	bc := &BridgeClient{}
	if _, err := bc.childrenOf("ses_parent"); err == nil {
		t.Fatal("expected error when both SQLite and FS readers fail")
	}
}

// xdgOpencodeGlobalDir returns the FS session/global dir under the current
// XDG_DATA_HOME, matching where readSessionChildren looks. It reuses the same
// layout the existing FS tests write into.
func xdgOpencodeGlobalDir(t *testing.T) string {
	t.Helper()
	storage, err := opencodeStorageDir()
	if err != nil {
		t.Fatalf("resolve storage dir: %v", err)
	}
	return filepath.Join(storage, "session", "global")
}
