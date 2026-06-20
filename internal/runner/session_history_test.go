package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadSessionHistory_AssemblesOrderedTranscript(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	storage := filepath.Join(data, "opencode", "storage")

	sid := "ses_abc"
	// Two messages, written out of chronological order on disk.
	writeJSON(t, filepath.Join(storage, "message", sid, "msg_2.json"),
		`{"id":"msg_2","sessionID":"ses_abc","role":"assistant","time":{"created":200}}`)
	writeJSON(t, filepath.Join(storage, "message", sid, "msg_1.json"),
		`{"id":"msg_1","sessionID":"ses_abc","role":"user","time":{"created":100}}`)
	// Parts for msg_1.
	writeJSON(t, filepath.Join(storage, "part", "msg_1", "prt_1.json"),
		`{"id":"prt_1","type":"text","text":"hello"}`)
	// Parts for msg_2.
	writeJSON(t, filepath.Join(storage, "part", "msg_2", "prt_1.json"),
		`{"id":"prt_1","type":"text","text":"hi there"}`)

	out, err := readSessionHistory(sid)
	if err != nil {
		t.Fatalf("readSessionHistory: %v", err)
	}

	var msgs []struct {
		Info struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"info"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal(out, &msgs); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	// Oldest first by time.created.
	if msgs[0].Info.ID != "msg_1" || msgs[1].Info.ID != "msg_2" {
		t.Errorf("messages out of order: %s, %s", msgs[0].Info.ID, msgs[1].Info.ID)
	}
	if len(msgs[0].Parts) != 1 || msgs[0].Parts[0].Text != "hello" {
		t.Errorf("msg_1 parts wrong: %+v", msgs[0].Parts)
	}
}

func TestReadSessionHistory_MissingSession(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := readSessionHistory("ses_nope"); err == nil {
		t.Error("expected error for missing session")
	}
}

func TestReadSessionHistory_RejectsPathTraversal(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := readSessionHistory("../../etc"); err == nil {
		t.Error("expected error for path traversal in session id")
	}
}
