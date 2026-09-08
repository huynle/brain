package runner

import (
	"encoding/json"
	"fmt"
	"testing"
)

// closureChildrenOf builds a childrenOf function from a static parent->children
// map, mirroring readSessionChildren's contract: an unknown/leaf parent
// returns an empty slice (never an error).
func closureChildrenOf(edges map[string][]SessionChild) func(string) ([]SessionChild, error) {
	return func(id string) ([]SessionChild, error) {
		kids := edges[id]
		if kids == nil {
			return []SessionChild{}, nil
		}
		return kids, nil
	}
}

func TestFetchSessionChildren_FlatJSON(t *testing.T) {
	edges := map[string][]SessionChild{
		"ses_p": {
			{SessionID: "ses_c1", ParentID: "ses_p", Title: "Child 1", Created: 1000, Agent: "explore"},
			{SessionID: "ses_c2", ParentID: "ses_p", Title: "Child 2", Created: 2000, Agent: "build"},
		},
	}
	nodes, err := buildChildrenTree("ses_p", false, 0, closureChildrenOf(edges))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 direct children, got %d: %+v", len(nodes), nodes)
	}
	if nodes[0].SessionID != "ses_c1" || nodes[1].SessionID != "ses_c2" {
		t.Errorf("children order/ids wrong: %+v", nodes)
	}
	if nodes[0].ParentID != "ses_p" || nodes[0].Title != "Child 1" || nodes[0].Created != 1000 || nodes[0].Agent != "explore" {
		t.Errorf("child fields not carried through: %+v", nodes[0])
	}
	// Non-recursive must not emit a children key.
	b, err := json.Marshal(nodes)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); got == "" || containsKey(got, "children") {
		t.Errorf("flat JSON must omit children key, got %s", got)
	}
}

func TestFetchSessionChildren_Recursive(t *testing.T) {
	// P -> C1 -> C2 (grandchild).
	edges := map[string][]SessionChild{
		"ses_p":  {{SessionID: "ses_c1", ParentID: "ses_p", Title: "C1"}},
		"ses_c1": {{SessionID: "ses_c2", ParentID: "ses_c1", Title: "C2"}},
	}
	childrenOf := closureChildrenOf(edges)

	// depth=5: full nested tree.
	nodes, err := buildChildrenTree("ses_p", true, 5, childrenOf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodes) != 1 || nodes[0].SessionID != "ses_c1" {
		t.Fatalf("expected [C1], got %+v", nodes)
	}
	if len(nodes[0].Children) != 1 || nodes[0].Children[0].SessionID != "ses_c2" {
		t.Fatalf("expected C1.children=[C2], got %+v", nodes[0].Children)
	}
	if len(nodes[0].Children[0].Children) != 0 {
		t.Errorf("C2 should be a leaf, got %+v", nodes[0].Children[0].Children)
	}

	// depth=1: only direct children, no nested descent.
	shallow, err := buildChildrenTree("ses_p", true, 1, childrenOf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shallow) != 1 || shallow[0].SessionID != "ses_c1" {
		t.Fatalf("expected [C1], got %+v", shallow)
	}
	if len(shallow[0].Children) != 0 {
		t.Errorf("depth=1 must not descend into grandchildren, got %+v", shallow[0].Children)
	}
}

func TestFetchSessionChildren_CycleGuard(t *testing.T) {
	// A is parent of B, and B's child points back to A — a cycle.
	edges := map[string][]SessionChild{
		"ses_a": {{SessionID: "ses_b", ParentID: "ses_a"}},
		"ses_b": {{SessionID: "ses_a", ParentID: "ses_b"}},
	}
	// A generous depth that would loop forever without a visited-set guard.
	nodes, err := buildChildrenTree("ses_a", true, 100, closureChildrenOf(edges))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A -> B, then B -> A must be pruned because A is already on the path.
	if len(nodes) != 1 || nodes[0].SessionID != "ses_b" {
		t.Fatalf("expected [B], got %+v", nodes)
	}
	// B's child A is an ancestor -> pruned, so B is a leaf here.
	if len(nodes[0].Children) != 0 {
		t.Errorf("cycle back to ancestor A must be pruned, got %+v", nodes[0].Children)
	}
	// Also assert each id appears at most once along the single path.
	seen := map[string]int{"ses_a": 1} // root is on the path
	var walk func(ns []sessionChildNode)
	walk = func(ns []sessionChildNode) {
		for _, n := range ns {
			seen[n.SessionID]++
			if seen[n.SessionID] > 1 {
				t.Errorf("id %q appears more than once along a path", n.SessionID)
			}
			walk(n.Children)
		}
	}
	walk(nodes)
}

func TestFetchSessionChildren_NoChildren(t *testing.T) {
	// Leaf with no edges at all.
	nodes, err := buildChildrenTree("ses_leaf", true, 5, closureChildrenOf(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nodes == nil {
		t.Fatal("expected non-nil empty slice so JSON marshals as [], got nil")
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 children, got %d", len(nodes))
	}
	b, err := json.Marshal(nodes)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("empty tree must marshal as [], got %s", b)
	}
}

func TestFetchSessionChildren_ChildrenOfError(t *testing.T) {
	// If the root read fails, the error must surface.
	boom := fmt.Errorf("db missing")
	_, err := buildChildrenTree("ses_p", false, 0, func(string) ([]SessionChild, error) {
		return nil, boom
	})
	if err == nil {
		t.Fatal("expected error from a failing root read")
	}
}

// containsKey is a tiny JSON-string helper for the flat-shape assertion.
func containsKey(s, key string) bool {
	needle := `"` + key + `"`
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
