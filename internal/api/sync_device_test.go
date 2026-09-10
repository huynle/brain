package api

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type deviceFake struct {
	BrainService
	devices  []types.SyncDevice
	raw, rev string
}

func (f *deviceFake) SyncDevices(context.Context) ([]types.SyncDevice, error) { return f.devices, nil }
func (f *deviceFake) SaveSyncDevice(_ context.Context, _ *types.SyncDevice, d types.SyncDevice) error {
	f.devices = []types.SyncDevice{d}
	return nil
}
func (f *deviceFake) SyncEntryVersion(context.Context, string) (string, string, error) {
	return f.raw, f.rev, nil
}
func TestSyncDeviceReconciliationGuards(t *testing.T) {
	f := &deviceFake{raw: "---\ntitle: server\n---\nserver", rev: "r2"}
	h := NewHandler(f)
	r := chi.NewRouter()
	r.Post("/{deviceID}/report", h.HandleSyncReport)
	r.Get("/", h.HandleSyncDevices)
	r.Get("/{deviceID}/operations/{operationID}/diff", h.HandleSyncDiff)
	r.Post("/{deviceID}/operations/{operationID}/reconcile", h.HandleSyncReconcile)
	call := func(method, path, body, owner string, want int) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(context.WithValue(req.Context(), ctxAuthResult, &AuthResult{Type: "jwt", Name: owner, Scope: "admin:*"}))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s got %d: %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	report := `{"reported_online":true,"ready":true,"pending":[{"id":"op","path":"projects/p/task/a.md","method":"PATCH","revision":"r1","raw":"---\ntitle: draft\n---\ndraft","error":"conflict","failure":"conflict"}]}`
	call("POST", "/device-1234567890/report", report, "browser", 200)
	call("POST", "/device-1234567890/report", report, "other", 403)
	var diff syncDiff
	_ = json.Unmarshal([]byte(call("GET", "/device-1234567890/operations/op/diff", "", "agent", 200)), &diff)
	if !strings.Contains(diff.Diff, "+draft") || diff.ServerRevision != "r2" {
		t.Fatal(diff)
	}
	resolve := func(snapshot, action string) string {
		b, _ := json.Marshal(map[string]string{"snapshot": snapshot, "action": action})
		return string(b)
	}
	call("POST", "/device-1234567890/operations/op/reconcile", resolve("old", "rebase"), "agent", 409)
	f.rev = "r3"
	call("POST", "/device-1234567890/operations/op/reconcile", resolve(diff.Snapshot, "rebase"), "agent", 409)
	f.rev = "r2"
	call("POST", "/device-1234567890/operations/op/reconcile", resolve(diff.Snapshot, "rebase"), "agent", 202)
	id := f.devices[0].Command.ID
	call("POST", "/device-1234567890/operations/op/reconcile", resolve(diff.Snapshot, "discard"), "agent", 409)
	call("POST", "/device-1234567890/report", report, "browser", 200)
	if f.devices[0].Command.ID != id {
		t.Fatal("report lost command")
	}
	var ack map[string]any
	_ = json.Unmarshal([]byte(report), &ack)
	ack["ack_id"] = id
	ack["ack_outcome"] = "applied_locally"
	b, _ := json.Marshal(ack)
	call("POST", "/device-1234567890/report", string(b), "browser", 200)
	if f.devices[0].Command.Outcome != "applied_locally" {
		t.Fatal("missing acknowledgement")
	}
	f.devices[0].LastSeen = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
	status := call("GET", "/", "", "agent", 200)
	if !strings.Contains(status, `"connection":"unknown"`) || strings.Contains(status, "expected_raw\":\"---") || strings.Contains(status, "owner") {
		t.Fatal(status)
	}
	f.devices[0].Pending[0].Failure = "uncertain"
	_ = json.Unmarshal([]byte(call("GET", "/device-1234567890/operations/op/diff", "", "agent", 200)), &diff)
	call("POST", "/device-1234567890/operations/op/reconcile", resolve(diff.Snapshot, "rebase"), "agent", 409)
	call("POST", "/device-1234567890/operations/op/reconcile", resolve(diff.Snapshot, "discard"), "agent", 202)
}
