package sdkcontract

import (
	"os"
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
	"entries.create":             "embedding_sync+embedding_background+downstream_if_runnable",
	"entries.update":             "embedding_sync+embedding_background+downstream_if_runnable",
	"entries.updateMetadata":     "embedding_sync+embedding_background+downstream_if_runnable",
	"entries.move":               "none",
	"entries.delete":             "none",
	"entries.bulkUpdate":         "embedding_sync+embedding_background+downstream_if_runnable",
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
	"goals.create":               "embedding_sync+embedding_background+downstream_executor",
	"goals.update":               "embedding_sync+embedding_background+downstream_executor",
	"goals.delete":               "none",
	"goals.run":                  "embedding_sync+embedding_background+downstream_executor",
	"automations.run":            "embedding_sync+embedding_background+downstream_executor",
	"automations.runs":           "none",
	"automations.getRun":         "none",
	"reminders.list":             "none",
	"reminders.get":              "none",
	"reminders.create":           "embedding_sync+embedding_background+web_push+downstream_action",
	"reminders.update":           "embedding_sync+embedding_background+web_push+downstream_action",
	"reminders.delete":           "none",
	"reminders.ack":              "none",
	"reminders.snooze":           "embedding_sync+embedding_background+web_push+downstream_action",
	"reminders.fire":             "embedding_sync+embedding_background+web_push+downstream_action",
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
	"attachments.attach":         "embedding_sync+embedding_background",
	"attachments.detach":         "embedding_sync+embedding_background",
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
	// Runner/dispatch/control/scheduler (hosted MCP step 2). Resuming a dial
	// releases queued work to executors; prompting or granting a permission
	// drives a remote agent (tools + model spend). Pausing, aborting, spawning
	// an idle instance and killing one reach no provider themselves.
	"runners.status":                    "none",
	"runners.list":                      "none",
	"runners.get":                       "none",
	"runners.instances":                 "none",
	"runners.allInstances":              "none",
	"dispatch.pauseAll":                 "none",
	"dispatch.resumeAll":                "downstream_executor",
	"dispatch.pauseProject":             "none",
	"dispatch.resumeProject":            "downstream_executor",
	"dispatch.pauseFeature":             "none",
	"dispatch.resumeFeature":            "downstream_executor",
	"dispatch.pauseProjectAutomations":  "none",
	"dispatch.resumeProjectAutomations": "downstream_executor",
	"tasks.dispatchLease":               "none",
	"tasks.placementReasons":            "none",
	"scheduler.status":                  "none",
	"control.sendPrompt":                "downstream_executor",
	"control.abortSession":              "none",
	"control.respondPermission":         "downstream_executor",
	"control.spawnInstance":             "none",
	"control.killInstance":              "none",
	// Hosted MCP step 3. A monitor is a scheduled or feature-gated task that
	// runs an agent when it fires (creation also indexes the task entry). A
	// supervisor operation prompts an agent, injects/relaunches a task or
	// triggers one. Sync reconcile queues a browser command that writes later
	// through the ordinary entry-sync path; nothing else reaches a provider.
	"monitors.create":                "embedding_sync+embedding_background+downstream_executor",
	"monitors.deleteByScope":         "none",
	"tasks.runnerCandidates":         "none",
	"tasks.proposedRunnerCandidates": "none",
	"features.runnerCandidates":      "none",
	"clientContext.resolve":          "none",
	"sync.devices":                   "none",
	"sync.diff":                      "none",
	"sync.reconcile":                 "none",
	"control.sessionTail":            "none",
	"control.sessionDescendants":     "none",
	"supervision.capabilities":       "none",
	"supervision.snapshot":           "none",
	"supervision.dispatchPreview":    "none",
	"supervision.submitOperation":    "downstream_executor",
	"supervision.getOperation":       "none",
	"supervision.checkpoints":        "none",
	"supervision.updateCheckpoint":   "none",
	"supervision.budget":             "none",
	"supervision.updateBudget":       "none",
}

var providerTokens = map[string]bool{
	"none": true, "embedding_sync": true, "embedding_background": true,
	"embedding_for_semantic_hybrid": true, "embedding_requires_review": true,
	"extraction_provider": true, "web_push": true, "webhook_http": true,
	"external_delivery_verification": true, "downstream_executor": true,
	"downstream_action": true, "downstream_if_runnable": true,
}

func TestOperationPolicyProviderEffectsArePinned(t *testing.T) {
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
	if len(reviewedProviderEffects) != 146 || len(policy.Operations) != 146 {
		t.Fatalf("pinned=%d policy=%d, want 146", len(reviewedProviderEffects), len(policy.Operations))
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
}
