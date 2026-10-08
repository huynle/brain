package sdkcontract

import (
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/bridge"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/logbuffer"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
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
		api.WithSchedulerVisibilityService(&storage.TenantStore{}),
		api.WithBridgeService(&bridge.Hub{}),
		api.WithSupervisorOperations(&storage.TenantStore{}),
		api.WithSupervisorCheckpoints(&storage.TenantStore{}),
		api.WithExecutionBudgets(&storage.TenantStore{}),
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

// egressDerivation is everything the call graph says reaches a provider sink.
// Maps are review name -> "+"-joined sorted tokens.
type egressDerivation struct {
	ops        map[string]map[string][]string // SDK operation -> token -> witness
	nonSDK     map[string]string
	background map[string]string
	callbacks  map[string]string
	// number of sink-reaching roots merged under each review name
	backgroundCount map[string]int
	callbackCount   map[string]int
	startup         map[string]string
}

func tokenString(found map[string][]string) string { return strings.Join(sortedKeys(found), "+") }

func deriveEgress(g *egressGraph, handlers map[string]string, chiRoutes []string, cuts map[string][]cutEdge) egressDerivation {
	d := egressDerivation{ops: map[string]map[string][]string{}, nonSDK: map[string]string{}, background: map[string]string{}, callbacks: map[string]string{}, startup: map[string]string{}, backgroundCount: map[string]int{}, callbackCount: map[string]int{}}
	sdk := map[string]bool{}
	for op, h := range handlers {
		sdk[h] = true
		cutSet := map[cutEdge]bool{}
		for _, c := range cuts[op] {
			cutSet[c] = true
		}
		d.ops[op] = g.reach(h, cutSet, nil)
	}
	routeRoots := map[string]bool{}
	for _, r := range chiRoutes {
		routeRoots[r] = true
	}
	for r := range g.routerRef {
		routeRoots[r] = true
	}
	for h := range sdk {
		routeRoots[h] = true
	}
	for _, r := range sortedKeys(routeRoots) {
		if sdk[r] {
			continue
		}
		if toks := g.tokens[r]; len(toks) > 0 {
			d.nonSDK[r] = strings.Join(sortedKeys(toks), "+")
		}
	}
	routeReach := g.closure(sortedKeys(routeRoots), nil)
	merge := func(m map[string]string, name, toks string) {
		if prev := m[name]; prev != "" {
			set := map[string]bool{}
			for _, t := range strings.Split(prev+"+"+toks, "+") {
				set[t] = true
			}
			toks = strings.Join(sortedKeys(set), "+")
		}
		m[name] = toks
	}
	for _, root := range sortedKeys(g.goRoots) {
		if routeReach[root] {
			continue
		}
		if toks := g.tokens[root]; len(toks) > 0 {
			merge(d.background, g.reviewName(root), strings.Join(sortedKeys(toks), "+"))
			d.backgroundCount[g.reviewName(root)]++
		}
	}
	// Workers started through a shared launcher (launch(func(){...})) sit
	// behind one go statement: also count every sink-reaching function
	// literal inside a background root's enclosing function, so adding one
	// changes the reviewed count.
	for _, key := range sortedKeys(g.declared) {
		name, isLit := g.enclosing[key]
		if !isLit || g.goRoots[key] || len(g.tokens[key]) == 0 {
			continue
		}
		if _, background := d.background[name]; background {
			d.backgroundCount[name]++
		}
	}
	stop := map[string]bool{}
	for k := range g.goRoots {
		stop[k] = true
	}
	for k := range routeRoots {
		stop[k] = true
	}
	for _, e := range g.entries {
		if found := g.reach(e, nil, stop); len(found) > 0 {
			d.startup[e] = tokenString(found)
		}
	}
	var everything []string
	everything = append(everything, sortedKeys(routeRoots)...)
	everything = append(everything, sortedKeys(g.goRoots)...)
	everything = append(everything, g.entries...)
	live := g.closure(everything, nil)
	for _, n := range sortedKeys(g.address) {
		if live[n] || !g.declared[n] {
			continue
		}
		if toks := g.tokens[n]; len(toks) > 0 {
			merge(d.callbacks, g.reviewName(n), strings.Join(sortedKeys(toks), "+"))
			d.callbackCount[g.reviewName(n)]++
		}
	}
	return d
}

func TestEgressExploration(t *testing.T) {
	if os.Getenv("BRAIN_EGRESS_EXPLORE") == "" {
		t.Skip("exploration only")
	}
	g := sharedEgressGraph(t)
	handlers, chi := operationHandlers(t, g)
	d := deriveEgress(g, handlers, chi, nil)
	short := func(s string) string { return strings.ReplaceAll(s, modulePath+"/", "") }
	for _, op := range sortedKeys(d.ops) {
		if len(d.ops[op]) > 0 {
			t.Logf("OP %s: %s", op, tokenString(d.ops[op]))
		}
	}
	for _, sec := range []struct {
		name string
		m    map[string]string
	}{{"NONSDK", d.nonSDK}, {"BACKGROUND", d.background}, {"CALLBACK", d.callbacks}, {"STARTUP", d.startup}} {
		for _, k := range sortedKeys(sec.m) {
			t.Logf("%s %s: %s", sec.name, short(k), sec.m[k])
		}
	}
	for _, r := range g.reflect {
		t.Logf("REFLECT %s", r)
	}
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

const (
	pkgAPI     = modulePath + "/internal/api"
	pkgService = modulePath + "/internal/service"
)

// reviewedCuts are flow-sensitive exceptions tied to ONE reviewed call edge.
// Reachability for the operation is recomputed without exactly that edge; any
// other path to a sink (e.g. a new direct Save) still fails. A cut is stale if
// the edge disappears or removing it no longer removes a derived token.
var reviewedCuts = map[string][]cutEdge{
	// AckReminder -> UpdateReminder sets status only: body/attachments are
	// unchanged (metadata sync, no provider) and feature-schedule gate fields are
	// never set (reviews 1ikgd5xs, nwwa27yh).
	"reminders.ack": {{"(*" + pkgService + ".ReminderService).AckReminder", "(*" + pkgService + ".ReminderService).UpdateReminder"}},
	// HandleSchedulerStatus calls h.scheduler.Status() on api.SchedulerService
	// (Status() types.SchedulerStatus). The graph resolves interface calls by
	// method name, so it also follows AssistantService.Status, whose result type
	// (AssistantStatusResponse) cannot satisfy that interface; the assistant's
	// tool table is what reaches embedding. TestSchedulerStatusCutIsSound pins
	// the non-implementation so this cut cannot hide a real implementation.
	"scheduler.status": {{"(*" + pkgAPI + ".Handler).HandleSchedulerStatus", "(*" + pkgAPI + ".AssistantService).Status"}},
}

// The cut is sound only while the edge it removes exists solely because the
// graph expands h.scheduler.Status() to every method named Status: the
// assistant must not implement the interface, and the handler must not call
// the assistant's Status itself (that would be the same edge, hidden).
func TestSchedulerStatusCutIsSound(t *testing.T) {
	iface := reflect.TypeOf((*api.SchedulerService)(nil)).Elem()
	if reflect.TypeOf(&api.AssistantService{}).Implements(iface) {
		t.Fatal("AssistantService now implements api.SchedulerService: the scheduler.status cut is no longer an over-approximation; review its provider effects")
	}
	g := sharedEgressGraph(t)
	for _, c := range reviewedCuts["scheduler.status"] {
		if g.direct[c.from][c.to] {
			t.Fatalf("%s calls %s directly: the scheduler.status cut would hide a real call, not an interface over-approximation; review its provider effects", c.from, c.to)
		}
		if !g.edges[c.from][c.to] {
			t.Fatalf("stale scheduler.status cut: no edge %s -> %s", c.from, c.to)
		}
	}
}

// reviewedNonSDKRoutes: every router handler outside the 146-operation
// contract that reaches a provider sink, with its exact derived tokens.
var reviewedNonSDKRoutes = map[string]string{
	"(*" + pkgAPI + ".Handler).HandleAssistantChat":                "embedding_background+embedding_sync",
	"(*" + pkgAPI + ".Handler).HandleAssistantChatStream":          "embedding_background+embedding_sync",
	"(*" + pkgAPI + ".Handler).HandleAssistantStatus":              "embedding_background+embedding_sync",
	"(*" + pkgAPI + ".Handler).HandleBackfillAttachmentExtraction": "embedding_sync",
	"(*" + pkgAPI + ".Handler).HandleEmbeddingBackfill":            "embedding_sync",
	"(*" + pkgAPI + ".Handler).HandleEntrySyncMutation":            "embedding_background+embedding_sync",
	"(*" + pkgAPI + ".Handler).HandleToggleMonitor":                "embedding_background+embedding_sync",
}

type reviewedRoot struct {
	count  int      // sink-reaching goroutine/callback roots plus sink-reaching literals under this name
	tokens string   // exact derived tokens
	ops    []string // operations whose state drives it (nil: non-SDK)
}

// reviewedBackground: every goroutine root in the module that reaches a sink
// without an HTTP request, named by its (line-independent) enclosing
// declaration, with the exact number of such roots there.
var reviewedBackground = map[string]reviewedRoot{
	"(*" + pkgAPI + ".Handler).StartPush":                                   {1, "web_push", []string{"reminders.create", "reminders.update", "reminders.snooze", "reminders.fire"}}, // fired-reminder push poller
	"(*" + pkgService + ".AttentionDispatcher).Start":                       {1, "web_push", []string{"attention.create"}},
	modulePath + "/internal/apiserver.startSingleGraphWorkers":              {8, "embedding_background+embedding_sync", []string{"reminders.create", "reminders.update", "reminders.snooze", "goals.create", "goals.update", "event_fanout"}}, // launcher go literal + launch closure + 4 sink-reaching launch(func(){...}) workers (automations, goals ticker, reminder scheduler, trigger dispatcher; the webhook dispatcher reaches no sink) + 2 shutdown literals (returned closure, once.Do) (review qytghjxc)
	"(*" + pkgAPI + ".conversationJobs).run":                                {2, "embedding_background+embedding_sync", nil},                                                                                                                  // assistant jobs: not in the SDK contract
	"(*" + pkgService + ".BulkJobService).Start":                            {1, "embedding_background+embedding_sync", nil},                                                                                                                  // bulk-jobs API: not in the SDK contract
	modulePath + "/cmd/brain/commands.runServerWithOptionalRunner":          {1, "embedding_background+embedding_sync+web_push", nil},                                                                                                         // whole server started in a goroutine
	"(*" + modulePath + "/internal/apiserver.tenantGraphManager).construct": {1, "embedding_background+embedding_sync", nil},                                                                                                                  // tenant-mode graph construction (public startup is single-mode)
	modulePath + "/internal/apiserver.wireSupervisorControlEvents":          {1, "embedding_background+embedding_sync", nil},                                                                                                                  // runner-bridge control observer: not in the SDK contract
}

// reviewedCallbacks: address-taken code reaching a sink that nothing in the
// module calls (invoked by external code or a table lookup).
var reviewedCallbacks = map[string]reviewedRoot{
	modulePath + "/internal/apiserver.tenantWorkloadHTTP": {1, "embedding_background+embedding_sync", nil}, // tenant-mode HTTP entry (sealed allowlist), registered outside router.go
}

// reviewedStartup: program entry points' synchronous effects (stopping at
// goroutine and route roots), e.g. built-in automation registration.
var reviewedStartup = map[string]string{
	modulePath + "/cmd/brain.main": "embedding_background+embedding_sync",
}

// TestOperationProviderEffectsDerivedFromCallGraph derives embedding and Web
// Push effects across the whole module and requires the policy and the
// reviewed root lists to match it.
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
	g := sharedEgressGraph(t)
	handlers, chi := operationHandlers(t, g)
	if len(handlers) != 146 {
		t.Fatalf("resolved %d operation handlers, want 146", len(handlers))
	}
	d := deriveEgress(g, handlers, chi, reviewedCuts)
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
		for _, tok := range sortedKeys(d.ops[op]) {
			justify(op, tok)
			if !tokenSatisfied(row, tok) {
				t.Errorf("%s reaches %s via %s but its provider column %q omits it", op, tok, strings.Join(d.ops[op][tok], " -> "), policy.Operations[op][4])
			}
		}
	}
	for op, cuts := range reviewedCuts {
		uncut := g.reach(handlers[op], nil, nil)
		for _, c := range cuts {
			if !g.edges[c.from][c.to] {
				t.Errorf("stale cut for %s: edge %s -> %s no longer exists", op, c.from, c.to)
			}
		}
		if len(uncut) == len(d.ops[op]) {
			t.Errorf("stale cut for %s: removing the reviewed edge removes no token", op)
		}
	}
	exact := func(kind string, got, want map[string]string) {
		if strings.Join(sortedPairs(got), "\n") != strings.Join(sortedPairs(want), "\n") {
			t.Errorf("%s provider reachability changed; review it.\nderived:\n%s\nreviewed:\n%s", kind, strings.Join(sortedPairs(got), "\n"), strings.Join(sortedPairs(want), "\n"))
		}
	}
	exact("non-SDK routes", d.nonSDK, reviewedNonSDKRoutes)
	exact("startup entry points", d.startup, reviewedStartup)
	exactRoots := func(kind string, gotTokens map[string]string, gotCount map[string]int, want map[string]reviewedRoot) {
		got, wantFlat := map[string]string{}, map[string]string{}
		for k, v := range gotTokens {
			got[k] = fmt.Sprintf("%d %s", gotCount[k], v)
		}
		for k, v := range want {
			wantFlat[k] = fmt.Sprintf("%d %s", v.count, v.tokens)
		}
		exact(kind, got, wantFlat)
		for name, r := range want {
			for _, op := range r.ops {
				if op == "event_fanout" {
					if len(policy.EventFanout) == 0 {
						t.Errorf("%s is event fan-out but event_fanout is not declared", name)
					}
					continue
				}
				row := strings.Split(policy.Operations[op][4], "+")
				for _, tok := range strings.Split(gotTokens[name], "+") {
					if tok == "" {
						continue
					}
					justify(op, tok)
					if !tokenSatisfied(row, tok) {
						t.Errorf("%s (driven by %s) reaches %s but its provider column %q omits it", op, name, tok, policy.Operations[op][4])
					}
				}
			}
		}
	}
	exactRoots("background goroutine roots", d.background, d.backgroundCount, reviewedBackground)
	exactRoots("callback roots", d.callbacks, d.callbackCount, reviewedCallbacks)
	for op, row := range policy.Operations {
		for _, tok := range strings.Split(row[4], "+") {
			if derivableTokens[tok] && !justified[op][tok] {
				t.Errorf("%s claims %s but no handler or reviewed background path reaches it", op, tok)
			}
		}
	}
}

func sortedPairs(m map[string]string) []string {
	var out []string
	for _, k := range sortedKeys(m) {
		out = append(out, k+" = "+m[k])
	}
	return out
}

// reviewedReflection: reflective calls are a known limit of the static
// analysis (reflect.Value.Call/CallSlice/Method/MethodByName can reach any
// method). None exist in the module; any new one must be reviewed here.
var reviewedReflection = map[string]bool{}

func TestNoUnreviewedReflectiveCalls(t *testing.T) {
	g := sharedEgressGraph(t)
	for _, site := range g.reflect {
		if !reviewedReflection[site] {
			t.Errorf("unreviewed reflective call (egress analysis cannot follow it): %s", site)
		}
	}
}
