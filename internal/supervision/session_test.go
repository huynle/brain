package supervision

import (
	"encoding/json"
	"strings"
	"testing"
)

func history(text string) []byte {
	b, _ := json.Marshal([]any{map[string]any{"info": map[string]any{"id": "m1", "role": "assistant"}, "parts": []any{
		map[string]any{"id": "p0", "type": "reasoning", "text": "hidden deliberation"},
		map[string]any{"id": "p1", "type": "text", "text": "visible"},
		map[string]any{"id": "p2", "type": "tool", "tool": "shell", "state": map[string]any{"status": "running", "input": map[string]any{"password": "hidden-input"}, "output": text}},
	}}})
	return b
}
func TestSessionTailPrivacyCursorAndStreaming(t *testing.T) {
	raw := history("Authorization: Bearer secret-value")
	p, err := SessionTail(raw, "r/s", "", 20, 16384)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	for _, secret := range []string{"secret-value", "hidden-input", "hidden deliberation"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("leaked %s", secret)
		}
	}
	if len(p.Records) != 2 || p.Records[1].Status != "running" {
		t.Fatal(p)
	}
	next, err := SessionTail(raw, "r/s", p.NextCursor, 20, 16384)
	if err != nil || len(next.Records) != 0 {
		t.Fatal(next, err)
	}
	next, err = SessionTail(history("changed output"), "r/s", p.NextCursor, 20, 16384)
	if err != nil || len(next.Records) != 1 || next.Records[0].ID != p.Records[1].ID {
		t.Fatal(next, err)
	}
	if _, err = SessionTail(raw, "other/session", p.NextCursor, 20, 16384); err == nil {
		t.Fatal("accepted cross-session cursor")
	}
}
func TestSessionTailBoundsAndExpiration(t *testing.T) {
	p, err := SessionTail(history(strings.Repeat("界", 100000)), "r/s", "", 1, 1024)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	if len(b) > 1024 || !p.Truncated || !p.Records[0].Truncated {
		t.Fatalf("bytes=%d page=%+v", len(b), p)
	}
	p, err = SessionTail(history("new"), "r/s", encodeCursor(sessionCursor{Scope: "r/s", ID: "missing", Hash: "x"}), 20, 16384)
	if err != nil || !p.CursorExpired {
		t.Fatal(p, err)
	}
}
