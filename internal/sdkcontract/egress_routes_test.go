package sdkcontract

import (
	"net/http"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/logbuffer"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/tenant"
	"gopkg.in/yaml.v3"
)

var closureSuffix = regexp.MustCompile(`(\.func\d+)+$`)

// handlerKey converts a runtime function name for a registered endpoint into
// the types.Func.FullName key used by egressGraph. Closures resolve to their
// enclosing declaration, whose body the graph already attributes them to.
func handlerKey(runtimeName string) string {
	name := strings.TrimSuffix(runtimeName, "-fm")
	name = closureSuffix.ReplaceAllString(name, "")
	slash := strings.LastIndex(name, "/")
	dot := strings.Index(name[slash+1:], ".") + slash + 1
	pkg, rest := name[:dot], name[dot+1:]
	switch {
	case strings.HasPrefix(rest, "(*"):
		typ, method, _ := strings.Cut(strings.TrimPrefix(rest, "(*"), ").")
		return "(*" + pkg + "." + typ + ")." + method
	case strings.Contains(rest, "."):
		typ, method, _ := strings.Cut(rest, ".")
		return "(" + pkg + "." + typ + ")." + method
	}
	return pkg + "." + rest
}

// resolveHandlerKey prefers the converted name; for closures inlined into
// another function (e.g. NewRouter.func1.HealthHandler.func2) it falls back
// to the rightmost declared function segment.
func resolveHandlerKey(g *egressGraph, runtimeName string) string {
	key := handlerKey(runtimeName)
	if g == nil || g.declared[key] {
		return key
	}
	name := strings.TrimSuffix(runtimeName, "-fm")
	slash := strings.LastIndex(name, "/")
	dot := strings.Index(name[slash+1:], ".") + slash + 1
	pkg, segments := name[:dot], strings.Split(name[dot+1:], ".")
	for i := len(segments) - 1; i >= 0; i-- {
		if candidate := pkg + "." + segments[i]; g.declared[candidate] {
			return candidate
		}
	}
	return key
}

// operationHandlers maps every OpenAPI operationId to the registered endpoint
// handler's graph key, using the real router composition.
func operationHandlers(t *testing.T, g *egressGraph) (map[string]string, []string) {
	t.Helper()
	data, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := yaml.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	params := regexp.MustCompile(`\{[^}]+\}`)
	normalize := func(path string) string { return strings.TrimRight(params.ReplaceAllString(path, "{}"), "/") }
	cfg := config.Config{}
	cfg.Tenancy.Mode = tenant.ModeSingle
	// Every optional service is present (zero values: chi.Walk never invokes a
	// handler), so routes resolve to real handlers, not unavailable placeholders.
	router := api.NewRouter(cfg, api.WithHandler(api.NewHandler(nil,
		api.WithTaskService(service.NewTaskService(&cfg, nil, nil)),
		api.WithEventService(service.NewEventService(realtime.NewEventHub())),
		api.WithAttachmentService(&service.AttachmentServiceImpl{}),
		api.WithAttentionService(&service.AttentionService{}),
		api.WithGoalService(&service.GoalService{}),
		api.WithReminderService(&service.ReminderService{}),
		api.WithWebhookService(&service.WebhookServiceImpl{}),
		api.WithTimelineService(&service.TimelineService{}),
		api.WithAutomationRunService(&service.AutomationService{}),
		api.WithProjectPlacementService(&service.ProjectPlacementService{}),
		api.WithMonitorService(&service.MonitorServiceImpl{}),
		api.WithRunnerService(&service.RunnerServiceImpl{}),
		api.WithRunnerRegistryService(&service.RunnerRegistryServiceImpl{}),
		api.WithClientContextService(&service.ClientContextServiceImpl{}),
		api.WithBulkJobService(&service.BulkJobService{}),
		api.WithSchedulerService(&service.SchedulerService{}),
		api.WithLogBuffer(logbuffer.New(1)),
	)))
	routes := map[string]string{}
	if err := chi.Walk(router, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		v := reflect.ValueOf(h)
		if hf, ok := h.(http.HandlerFunc); ok {
			v = reflect.ValueOf(hf)
		}
		if v.Kind() == reflect.Func {
			routes[method+" "+normalize(route)] = resolveHandlerKey(g, runtime.FuncForPC(v.Pointer()).Name())
		} else {
			routes[method+" "+normalize(route)] = "non-func:" + v.Type().String()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wildcards := map[string]string{"entries.get": "/entries/*", "entries.update": "/entries/*", "entries.delete": "/entries/*", "entries.move": "/entries/*", "entries.updateMetadata": "/entries/*"}
	out := map[string]string{}
	for path, item := range wire["paths"].(map[string]any) {
		for method, raw := range item.(map[string]any) {
			if method == "parameters" {
				continue
			}
			id := raw.(map[string]any)["operationId"].(string)
			routePath := path
			if w, ok := wildcards[id]; ok {
				routePath = w
			}
			key, ok := routes[strings.ToUpper(method)+" /api/v1"+normalize(routePath)]
			if !ok {
				t.Fatalf("operation %s has no router match", id)
			}
			out[id] = key
		}
	}
	var all []string
	for _, key := range routes {
		all = append(all, key)
	}
	return out, all
}

// derivableTokens are provider effects the call graph can see. Others
// (webhook_http, extraction_provider, external_delivery_verification,
// downstream_*) remain pinned by TestOperationPolicyProviderEffectsArePinned.
var derivableTokens = map[string]bool{"embedding_sync": true, "embedding_background": true, "web_push": true}

// tokenSatisfied reports whether a row's provider tokens cover a derived one.
// embedding_for_semantic_hybrid names search's synchronous query embedding.
func tokenSatisfied(row []string, derived string) bool {
	for _, r := range row {
		if r == derived || (derived == "embedding_sync" && r == "embedding_for_semantic_hybrid") {
			return true
		}
	}
	return false
}

// reviewedBackground lists every background entry point (poller, dispatcher,
// scheduler, startup path, or non-SDK surface) that reaches a provider sink
// without an HTTP request, mapped to the operations whose state drives it.
// Entries with no operations are reviewed non-SDK paths; "event_fanout" is
// covered by the policy's top-level event_fanout declaration. The set must
// equal the derived background entry points exactly: a new one fails.
var reviewedBackground = map[string][]string{
	"(*" + modulePath + "/internal/api.Handler).StartPush":                            {"reminders.create", "reminders.update", "reminders.snooze", "reminders.fire"}, // collectPush: Web Push for fired reminders
	"(*" + modulePath + "/internal/service.ReminderService).Start":                    {"reminders.create", "reminders.update", "reminders.snooze"},                   // scheduler fires due reminders
	"(*" + modulePath + "/internal/service.AttentionDispatcher).Start":                {"attention.create"},                                                           // attention.created -> Web Push
	"(*" + modulePath + "/internal/service.GoalService).Start":                        {"goals.create", "goals.update"},                                               // goal ticker reconcile
	"(*" + modulePath + "/internal/service.AutomationService).Start":                  {"event_fanout"},                                                               // event-triggered automations
	"(*" + modulePath + "/internal/service.BulkJobService).Start":                     nil,                                                                            // bulk-jobs API is not in the SDK contract
	"(*" + modulePath + "/internal/service.AttachmentServiceImpl).StoreDerivedText":   nil,                                                                            // no caller in the repository
	"(*" + modulePath + "/internal/service.BrainServiceImpl).EnsureBrainMergeRequest": nil,                                                                            // no production caller
	modulePath + "/internal/service.EnsureBuiltInFeatureCheckoutAutomation":           nil,                                                                            // startup built-in automation registration
	modulePath + "/internal/service.EnsureBuiltInFeatureCheckoutSimpleAutomation":     nil,
	modulePath + "/internal/service.EnsureBuiltInFeatureDeliveryAutomation":           nil,
	"(*" + modulePath + "/internal/api.AssistantService).StartConversationJobs":       nil, // assistant is not in the SDK contract
	"(*" + modulePath + "/internal/api.Handler).HandleAssistantChat":                  nil,
	"(*" + modulePath + "/internal/api.Handler).HandleAssistantChatStream":            nil,
	"(*" + modulePath + "/internal/api.Handler).HandleAssistantStatus":                nil,
	modulePath + "/internal/api.handleBulkUpdate":                                     nil, // assistant tool dispatch
	modulePath + "/internal/api.handleSearchBrain":                                    nil,
	modulePath + "/internal/api.handleUpdateEntry":                                    nil,
}

// reviewedFlowExceptions are derived tokens a row may omit because the
// flow-insensitive graph over-approximates a reviewed, condition-guarded path.
// Each must still be derived (no stale exceptions) and carry a reason.
var reviewedFlowExceptions = map[string]map[string]string{
	"reminders.ack": {
		"embedding_sync":       "AckReminder -> UpdateReminder sets status only; feature-schedule gate fields are never set (review nwwa27yh/1ikgd5xs)",
		"embedding_background": "status-only Update: body/attachments unchanged -> scheduleEmbeddingMetadataSync, no provider call",
	},
}

// TestOperationProviderEffectsDerivedFromCallGraph derives each SDK
// operation's embedding/Web Push reachability from code (real router handler
// -> conservative call graph -> provider sinks) and requires the policy row to
// cover it. It also requires every derivable token in a row to be justified
// by a handler path or a reviewed background mapping, and every background
// entry point to be reviewed.
func TestOperationProviderEffectsDerivedFromCallGraph(t *testing.T) {
	data, err := os.ReadFile("../../api/operation-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		EventFanout []string `yaml:"event_fanout"`
		Operations  map[string][]string
	}
	if err := yaml.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	g := buildEgressGraph(t)
	handlers, allRoutes := operationHandlers(t, g)
	if len(handlers) != 105 {
		t.Fatalf("resolved %d operation handlers, want 105", len(handlers))
	}
	justified := map[string]map[string]bool{}
	justify := func(op, tok string) {
		if justified[op] == nil {
			justified[op] = map[string]bool{}
		}
		justified[op][tok] = true
	}
	for _, op := range sortedKeys(handlers) {
		h := handlers[op]
		if !g.declared[h] || strings.HasSuffix(h, ".notImplemented") {
			t.Errorf("%s resolves to %s: not a real analyzed handler (fail closed)", op, h)
			continue
		}
		row := strings.Split(policy.Operations[op][4], "+")
		found := g.reach(h)
		for _, tok := range sortedKeys(found) {
			justify(op, tok)
			if tokenSatisfied(row, tok) {
				continue
			}
			if reason := reviewedFlowExceptions[op][tok]; reason != "" {
				continue
			}
			t.Errorf("%s reaches %s via %s but its provider column %q omits it", op, tok, strings.Join(found[tok], " -> "), policy.Operations[op][4])
		}
		for tok := range reviewedFlowExceptions[op] {
			if _, ok := found[tok]; !ok {
				t.Errorf("stale flow exception %s/%s: no longer derived", op, tok)
			}
		}
	}
	tops := backgroundTops(g, allRoutes)
	if strings.Join(tops, "\n") != strings.Join(sortedKeys(reviewedBackground), "\n") {
		t.Fatalf("background provider entry points changed; review api/operation-policy.yaml.\nderived:\n%s\nreviewed:\n%s", strings.Join(tops, "\n"), strings.Join(sortedKeys(reviewedBackground), "\n"))
	}
	for _, top := range tops {
		tokens := g.reach(top)
		for _, op := range reviewedBackground[top] {
			if op == "event_fanout" {
				if len(policy.EventFanout) == 0 {
					t.Errorf("%s is event fan-out but event_fanout is not declared", top)
				}
				continue
			}
			row := strings.Split(policy.Operations[op][4], "+")
			for tok := range tokens {
				justify(op, tok)
				if !tokenSatisfied(row, tok) {
					t.Errorf("%s (background %s) reaches %s but its provider column %q omits it", op, top, tok, policy.Operations[op][4])
				}
			}
		}
	}
	// No hand-claimed derivable token without a code path.
	for op, row := range policy.Operations {
		for _, tok := range strings.Split(row[4], "+") {
			if derivableTokens[tok] && !justified[op][tok] {
				t.Errorf("%s claims %s but no handler or reviewed background path reaches it", op, tok)
			}
		}
	}
}

// backgroundTops returns analyzed functions that reach a provider sink, are
// not reachable from any HTTP route handler, and have no analyzed caller: the
// entry points of pollers, dispatchers, schedulers and startup paths.
func backgroundTops(g *egressGraph, routeHandlers []string) []string {
	fromRoutes := map[string]bool{}
	var stack []string
	for _, r := range routeHandlers {
		if !fromRoutes[r] {
			fromRoutes[r] = true
			stack = append(stack, r)
		}
	}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for next := range g.edges[n] {
			if !fromRoutes[next] {
				fromRoutes[next] = true
				stack = append(stack, next)
			}
		}
	}
	var tops []string
	for key := range g.declared {
		if fromRoutes[key] || len(g.reach(key)) == 0 {
			continue
		}
		hasCaller := false
		for caller := range g.callers[key] {
			if g.declared[caller] {
				hasCaller = true
				break
			}
		}
		if !hasCaller {
			tops = append(tops, key)
		}
	}
	sort.Strings(tops)
	return tops
}
