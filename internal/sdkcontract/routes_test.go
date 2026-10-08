package sdkcontract_test

import (
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"gopkg.in/yaml.v3"
)

// This is route existence, not authorization or handler success evidence.
// The router registers unavailable-service placeholders at the same paths.
func TestDeliveredContractMatchesRouterAndInventory(t *testing.T) {
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			ID string `yaml:"operationId"`
		} `yaml:"paths"`
	}
	// Path-level parameters are arrays, so decode operation maps explicitly.
	var wire map[string]any
	if err := yaml.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	doc.Paths = make(map[string]map[string]struct {
		ID string `yaml:"operationId"`
	})
	for path, item := range wire["paths"].(map[string]any) {
		doc.Paths[path] = make(map[string]struct {
			ID string `yaml:"operationId"`
		})
		for method, raw := range item.(map[string]any) {
			if method == "parameters" {
				continue
			}
			doc.Paths[path][method] = struct {
				ID string `yaml:"operationId"`
			}{raw.(map[string]any)["operationId"].(string)}
		}
	}
	params := regexp.MustCompile(`\{[^}]+\}`)
	normalize := func(path string) string { return strings.TrimRight(params.ReplaceAllString(path, "{}"), "/") }
	cfg := config.Config{}
	cfg.Tenancy.Mode = tenant.ModeSingle
	// Task assignment routes have no unavailable-service placeholders. Walk
	// the task-enabled composition; no method is invoked by chi.Walk.
	// Remote-control routes exist only with a bridge, and the supervisor
	// operation/checkpoint/budget routes only with their stores (no
	// placeholder paths).
	router := api.NewRouter(cfg, api.WithHandler(api.NewHandler(nil, api.WithTaskService(service.NewTaskService(&cfg, nil, nil)), api.WithEventService(service.NewEventService(realtime.NewEventHub())), api.WithBridgeService(&bridge.Hub{}),
		api.WithSupervisorOperations(&storage.TenantStore{}), api.WithSupervisorCheckpoints(&storage.TenantStore{}), api.WithExecutionBudgets(&storage.TenantStore{}))))
	routes := map[string]bool{}
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes[method+" "+normalize(route)] = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	matrix, err := os.ReadFile("../../docs/sdk-operation-matrix.md")
	if err != nil {
		t.Fatal(err)
	}
	inventory := map[string]string{}
	for _, line := range strings.Split(string(matrix), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 4 {
			continue
		}
		inventory[strings.TrimSpace(parts[1])] = normalize(strings.TrimSpace(parts[2]))
	}
	count := 0
	// These legacy operations deliberately use the entry wildcard dispatcher.
	// This inventory checks dispatch route existence, not suffix-handler behavior.
	wildcards := map[string]string{"entries.get": "/entries/*", "entries.update": "/entries/*", "entries.delete": "/entries/*", "entries.move": "/entries/*", "entries.updateMetadata": "/entries/*"}
	for path, methods := range doc.Paths {
		for method, op := range methods {
			count++
			routePath := path
			if wildcard, ok := wildcards[op.ID]; ok {
				routePath = wildcard
			}
			if !routes[strings.ToUpper(method)+" /api/v1"+normalize(routePath)] {
				t.Errorf("contract operation %s has no router match: %s %s", op.ID, method, path)
			}
			if got, want := inventory[op.ID], strings.ToUpper(method)+" "+normalize(path); got != want {
				t.Errorf("inventory %s = %q, contract = %q", op.ID, got, want)
			}
		}
	}
	t.Logf("checked %d delivered operations against router and inventory; pending inventory is not claimed implemented", count)
}

// Preserve and disclose legacy chi precedence; do not "fix" OpenAPI ambiguity
// by changing existing REST routes or pretending both meanings are reachable.
func TestLegacyTaskFeatureAmbiguityUsesStaticFeatureRoute(t *testing.T) {
	cfg := config.Config{}
	cfg.Tenancy.Mode = tenant.ModeSingle
	router := api.NewRouter(cfg, api.WithHandler(api.NewHandler(nil, api.WithTaskService(service.NewTaskService(&cfg, nil, nil)))))
	for _, tc := range []struct {
		path, pattern string
	}{
		// POST: a project literally named "runner" cannot clear the
		// assignment of a feature named pause/resume; the dial route wins.
		{"POST /api/v1/tasks/runner/features/pause/assignment/clear", "/api/v1/tasks/runner/features/pause/{projectId}/{featureId}"},
		{"POST /api/v1/tasks/other/features/pause/assignment/clear", "/api/v1/tasks/{projectId}/features/{featureId}/assignment/clear"},
		{"/api/v1/tasks/project/features/delivery", "/api/v1/tasks/{projectId}/features/{featureId}"},
		{"/api/v1/tasks/project/ordinary/delivery", "/api/v1/tasks/{projectId}/{taskId}/delivery"},
		// Same legacy precedence for the scheduler views: a task literally
		// named "features" cannot be addressed; ordinary ids are unaffected.
		{"/api/v1/tasks/project/features/dispatch-lease", "/api/v1/tasks/{projectId}/features/{featureId}"},
		{"/api/v1/tasks/project/ordinary/dispatch-lease", "/api/v1/tasks/{projectId}/{taskId}/dispatch-lease"},
		{"/api/v1/tasks/project/ordinary/placement-reasons", "/api/v1/tasks/{projectId}/{taskId}/placement-reasons"},
		// Runner candidates: a task literally named "features" cannot be
		// evaluated; the feature route wins, as for delivery above. The
		// proposed-task POST is a static segment and never a task id.
		{"/api/v1/tasks/project/features/runner-candidates", "/api/v1/tasks/{projectId}/features/{featureId}"},
		{"/api/v1/tasks/project/ordinary/runner-candidates", "/api/v1/tasks/{projectId}/{taskId}/runner-candidates"},
		{"/api/v1/tasks/project/features/f/runner-candidates", "/api/v1/tasks/{projectId}/features/{featureId}/runner-candidates"},
		{"POST /api/v1/tasks/project/runner-candidates", "/api/v1/tasks/{projectId}/runner-candidates"},
	} {
		method, path := http.MethodGet, tc.path
		if m, p, ok := strings.Cut(tc.path, " "); ok {
			method, path = m, p
		}
		rctx := chi.NewRouteContext()
		if !router.Match(rctx, method, path) {
			t.Fatalf("missing legacy route %s", tc.path)
		}
		if got := rctx.RoutePattern(); got != tc.pattern {
			t.Fatalf("legacy dispatch %s: got %s want %s", tc.path, got, tc.pattern)
		}
	}
}
