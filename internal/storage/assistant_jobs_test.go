package storage

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestRecoveryOwnershipAndAtomicInbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r := Record{Job: Job{ID: "job", Conversation: "chat", State: "running"}, Owner: "alice", Credential: "private", Messages: json.RawMessage(`[{"role":"assistant","content":"saved work"}]`)}
	if err = s.Create(r); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get("bob", "job"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-owner read allowed", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Recover(); err != nil {
		t.Fatal(err)
	}
	r, err = s.Get("alice", "job")
	if err != nil || r.State != "paused" || len(r.Messages) == 0 {
		t.Fatalf("recovery lost context: %+v %v", r.Job, err)
	}
	c := Conversation{ID: "chat", Title: "Saved chat", History: json.RawMessage(`[{"role":"assistant","content":"The job paused."}]`)}
	if err = s.CommitResponse("alice", c, map[string]int{"job": r.Revision}); err != nil {
		t.Fatal(err)
	}
	r, _ = s.Get("alice", "job")
	if r.Acknowledged != r.Revision {
		t.Fatal("inbox was not acknowledged")
	}
	conversations, err := s.Conversations("alice")
	if err != nil || len(conversations) != 1 || string(conversations[0].History) != string(c.History) {
		t.Fatal("delivery transcript missing", err)
	}
	if err = s.CommitResponse("bob", c, map[string]int{"job": r.Revision}); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-owner acknowledgement allowed", err)
	}
	bob, err := s.Conversations("bob")
	if err != nil || len(bob) != 0 {
		t.Fatal("failed transaction persisted transcript", err)
	}
	public, _ := json.Marshal(r.Job)
	if string(public) == "" {
		t.Fatal("empty public view")
	}
}
