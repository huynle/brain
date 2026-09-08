package runner

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/bridge"
)

// TestBridgeChildren_EndToEnd exercises the full runner-side FrameChildren
// path: a real Hub sends FrameChildren over a real websocket to a real
// BridgeClient, whose handleChildren dispatch calls fetchSessionChildren,
// which reads a fixture OpenCode SQLite DB and returns the child-session tree.
//
// This mirrors TestBridge_EndToEnd (the existing proxied-GET round-trip) but
// for children instead of a proxied request, and reuses newBridgeTestEnv +
// the SQLite fixture helpers (newSessionFixtureDB/insertSession). Reverting
// the handleChildren case in bridge_client.go's handleFrame, or
// fetchSessionChildren, would fail this test.
func TestBridgeChildren_EndToEnd(t *testing.T) {
	// Seed a fixture OpenCode DB FIRST so XDG_DATA_HOME is set before the
	// runner (which reads children from that same env-resolved path) starts.
	db := newSessionFixtureDB(t)
	insertSession(t, db, "ses_parent", "", "Parent", 1000, "build")
	insertSession(t, db, "ses_child", "ses_parent", "Child (subagent)", 1500, "explore")
	insertSession(t, db, "ses_grand", "ses_child", "Grandchild (sub-subagent)", 1800, "tdd-dev")

	_, opencodePort := fakeOpencode(t)
	hub, _, tr, runnerID, _ := newBridgeTestEnv(t, opencodePort)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bc := NewBridgeClient(tr)
	tr.setBridgeClient(bc)
	go bc.Start(ctx)

	waitFor(t, 5*time.Second, func() bool { return hub.Connected(runnerID) }, "bridge connection")

	// Recursive fetch: the grandchild must nest under the child.
	raw, err := hub.FetchChildren(ctx, runnerID, "ses_parent", true, 5)
	if err != nil {
		t.Fatalf("FetchChildren: %v", err)
	}

	var tree []sessionChildNode
	if err := json.Unmarshal(raw, &tree); err != nil {
		t.Fatalf("unmarshal children tree: %v\nraw=%s", err, raw)
	}
	if len(tree) != 1 || tree[0].SessionID != "ses_child" {
		t.Fatalf("expected [ses_child] as direct children, got %s", raw)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].SessionID != "ses_grand" {
		t.Fatalf("expected grandchild ses_grand nested under child, got %s", raw)
	}
	if tree[0].Children[0].ParentID != "ses_child" {
		t.Errorf("grandchild ParentID = %q, want ses_child", tree[0].Children[0].ParentID)
	}

	// A leaf session round-trips as a non-nil [] over the bridge.
	rawLeaf, err := hub.FetchChildren(ctx, runnerID, "ses_grand", true, 5)
	if err != nil {
		t.Fatalf("FetchChildren(leaf): %v", err)
	}
	if string(rawLeaf) != "[]" {
		t.Errorf("leaf children = %s, want []", rawLeaf)
	}

	// A FrameChildren for a parent whose DB read fails would surface an error;
	// here the DB exists, so an unknown session id is simply a leaf, not an
	// error — assert that empty-not-error contract holds over the bridge too.
	rawUnknown, err := hub.FetchChildren(ctx, runnerID, "ses_does_not_exist", false, 0)
	if err != nil {
		t.Fatalf("FetchChildren(unknown) should be empty-not-error, got %v", err)
	}
	if string(rawUnknown) != "[]" {
		t.Errorf("unknown session children = %s, want []", rawUnknown)
	}

	// Silence the unused-import guard if bridge is not otherwise referenced.
	_ = bridge.FrameChildren
}
