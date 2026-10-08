package api

import (
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/sdk/brain"
)

// Discovery does not widen the sealed tenant route surface or grant operations.
func registerSDKCapabilities(r chi.Router, cfg config.Config, h *Handler) {
	if cfg.Tenancy.Mode != "" && cfg.Tenancy.Mode != tenant.ModeSingle {
		return
	}
	manifest := brain.CapabilityManifest{ContractVersion: brain.ContractVersion, Operations: sdkOperations(h)}
	// There is no production script runtime, configuration, admitted deployment
	// or dedicated caller permission in this composition. Each dimension is false
	// independently; admin/mcp scopes do not imply script:execute.
	r.With(RequireScope("admin:*", "runner:*", "read:*")).Get("/capabilities", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		WriteJSON(w, http.StatusOK, manifest)
	})
}

// Only wired public-contract operations are advertised. Service presence is not
// readiness of an external provider, resource existence, or caller authorization.
func sdkOperations(h *Handler) []string {
	ops := []string{"capabilities.get", "health.get"}
	if h == nil {
		return ops
	}
	add := func(enabled bool, ids string) {
		if enabled {
			ops = append(ops, strings.Fields(ids)...)
		}
	}
	add(h.brain != nil, `entries.list entries.create entries.get entries.update entries.updateMetadata entries.delete entries.move entries.bulkUpdate entries.bulkDelete search.query search.inject sections.list sections.get graph.backlinks graph.outlinks graph.related graph.orphans observability.stats observability.stale`)
	add(h.attachments != nil, `attachments.list attachments.upload attachments.get attachments.delete attachments.download attachments.text attachments.extract attachments.forEntry attachments.attach attachments.detach`)
	add(h.tasks != nil, `tasks.list tasks.get tasks.ready tasks.next tasks.waiting tasks.blocked tasks.status tasks.metadata tasks.claimStatus tasks.delivery tasks.verifyDelivery tasks.resume tasks.resumeWithContext tasks.assign tasks.clearAssignment tasks.trigger tasks.dispatch features.list features.ready features.get features.resume features.resumeWithContext features.assign features.clearAssignment features.checkout projects.list`)
	add(h.tasks != nil && h.brain != nil, `projects.delete`)
	add(h.tasks != nil && h.runTask != nil, `tasks.run`)
	add(h.tasks != nil && h.runFeature != nil, `features.run`)
	add(h.tasks != nil && h.depChains != nil, `features.cancel features.chains`)
	add(h.tasks != nil && h.runProject != nil, `projects.run`)
	add(h.logBuffer != nil, `tasks.logs`)
	add(h.events != nil, `events.stream events.recent events.wait events.resourceHealth`)
	add(h.timeline != nil, `observability.timeline`)
	add(h.placement != nil, `projects.getPlacement projects.setPlacement`)
	add(h.goalService != nil, `goals.list goals.create goals.update goals.delete goals.progress goals.audit goals.run`)
	add(h.reminders != nil, `reminders.list reminders.create reminders.get reminders.update reminders.delete reminders.ack reminders.snooze reminders.fire`)
	add(h.attention != nil, `attention.list attention.create attention.get attention.counts attention.read attention.unread attention.snooze attention.resolve attention.dismiss`)
	add(h.webhooks != nil, `webhooks.list webhooks.create webhooks.get webhooks.update webhooks.delete webhooks.deliveries webhooks.test`)
	add(h.automationRun != nil, `automations.run automations.runs automations.getRun`)
	add(h.runner != nil, `runners.status dispatch.pauseAll dispatch.resumeAll dispatch.pauseProject dispatch.resumeProject dispatch.pauseFeature dispatch.resumeFeature dispatch.pauseProjectAutomations dispatch.resumeProjectAutomations`)
	add(h.runnerRegistry != nil, `runners.list runners.get runners.instances runners.allInstances`)
	add(h.schedulerViews != nil, `tasks.dispatchLease tasks.placementReasons`)
	add(h.scheduler != nil, `scheduler.status`)
	// Remote control is advertised when the bridge is wired; it still requires
	// control:* per request and never implies script execution.
	add(h.bridge != nil, `control.sendPrompt control.abortSession control.respondPermission control.spawnInstance control.killInstance`)
	slices.Sort(ops)
	return ops
}
