package runner

import (
	"encoding/json"
	"testing"
)

// These tests pin the decode of OpenCode's GET /session payload into
// opencodeSession, specifically that the camelCase `parentID` field
// round-trips into opencodeSession.ParentID.
//
// REGRESSION FRAMING: before the subagent session-content feature,
// opencodeSession had no ParentID field at all. OpenCode still sent
// `"parentID": "..."` on every subagent session, so the linkage a subagent
// carries to its host session was silently DROPPED at unmarshal time —
// json.Unmarshal ignores unknown object keys. Deleting the
//
//	ParentID string `json:"parentID,omitempty"`
//
// line from the opencodeSession struct in runner.go (~2266) makes
// TestOpencodeSession_ParentIDRoundTrips fail (ParentID would decode to ""),
// which is exactly the pre-feature behavior these tests guard against.

// TestOpencodeSession_ParentIDRoundTrips is the focused regression: a single
// subagent session object with a camelCase parentID must decode with that
// parent captured, alongside id and both time fields. This is the assertion
// that would FAIL if the ParentID field were removed from opencodeSession.
func TestOpencodeSession_ParentIDRoundTrips(t *testing.T) {
	// A realistic single subagent session object as OpenCode emits it.
	payload := []byte(`{
		"id": "ses_child456",
		"parentID": "ses_parent123",
		"title": "explore subagent",
		"time": {"created": 1700000000, "updated": 1700000500}
	}`)

	var s opencodeSession
	if err := json.Unmarshal(payload, &s); err != nil {
		t.Fatalf("unmarshal single session: %v", err)
	}

	// The whole point of the regression: parentID must be captured, not dropped.
	if s.ParentID != "ses_parent123" {
		t.Errorf("ParentID = %q, want ses_parent123 (parentID must round-trip; pre-feature it was dropped)", s.ParentID)
	}
	if s.ID != "ses_child456" {
		t.Errorf("ID = %q, want ses_child456", s.ID)
	}
	if s.Time.Created != 1700000000 {
		t.Errorf("Time.Created = %d, want 1700000000", s.Time.Created)
	}
	if s.Time.Updated != 1700000500 {
		t.Errorf("Time.Updated = %d, want 1700000500", s.Time.Updated)
	}
}

// TestOpencodeSession_RootHasNoParentID asserts a root session (one OpenCode
// emits with no parentID key) decodes to an empty ParentID rather than
// inventing a parent — the omitempty tag and the absence of the key must
// leave the zero value.
func TestOpencodeSession_RootHasNoParentID(t *testing.T) {
	payload := []byte(`{
		"id": "ses_root000",
		"title": "root session",
		"time": {"created": 1699999000, "updated": 1699999900}
	}`)

	var s opencodeSession
	if err := json.Unmarshal(payload, &s); err != nil {
		t.Fatalf("unmarshal root session: %v", err)
	}
	if s.ParentID != "" {
		t.Errorf("root ParentID = %q, want \"\" (no parentID key present)", s.ParentID)
	}
	if s.ID != "ses_root000" {
		t.Errorf("ID = %q, want ses_root000", s.ID)
	}
}

// TestOpencodeSession_ArrayFormCapturesParentIDs decodes the array form of
// GET /session (the shape fetchSessions tries first) and asserts every
// element's parentID is captured — a root with none, and two children whose
// parentID points back at the root.
func TestOpencodeSession_ArrayFormCapturesParentIDs(t *testing.T) {
	// Array form: [ {..}, {..} ] — exactly what fetchSessions unmarshals first.
	payload := []byte(`[
		{"id": "ses_root000", "title": "root", "time": {"created": 100, "updated": 100}},
		{"id": "ses_childA", "parentID": "ses_root000", "title": "A", "time": {"created": 200, "updated": 250}},
		{"id": "ses_childB", "parentID": "ses_root000", "title": "B", "time": {"created": 300, "updated": 300}}
	]`)

	var sessions []opencodeSession
	if err := json.Unmarshal(payload, &sessions); err != nil {
		t.Fatalf("unmarshal session array: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("decoded %d sessions, want 3: %+v", len(sessions), sessions)
	}

	byID := make(map[string]opencodeSession, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}

	root, ok := byID["ses_root000"]
	if !ok {
		t.Fatal("root session missing from decode")
	}
	if root.ParentID != "" {
		t.Errorf("root ParentID = %q, want \"\"", root.ParentID)
	}

	for _, id := range []string{"ses_childA", "ses_childB"} {
		child, ok := byID[id]
		if !ok {
			t.Fatalf("child %s missing from decode", id)
		}
		if child.ParentID != "ses_root000" {
			t.Errorf("%s ParentID = %q, want ses_root000", id, child.ParentID)
		}
	}

	// Spot-check that time fields also survived the array decode.
	if a := byID["ses_childA"]; a.Time.Created != 200 || a.Time.Updated != 250 {
		t.Errorf("childA time = %+v, want created=200 updated=250", a.Time)
	}
}

// TestOpencodeSession_SingleObjectFormCapturesParentID exercises the
// single-object fallback path of fetchSessions: when GET /session returns a
// bare object (not an array), fetchSessions decodes it into a single
// opencodeSession. The parentID must survive that decode too.
func TestOpencodeSession_SingleObjectFormCapturesParentID(t *testing.T) {
	payload := []byte(`{"id": "ses_solo", "parentID": "ses_host", "time": {"created": 42, "updated": 99}}`)

	// Mirror fetchSessions' logic: array first, then single object.
	var sessions []opencodeSession
	if err := json.Unmarshal(payload, &sessions); err == nil {
		t.Fatalf("single object unexpectedly decoded as array: %+v", sessions)
	}
	var single opencodeSession
	if err := json.Unmarshal(payload, &single); err != nil {
		t.Fatalf("unmarshal single object: %v", err)
	}
	if single.ParentID != "ses_host" {
		t.Errorf("ParentID = %q, want ses_host", single.ParentID)
	}
	if single.ID != "ses_solo" {
		t.Errorf("ID = %q, want ses_solo", single.ID)
	}
	if single.Time.Created != 42 || single.Time.Updated != 99 {
		t.Errorf("time = %+v, want created=42 updated=99", single.Time)
	}
}
