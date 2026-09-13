package apiserver

import (
	"fmt"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantPhase5RoutesHaveZeroEffects(t *testing.T) {
	f := newGraphFixture(t, 2)
	g := f.graph(t, tenant.Local)
	defer g.Close()
	h := tenantContentRoutes(g.handler)
	snapshot := func() []string {
		var result []string
		for _, table := range []string{"notes", "event_log", "bulk_jobs", "bulk_job_items", "execution_budgets", "budget_reservations", "supervisor_operations", "supervisor_checkpoints", "supervisor_checkpoint_versions", "entry_sync_devices", "entry_sync_operations", "entry_sync_changes", "entry_sync_identity"} {
			rows, err := f.db.Query("SELECT * FROM " + table)
			if err != nil {
				t.Fatal(err)
			}
			cols, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				values := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range values {
					ptrs[i] = &values[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				result = append(result, fmt.Sprintf("%s:%v", table, values))
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
		}
		sort.Strings(result)
		return result
	}
	before := snapshot()
	for _, route := range []string{
		"/api/v1/bulk-jobs", "/api/v1/bulk-jobs/j/items", "/api/v1/bulk-jobs/j/control",
		"/api/v1/supervision/operations", "/api/v1/supervision/operations/o", "/api/v1/supervision/checkpoints", "/api/v1/supervision/budgets", "/api/v1/supervision/snapshot", "/api/v1/supervision/capabilities", "/api/v1/supervision/dispatch-preview",
		"/api/v1/sync/entries", "/api/v1/sync/entries/selected", "/api/v1/sync/identity", "/api/v1/sync/devices", "/api/v1/sync/devices/d/report", "/api/v1/sync/devices/d/operations/o/diff", "/api/v1/sync/devices/d/operations/o/reconcile",
		"/api/v1/assistant/jobs", "/api/v1/assistant/conversations", "/api/v1/assistant/speech", "/api/v1/assistant/transcribe", "/api/v1/assistant/voice-diagnostics",
		"/api/v1/push/subscribe", "/api/v1/push", "/api/v1/tasks/p/t/resume-with-context", "/api/v1/tasks/p/t/delivery", "/api/v1/control/runners/r/sessions/s/children", "/mcp",
	} {
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
			r := httptest.NewRequest(method, route, nil)
			r = r.WithContext(tenant.Into(r.Context(), tenant.Local))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != 501 {
				t.Fatalf("%s %s status %d", method, route, w.Code)
			}
		}
	}
	if !reflect.DeepEqual(before, snapshot()) {
		t.Fatal("denied route changed content, event, receipt, budget or sync state")
	}
	if g.assistant == nil {
		t.Fatal("test must exercise the real composed graph")
	}
}
