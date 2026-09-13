package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestSupervisorOperationLostAcknowledgmentAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	owner, err := storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := owner.ForTenant(tenant.Local)
	bridge := &mockBridgeService{doErr: errors.New("lost acknowledgment")}
	h := &Handler{supervisorOperations: store, bridge: bridge, executionBudgets: store}
	ctx := tenant.Into(context.Background(), tenant.Local)
	if ok, err := store.ConfigureExecutionBudget(ctx, types.ExecutionBudget{ID: "calls", Project: "p", Timezone: "UTC", Unit: "calls", Limit: 1}, 0); err != nil || !ok {
		t.Fatal(ok, err)
	}
	payload := `{"id":"operation-1","operation":"prompt","project":"p","runner_id":"r","instance_id":"i","session_id":"s","text":"hello","budget_id":"calls","budget_units":1}`
	call := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/supervision/operations", strings.NewReader(body)).WithContext(ctx)
		h.HandleSupervisorOperation(w, r)
		return w
	}
	w := call(payload)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "outcome_unknown") {
		t.Fatal(w.Code, w.Body)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	owner, err = storage.New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, _ = owner.ForTenant(tenant.Local)
	h.supervisorOperations = store
	h.executionBudgets = store
	w = call(payload)
	if w.Code != 200 || len(bridge.doCalls) != 1 {
		t.Fatal("replayed after restart", w.Code, bridge.doCalls)
	}
	w = call(strings.Replace(payload, "hello", "different", 1))
	if w.Code != 409 {
		t.Fatal("payload conflict", w.Code)
	}
	w = call(strings.Replace(payload, "operation-1", "operation-2", 1))
	if w.Code != 409 || len(bridge.doCalls) != 1 {
		t.Fatal("exhausted budget dispatched", w.Code, bridge.doCalls)
	}
}
func TestSupervisorCheckpointArtifactAndRevision(t *testing.T) {
	owner, err := storage.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	store, _ := owner.ForTenant(tenant.Local)
	h := &Handler{supervisorCheckpoints: store}
	ctx := tenant.Into(context.Background(), tenant.Local)
	call := func(action string, rev int, artifact, answer string) *httptest.ResponseRecorder {
		b, _ := json.Marshal(map[string]any{"action": action, "expected_revision": rev, "checkpoint": map[string]any{"id": "checkpoint-1", "project": "p", "artifact": artifact, "question": "Finished PDF readable?", "answer": answer}})
		w := httptest.NewRecorder()
		h.HandleSupervisorCheckpoints(w, httptest.NewRequest("POST", "/supervision/checkpoints", strings.NewReader(string(b))).WithContext(ctx))
		return w
	}
	for _, w := range []*httptest.ResponseRecorder{call("request", 0, "finished-sha", ""), call("request", 0, "finished-sha", ""), call("answer", 1, "finished-sha", "yes")} {
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if w := call("answer", 2, "synthetic-sha", "yes"); w.Code != 409 {
		t.Fatal("unrelated artifact accepted", w.Code)
	}
	if w := call("supersede", 2, "new-finished-sha", ""); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if w := call("answer", 2, "finished-sha", "old answer"); w.Code != 409 {
		t.Fatal("stale answer accepted", w.Code)
	}
	current, err := store.SupervisorCheckpoint(ctx, "p", "checkpoint-1")
	if err != nil || current.State != "pending" || current.Answer != "" {
		t.Fatal(current, err)
	}
}
