package sdkcontract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every row's provider column is pinned here, from a code audit
// (review zgck7qp2): direct or triggered provider egress per operation. Tokens
// combine with "+"; "none" is never combined. Event fan-out (webhooks and
// event-triggered automations) applies to every event-emitting operation and is
// declared once at top level as event_fanout, not repeated per row.
var reviewedProviderEffects = map[string]string{
	"capabilities.get":           "none",
	"health.get":                 "none",
	"entries.list":               "none",
	"entries.get":                "none",
	"entries.create":             "embedding_sync+downstream_if_runnable",
	"entries.update":             "embedding_background+downstream_if_runnable",
	"entries.updateMetadata":     "embedding_background+downstream_if_runnable",
	"entries.move":               "none",
	"entries.delete":             "none",
	"entries.bulkUpdate":         "embedding_background+downstream_if_runnable",
	"entries.bulkDelete":         "none",
	"search.query":               "embedding_for_semantic_hybrid",
	"search.inject":              "embedding_requires_review",
	"sections.list":              "none",
	"sections.get":               "none",
	"graph.backlinks":            "none",
	"graph.outlinks":             "none",
	"graph.related":              "none",
	"graph.orphans":              "none",
	"projects.list":              "none",
	"projects.delete":            "none",
	"projects.getPlacement":      "none",
	"projects.setPlacement":      "none",
	"tasks.list":                 "none",
	"tasks.get":                  "none",
	"tasks.ready":                "none",
	"tasks.waiting":              "none",
	"tasks.blocked":              "none",
	"tasks.next":                 "none",
	"tasks.status":               "none",
	"tasks.metadata":             "none",
	"tasks.claimStatus":          "none",
	"tasks.resume":               "downstream_executor",
	"tasks.resumeWithContext":    "downstream_executor",
	"tasks.assign":               "none",
	"tasks.clearAssignment":      "none",
	"tasks.trigger":              "downstream_executor",
	"tasks.run":                  "downstream_executor",
	"tasks.dispatch":             "downstream_executor",
	"tasks.logs":                 "none",
	"tasks.delivery":             "none",
	"tasks.verifyDelivery":       "external_delivery_verification",
	"features.list":              "none",
	"features.ready":             "none",
	"features.get":               "none",
	"features.chains":            "none",
	"features.checkout":          "downstream_executor",
	"features.assign":            "none",
	"features.clearAssignment":   "none",
	"features.run":               "downstream_executor",
	"features.cancel":            "none",
	"features.resume":            "downstream_executor",
	"features.resumeWithContext": "downstream_executor",
	"projects.run":               "downstream_executor",
	"goals.list":                 "none",
	"goals.progress":             "none",
	"goals.audit":                "none",
	"goals.create":               "embedding_sync+downstream_executor",
	"goals.update":               "embedding_background+downstream_executor",
	"goals.delete":               "none",
	"goals.run":                  "embedding_sync+embedding_background+downstream_executor",
	"automations.run":            "embedding_sync+embedding_background+downstream_executor",
	"automations.runs":           "none",
	"automations.getRun":         "none",
	"reminders.list":             "none",
	"reminders.get":              "none",
	"reminders.create":           "embedding_sync+downstream_action",
	"reminders.update":           "embedding_background+downstream_action",
	"reminders.delete":           "none",
	"reminders.ack":              "none",
	"reminders.snooze":           "none",
	"reminders.fire":             "embedding_sync+embedding_background+downstream_action",
	"attention.list":             "none",
	"attention.counts":           "none",
	"attention.get":              "none",
	"attention.create":           "web_push",
	"attention.read":             "none",
	"attention.unread":           "none",
	"attention.snooze":           "none",
	"attention.resolve":          "none",
	"attention.dismiss":          "none",
	"attachments.list":           "none",
	"attachments.get":            "none",
	"attachments.download":       "none",
	"attachments.text":           "none",
	"attachments.upload":         "none",
	"attachments.extract":        "extraction_provider+embedding_sync",
	"attachments.delete":         "none",
	"attachments.forEntry":       "none",
	"attachments.attach":         "embedding_background",
	"attachments.detach":         "embedding_background",
	"webhooks.list":              "none",
	"webhooks.get":               "none",
	"webhooks.create":            "webhook_http",
	"webhooks.update":            "webhook_http",
	"webhooks.delete":            "none",
	"webhooks.deliveries":        "none",
	"webhooks.test":              "webhook_http",
	"events.recent":              "none",
	"events.stream":              "none",
	"events.wait":                "none",
	"events.resourceHealth":      "none",
	"observability.stats":        "none",
	"observability.timeline":     "none",
	"observability.stale":        "none",
}

var providerTokens = map[string]bool{
	"none": true, "embedding_sync": true, "embedding_background": true,
	"embedding_for_semantic_hybrid": true, "embedding_requires_review": true,
	"extraction_provider": true, "web_push": true, "webhook_http": true,
	"external_delivery_verification": true, "downstream_executor": true,
	"downstream_action": true, "downstream_if_runnable": true,
}

// egressCall matches the service-layer mechanisms that reach a provider:
// synchronous embedding, background embedding refresh, entry Save/Update
// (which embed), and Web Push enqueue.
var egressCall = regexp.MustCompile(`indexEmbeddingsForEntry\(|scheduleEmbeddingRefresh\(|\bbrain\.(Save|Update)\(ctx|\bs\.(Save|Update)\(ctx|\bpush\.Enqueue\(`)

// reviewedEgress maps every internal/service function that directly reaches an
// egress mechanism to the public operations reaching it and the provider token
// they must carry. Entries with no operations are reviewed non-SDK paths. A new
// function reaching a provider fails the test until it is reviewed here.
var reviewedEgress = map[string][]struct{ op, token string }{
	"AttachmentServiceImpl.updateEntryAttachments":  {{"attachments.attach", "embedding_background"}, {"attachments.detach", "embedding_background"}},
	"AttentionDispatcher.deliver":                   {{"attention.create", "web_push"}},
	"AutomationService.createRunAudit":              {{"automations.run", "embedding_sync"}},
	"AutomationService.createTask":                  {{"automations.run", "embedding_sync"}, {"automations.run", "embedding_background"}},
	"BrainServiceImpl.AttachmentDerivedTextChanged": {{"attachments.extract", "embedding_sync"}},
	"BrainServiceImpl.BulkUpdate":                   {{"entries.bulkUpdate", "embedding_background"}},
	"BrainServiceImpl.EnsureBrainMergeRequest":      nil, // no production caller
	"BrainServiceImpl.Save":                         {{"entries.create", "embedding_sync"}, {"goals.create", "embedding_sync"}, {"reminders.create", "embedding_sync"}},
	"BrainServiceImpl.Update":                       {{"entries.update", "embedding_background"}, {"goals.update", "embedding_background"}, {"reminders.update", "embedding_background"}},
	"BrainServiceImpl.createFeatureScheduleGate":    {{"entries.create", "embedding_sync"}},       // reached from Save
	"BrainServiceImpl.injectGateDependency":         {{"entries.create", "embedding_sync"}},       // reached from Save
	"BrainServiceImpl.updateFeatureScheduleGate":    {{"entries.update", "embedding_background"}}, // reached from Update
	"BrainServiceImpl.scheduleEmbeddingRefresh":     nil,                                          // the mechanism itself
	"BrainServiceImpl.syncDurableFieldsToFile":      {{"entries.updateMetadata", "embedding_background"}},
	"BulkJobService.apply":                          nil, // bulk-jobs API is not in the SDK contract
	"EnsureBuiltInFeatureCheckoutAutomation":        nil, // startup built-in automation registration
	"EnsureBuiltInFeatureCheckoutSimpleAutomation":  nil,
	"EnsureBuiltInFeatureDeliveryAutomation":        nil,
	"GoalService.CreateGoal":                        {{"goals.create", "embedding_sync"}},
	"GoalService.Reconcile":                         {{"goals.run", "embedding_background"}},
	"GoalService.UpdateGoal":                        {{"goals.update", "embedding_background"}},
	"GoalService.generateGoalTask":                  {{"goals.run", "embedding_sync"}},
	"GoalService.mirrorAudit":                       {{"goals.run", "embedding_background"}},
	"MonitorServiceImpl.Create":                     nil, // monitors are not in the SDK contract
	"MonitorServiceImpl.CreateForFeature":           nil,
	"MonitorServiceImpl.Toggle":                     nil,
	"ReminderService.CreateReminder":                {{"reminders.create", "embedding_sync"}},
	"ReminderService.UpdateReminder":                {{"reminders.update", "embedding_background"}},
	"ReminderService.actionTask":                    {{"reminders.fire", "embedding_sync"}},
	"ReminderService.fire":                          {{"reminders.fire", "embedding_background"}},
}

func TestOperationPolicyProviderEffectsArePinnedAndDerived(t *testing.T) {
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
	if strings.Join(policy.EventFanout, ",") != "webhook_http_for_subscribed_events,event_triggered_automations" {
		t.Errorf("event_fanout must declare webhook and automation fan-out, got %v", policy.EventFanout)
	}
	if len(reviewedProviderEffects) != 105 || len(policy.Operations) != 105 {
		t.Fatalf("pinned=%d policy=%d, want 105", len(reviewedProviderEffects), len(policy.Operations))
	}
	for op, want := range reviewedProviderEffects {
		row := policy.Operations[op]
		if len(row) != 5 || row[4] != want {
			t.Errorf("%s provider=%v, reviewed=%q", op, row, want)
		}
		parts := strings.Split(want, "+")
		for _, p := range parts {
			if !providerTokens[p] || (p == "none" && len(parts) > 1) {
				t.Errorf("%s: invalid provider token %q", op, p)
			}
		}
	}
	files, err := filepath.Glob("../service/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("service sources: %v", err)
	}
	var found []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !egressCall.Match(src[fs.Position(fn.Body.Pos()).Offset:fs.Position(fn.Body.End()).Offset]) {
				continue
			}
			name := fn.Name.Name
			if fn.Recv != nil {
				recv := fn.Recv.List[0].Type
				if star, ok := recv.(*ast.StarExpr); ok {
					recv = star.X
				}
				if id, ok := recv.(*ast.Ident); ok {
					name = id.Name + "." + name
				}
			}
			found = append(found, name)
		}
	}
	sort.Strings(found)
	var reviewed []string
	for name := range reviewedEgress {
		reviewed = append(reviewed, name)
	}
	sort.Strings(reviewed)
	if strings.Join(found, "\n") != strings.Join(reviewed, "\n") {
		t.Fatalf("service egress functions changed; review api/operation-policy.yaml provider rows.\nfound:\n%s\nreviewed:\n%s", strings.Join(found, "\n"), strings.Join(reviewed, "\n"))
	}
	for name, uses := range reviewedEgress {
		for _, use := range uses {
			if !strings.Contains("+"+policy.Operations[use.op][4]+"+", "+"+use.token+"+") {
				t.Errorf("%s reaches %s from %s but its provider column %q omits it", use.op, use.token, name, policy.Operations[use.op][4])
			}
		}
	}
}
