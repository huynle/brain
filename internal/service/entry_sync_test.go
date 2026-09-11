package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func TestEntrySyncPaginationWritesDeletesMovesAndFiles(t *testing.T) {
	s, store, dir := newTestBrainService(t)
	ctx := context.Background()
	saved := []*types.CreateEntryResponse{}
	for i := 0; i < 7; i++ {
		e, err := s.Save(ctx, types.CreateEntryRequest{Type: "note", Project: "sync", Title: fmt.Sprintf("Seed %d", i), Content: "body"})
		if err != nil {
			t.Fatal(err)
		}
		saved = append(saved, e)
	}
	cache := map[string]*types.BrainEntry{}
	cursor := int64(0)
	epoch := ""
	drain := func(limit int) int {
		t.Helper()
		n := 0
		for {
			p, err := s.EntryChanges(ctx, epoch, cursor, limit)
			if err != nil {
				t.Fatal(err)
			}
			epoch = p.Epoch
			cursor = p.Cursor
			for _, c := range p.Changes {
				n++
				if c.Deleted {
					delete(cache, c.Path)
				} else {
					cache[c.Path] = c.Entry
					if c.Entry.Revision == "" || c.Raw == "" {
						t.Fatal("missing offline edit inputs")
					}
				}
			}
			if !p.More {
				return n
			}
		}
	}
	if n := drain(2); n != 7 {
		t.Fatalf("bootstrap %d", n)
	}
	if n := drain(2); n != 0 {
		t.Fatalf("unchanged transferred %d", n)
	}
	first, _ := s.Recall(ctx, saved[0].Path)
	if cache[first.Path].Revision != first.Revision {
		t.Fatal("sync revision differs from API revision")
	}
	body := "new body"
	if _, err := s.Update(ctx, saved[0].Path, types.UpdateEntryRequest{Content: &body, ExpectedRevision: first.Revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateNote(ctx, saved[1].Path, map[string]interface{}{"metadata": `{"note":"runtime"}`}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, saved[2].Path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Move(ctx, saved[3].Path, "moved"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, saved[4].Path)
	raw, _ := os.ReadFile(file)
	raw = bytes.Replace(raw, []byte("body"), []byte("outside file edit"), 1)
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.indexer.IndexFile(saved[4].Path); err != nil {
		t.Fatal(err)
	}
	if n := drain(2); n != 6 {
		t.Fatalf("want six changed paths, got %d", n)
	}
	if len(cache) != 6 || cache[saved[0].Path].Content != "new body" || !strings.Contains(cache[saved[4].Path].Content, "outside file edit") {
		t.Fatal("cache did not converge")
	}
	if _, err := s.EntryChanges(ctx, "another-database", cursor, 2); !errors.Is(err, storage.ErrSyncReset) {
		t.Fatal("missing epoch reset", err)
	}
}

func TestEntrySyncHTTPIdempotencyAndRawConflict(t *testing.T) {
	s, _, _ := newTestBrainService(t)
	router := api.NewRouter(config.Config{}, api.WithHandler(api.NewHandler(s)))
	post := func(body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(body)
		r := httptest.NewRequest("POST", "/api/v1/sync/entries", bytes.NewReader(b))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	mutation := map[string]any{"id": "operation-create-0001", "method": "POST", "body": map[string]any{"type": "task", "title": "Offline task", "content": "seed", "project": "sync", "status": "draft"}}
	a := post(mutation)
	b := post(mutation)
	if a.Code != 201 || a.Body.String() != b.Body.String() {
		t.Fatal(a.Code, a.Body.String(), b.Code, b.Body.String())
	}
	var created types.CreateEntryResponse
	_ = json.Unmarshal(a.Body.Bytes(), &created)
	list, _ := s.List(context.Background(), types.ListEntriesRequest{Project: "sync"})
	if len(list.Entries) != 1 {
		t.Fatal("duplicate create")
	}
	entry, _ := s.Recall(context.Background(), created.Path)
	raw, _ := s.RecallFull(context.Background(), created.Path)
	edit := map[string]any{"id": "operation-update-0001", "method": "PATCH", "path": created.Path, "revision": entry.Revision, "raw": strings.Replace(raw, "seed", "offline edit", 1)}
	a = post(edit)
	b = post(edit)
	if a.Code != 200 || a.Body.String() != b.Body.String() {
		t.Fatal(a.Code, a.Body.String(), b.Code, b.Body.String())
	}
	edit["id"] = "operation-update-0002"
	if w := post(edit); w.Code != 409 {
		t.Fatal("stale raw edit accepted", w.Code, w.Body.String())
	}
	mutation["body"] = map[string]any{"type": "note", "title": "different", "content": "different"}
	if w := post(mutation); w.Code != 409 {
		t.Fatal("reused id accepted", w.Code)
	}
}

func TestEntrySyncRejectsInvalidDefinitionsAndMissingPreconditions(t *testing.T) {
	s, _, _ := newTestBrainService(t)
	router := api.NewRouter(config.Config{}, api.WithHandler(api.NewHandler(s)))
	e, err := s.Save(context.Background(), types.CreateEntryRequest{Type: "task", Project: "sync", Title: "guarded", Content: "seed", Status: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	current, _ := s.Recall(context.Background(), e.Path)
	cases := []struct {
		revision string
		body     map[string]any
		want     int
	}{
		{"", map[string]any{"title": "no precondition"}, 400},
		{current.Revision, map[string]any{"status": "invented"}, 400},
		{current.Revision, map[string]any{"depends_on": []string{"missing-task"}}, 400},
	}
	for i, tc := range cases {
		data, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("invalid-definition-%d", i), "method": "PATCH", "path": e.Path, "revision": tc.revision, "body": tc.body})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sync/entries", bytes.NewReader(data)))
		if w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	after, _ := s.Recall(context.Background(), e.Path)
	if after.Revision != current.Revision {
		t.Fatal("invalid edit mutated entry")
	}
}

func TestSelectedSyncOnlyTransfersRequestedChanges(t *testing.T) {
	s, _, _ := newTestBrainService(t)
	ctx := context.Background()
	a, err := s.Save(ctx, types.CreateEntryRequest{Type: "note", Project: "selective", Title: "Opened", Content: "original"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Save(ctx, types.CreateEntryRequest{Type: "note", Project: "selective", Title: "Unused", Content: "must not download"})
	if err != nil {
		t.Fatal(err)
	}
	router := api.NewRouter(config.Config{}, api.WithHandler(api.NewHandler(s)))
	request := func(entries map[string]string) types.EntryChanges {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"entries": entries})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sync/entries/selected", bytes.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var result types.EntryChanges
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if p := request(map[string]string{}); p.Epoch == "" || len(p.Changes) != 0 {
		t.Fatal(p)
	}
	p := request(map[string]string{a.Path: ""})
	if len(p.Changes) != 1 || p.Changes[0].Raw == "" {
		t.Fatal(p)
	}
	rev := p.Changes[0].Entry.Revision
	if p = request(map[string]string{a.Path: rev}); len(p.Changes) != 0 {
		t.Fatal("unchanged payload transferred", p)
	}
	body := "changed"
	if _, err = s.Update(ctx, a.Path, types.UpdateEntryRequest{Content: &body}); err != nil {
		t.Fatal(err)
	}
	if p = request(map[string]string{a.Path: rev}); len(p.Changes) != 1 || p.Changes[0].Entry.Content != body {
		t.Fatal(p)
	}
	if err = s.Delete(ctx, a.Path); err != nil {
		t.Fatal(err)
	}
	if p = request(map[string]string{a.Path: rev}); len(p.Changes) != 1 || !p.Changes[0].Deleted {
		t.Fatal(p)
	}
	tooMany := map[string]string{}
	for i := 0; i < 251; i++ {
		tooMany[fmt.Sprintf("entry/%d.md", i)] = ""
	}
	data, _ := json.Marshal(map[string]any{"entries": tooMany})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/sync/entries/selected", bytes.NewReader(data)))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestEntryListPreviewLeavesFullReadIntact(t *testing.T) {
	s, _, _ := newTestBrainService(t)
	content := strings.Repeat("你好", 600)
	e, err := s.Save(context.Background(), types.CreateEntryRequest{Type: "note", Project: "preview", Title: "Long", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	router := api.NewRouter(config.Config{}, api.WithHandler(api.NewHandler(s)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/entries?project=preview&preview=true", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		Entries []types.BrainEntry `json:"entries"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Entries) != 1 || len([]rune(response.Entries[0].Content)) != 500 {
		t.Fatal("unbounded preview", len(response.Entries))
	}
	full, err := s.Recall(context.Background(), e.Path)
	if err != nil || full.Content != content {
		t.Fatal("preview altered stored content", err)
	}
}
