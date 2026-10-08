package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
	"gopkg.in/yaml.v3"
)

func TestSDKCapabilitiesAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, token, scope string
		auth               bool
		mode               tenant.Mode
		status             int
	}{
		{"local", "", "", false, tenant.ModeSingle, 200},
		{"read", "valid", "read:*", true, tenant.ModeSingle, 200},
		{"admin", "valid", "admin:*", true, tenant.ModeSingle, 200},
		{"missing", "", "read:*", true, tenant.ModeSingle, 401},
		{"bad", "bad", "read:*", true, tenant.ModeSingle, 401},
		{"forbidden", "valid", "control:*", true, tenant.ModeSingle, 403},
		{"multi", "valid", "admin:*", true, tenant.ModeMulti, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Config{EnableAuth: tc.auth}
			cfg.Tenancy.Mode = tc.mode
			router := NewRouter(cfg, WithTokenValidator(&testValidator{validToken: "valid", scope: tc.scope}))
			r := httptest.NewRequest("GET", "/api/v1/capabilities", nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if tc.status != 200 {
				return
			}
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"contract_version": "1.0.0", "operations": []any{"capabilities.get", "health.get"}, "scripts": map[string]any{"compiled": false, "configured": false, "deployment_available": false, "caller_authorized": false, "available": false}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("manifest=%#v", got)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("capabilities must not be cached")
			}
		})
	}
}

// syncReportingBrain and previewingRunTask carry the optional interfaces the
// sync and dispatch-preview handlers assert at request time.
type syncReportingBrain struct {
	BrainService
	syncDeviceService
}
type previewingRunTask struct {
	RunTaskService
	dispatchPreviewService
}

func TestSDKCapabilitiesInventory(t *testing.T) {
	// Discovery may inspect service presence, never call it or enumerate resources.
	h := &Handler{brain: syncReportingBrain{}, tasks: struct{ TaskService }{}, monitor: struct{ MonitorService }{}, clientContext: struct{ ClientContextService }{}, supervisorOperations: struct{ SupervisorOperationStore }{}, supervisorCheckpoints: struct{ SupervisorCheckpointStore }{}, executionBudgets: struct{ ExecutionBudgetStore }{}, attachments: struct{ AttachmentService }{}, goalService: struct{ GoalService }{}, reminders: struct{ ReminderService }{}, attention: struct{ AttentionService }{}, webhooks: struct{ WebhookService }{}, automationRun: struct{ AutomationRunService }{}, placement: struct{ ProjectPlacementService }{}, runTask: previewingRunTask{}, runFeature: struct{ RunFeatureService }{}, runProject: struct{ RunProjectService }{}, depChains: struct{ DependentChainService }{}, events: struct{ EventService }{}, timeline: struct{ TimelineService }{}, runner: struct{ RunnerService }{}, runnerRegistry: struct{ RunnerRegistryService }{}, schedulerViews: struct{ SchedulerVisibilityService }{}, scheduler: struct{ SchedulerService }{}, bridge: struct{ BridgeService }{}}
	read := func(h *Handler) []string {
		t.Helper()
		w := httptest.NewRecorder()
		NewRouter(config.Config{}, WithHandler(h)).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/capabilities", nil))
		var m struct {
			Operations []string `json:"operations"`
			Version    string   `json:"contract_version"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m.Version != "1.0.0" {
			t.Fatalf("contract version=%s", m.Version)
		}
		if !slices.IsSorted(m.Operations) {
			t.Fatal("unstable manifest order")
		}
		return m.Operations
	}
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["info"].(map[string]any)["version"] != "1.0.0" {
		t.Fatal("manifest version must match the OpenAPI contract")
	}
	want := []string{"capabilities.get"}
	for _, path := range doc["paths"].(map[string]any) {
		for _, raw := range path.(map[string]any) {
			if op, ok := raw.(map[string]any); ok {
				if id, ok := op["operationId"].(string); ok && id != "capabilities.get" && id != "tasks.logs" {
					want = append(want, id)
				}
			}
		}
	}
	slices.Sort(want)
	if got := read(h); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported inventory=%v want=%v", got, want)
	}
	h.tasks = nil
	got := read(h)
	for _, id := range []string{"tasks.list", "features.run", "projects.list", "projects.delete", "tasks.delivery"} {
		if slices.Contains(got, id) {
			t.Errorf("unwired operation advertised: %s", id)
		}
	}
	h.bridge, h.runner = nil, nil
	got = read(h)
	for _, id := range []string{"control.sendPrompt", "control.spawnInstance", "control.killInstance", "dispatch.pauseAll", "runners.status"} {
		if slices.Contains(got, id) {
			t.Errorf("unwired runner/control operation advertised: %s", id)
		}
	}
	if !slices.Contains(got, "runners.list") || !slices.Contains(got, "scheduler.status") {
		t.Fatal("independent registry/scheduler services disappeared")
	}
	if !slices.Contains(got, "entries.get") {
		t.Fatal("independent entry service disappeared")
	}
	if slices.Contains(got, "control.sessionTail") || slices.Contains(got, "control.sessionDescendants") || slices.Contains(got, "supervision.snapshot") {
		t.Error("session views need the bridge and the snapshot needs tasks")
	}
	h.monitor, h.clientContext, h.supervisorOperations, h.supervisorCheckpoints, h.executionBudgets = nil, nil, nil, nil, nil
	h.brain, h.runTask = struct{ BrainService }{}, struct{ RunTaskService }{}
	got = read(h)
	for _, id := range []string{"monitors.create", "monitors.deleteByScope", "clientContext.resolve", "sync.devices", "sync.reconcile", "supervision.dispatchPreview", "supervision.submitOperation", "supervision.getOperation", "supervision.checkpoints", "supervision.updateCheckpoint", "supervision.budget", "supervision.updateBudget"} {
		if slices.Contains(got, id) {
			t.Errorf("unwired operator operation advertised: %s", id)
		}
	}
	// The supervisor registry read needs no service: its route always exists.
	if got := read(&Handler{}); !reflect.DeepEqual(got, []string{"capabilities.get", "health.get", "supervision.capabilities"}) {
		t.Fatalf("empty handler advertised=%v", got)
	}
}
