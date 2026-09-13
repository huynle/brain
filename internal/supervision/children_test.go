package supervision

import "testing"

func TestChildrenPaginationAndTreeChange(t *testing.T) {
	raw := []byte(`[{"session_id":"b","parent_id":"root","children":[{"session_id":"c","parent_id":"b"}]},{"session_id":"a","parent_id":"root"}]`)
	p, err := ChildrenPage(raw, "r/root", "", 2)
	if err != nil || len(p.Children) != 2 || !p.Truncated {
		t.Fatal(p, err)
	}
	if p.Children[0].SessionID != "a" || p.Children[0].State != "unknown" || p.Children[0].LastActivity != nil {
		t.Fatal(p)
	}
	q, err := ChildrenPage(raw, "r/root", p.NextCursor, 2)
	if err != nil || len(q.Children) != 1 || q.Children[0].ParentID != "b" {
		t.Fatal(q, err)
	}
	if _, err = ChildrenPage(raw, "other/root", p.NextCursor, 2); err == nil {
		t.Fatal("cross-scope cursor")
	}
	q, err = ChildrenPage([]byte(`[{"session_id":"new","parent_id":"root"}]`), "r/root", p.NextCursor, 2)
	if err != nil || !q.CursorExpired || len(q.Children) != 1 {
		t.Fatal(q, err)
	}
}
