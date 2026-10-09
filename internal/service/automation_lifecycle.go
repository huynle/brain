package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/types"
)

// SetClock injects the clock that lifecycle decisions (starts_at, expires_at,
// max_runs) and audit timestamps read. Nil restores types.TimeNowUTC.
func (s *AutomationService) SetClock(now func() time.Time) {
	if s == nil {
		return
	}
	s.now = now
}

// clock returns the current instant from the injected clock. It is nil-safe,
// and the default reads types.TimeNowUTC lazily so overrides still apply.
func (s *AutomationService) clock() time.Time {
	if s == nil || s.now == nil {
		return types.TimeNowUTC()
	}
	return s.now()
}

// lifecycleAllows reports whether an automation may fire for project right
// now. A false result is silent: expiry is recorded on the entry itself, and
// a not-yet-started automation writes nothing (the audit would repeat every
// tick). Manual runs never reach this gate.
func (s *AutomationService) lifecycleAllows(ctx context.Context, automation types.BrainEntry, project string) (bool, error) {
	return s.lifecycleAllowsAt(ctx, automation, project, time.Time{})
}

// lifecycleAllowsAt is lifecycleAllows for one firing. A non-zero slot is the
// scheduled instant being fired, and a max_runs skip records it.
func (s *AutomationService) lifecycleAllowsAt(ctx context.Context, automation types.BrainEntry, project string, slot time.Time) (bool, error) {
	expired, err := s.expireIfDue(ctx, automation)
	if err != nil {
		return false, err
	}
	if expired {
		return false, nil
	}
	if automationNotStarted(automation, s.clock()) {
		return false, nil
	}
	reached, err := s.maxRunsReachedAt(ctx, automation, project, slot)
	if err != nil {
		return false, err
	}
	return !reached, nil
}

// maxRunsPageSize bounds each page read while counting run audits.
const maxRunsPageSize = 200

// maxRunsReachedAt reports whether max_runs is exhausted for (automation,
// project). A project-owned automation is completed with a note. A global one
// only stops for this project, which gets a single skip audit until the
// project's latest audit changes. slot is the scheduled instant being fired,
// or zero.
func (s *AutomationService) maxRunsReachedAt(ctx context.Context, automation types.BrainEntry, project string, slot time.Time) (bool, error) {
	if automation.MaxRuns == nil || *automation.MaxRuns <= 0 {
		return false, nil
	}
	limit := *automation.MaxRuns
	count, err := countAutomationRuns(ctx, s.brain, automation.ID, project, limit)
	if err != nil {
		return false, err
	}
	if count < limit {
		return false, nil
	}
	if automation.ProjectID != "" && automation.Binding == "" {
		return true, s.completeAtMaxRuns(ctx, automation.ID, limit, count)
	}
	return true, s.recordMaxRunsSkip(ctx, automation, project, slot)
}

// runAuditLister is the read the run-audit counter needs. BrainServiceImpl
// and the timeline's entry lister both satisfy it.
type runAuditLister interface {
	List(context.Context, types.ListEntriesRequest) (*types.ListEntriesResponse, error)
}

// countAutomationRuns counts the run audits of (automation, project) that
// created work, stopping once stopAt is reached. Skipped and manual audits
// never count.
func countAutomationRuns(ctx context.Context, lister runAuditLister, automationID, project string, stopAt int) (int, error) {
	count := 0
	for offset := 0; ; offset += maxRunsPageSize {
		resp, err := lister.List(ctx, types.ListEntriesRequest{
			Type:    "automation_run",
			Project: project,
			Tags:    "automation:" + automationID,
			Limit:   maxRunsPageSize,
			Offset:  offset,
		})
		if err != nil {
			return 0, fmt.Errorf("count automation runs: %w", err)
		}
		if resp == nil {
			return count, nil
		}
		for _, audit := range resp.Entries {
			if runAuditCreatedWork(audit) {
				count++
				if count >= stopAt {
					return count, nil
				}
			}
		}
		if len(resp.Entries) < maxRunsPageSize {
			return count, nil
		}
	}
}

// runAuditCreatedWork reports whether a run audit represents a run that
// created work: queued, success or failed, and not a manual run.
func runAuditCreatedWork(audit types.BrainEntry) bool {
	switch audit.Status {
	case "queued", "success", "failed":
	default:
		return false
	}
	for _, tag := range audit.Tags {
		if tag == runAuditManualTag {
			return false
		}
	}
	return true
}

// completeAtMaxRuns completes a project-owned automation whose max_runs is
// used up. It is guarded, so a concurrent change to max_runs or status wins.
func (s *AutomationService) completeAtMaxRuns(ctx context.Context, id string, limit, count int) error {
	note := fmt.Sprintf("max_runs reached (%d/%d)", count, limit)
	return s.updateAutomationGuarded(ctx, id, func(current types.BrainEntry) *types.UpdateEntryRequest {
		if current.Status != "active" || current.MaxRuns == nil || *current.MaxRuns != limit {
			return nil
		}
		status := "completed"
		return &types.UpdateEntryRequest{Status: &status, Note: &note}
	})
}

// recordMaxRunsSkip writes one max_runs skip audit for (automation, project),
// unless the project's latest audit already is that skip. A scheduled slot is
// recorded on the audit, so the skip also counts as that slot's handling.
func (s *AutomationService) recordMaxRunsSkip(ctx context.Context, automation types.BrainEntry, project string, slot time.Time) error {
	latest, err := s.listRunAudits(ctx, project, automation.ID, 1)
	if err != nil {
		return err
	}
	if len(latest) > 0 && strings.Contains(latest[0].Content, "skip_reason: max_runs\n") {
		return nil
	}
	_, err = s.createRunAudit(ctx, automationRunAudit{
		automation:   automation,
		evt:          types.Event{ProjectID: project},
		project:      project,
		status:       "skipped",
		skipReason:   "max_runs",
		scheduledFor: slot,
	})
	return err
}

// expireIfDue completes an active automation whose expires_at has passed.
// It reports whether the automation is expired, so the caller does not fire
// it. Goal automations are excluded: the goal loop owns their status.
func (s *AutomationService) expireIfDue(ctx context.Context, automation types.BrainEntry) (bool, error) {
	if isGoalAutomation(automation) || automation.ExpiresAt == "" {
		return false, nil
	}
	now := s.clock()
	if expiredCompletion(automation, now) == nil {
		return false, nil
	}
	if automation.Binding != "" {
		// An effective config names its binding. Its expiry may be the parent's
		// or the binding's own: the sweep over active entries completes the
		// entry that owns it. Here the firing is only refused, never written to
		// the parent.
		return true, nil
	}
	if err := s.updateAutomationGuarded(ctx, automation.ID, func(current types.BrainEntry) *types.UpdateEntryRequest {
		return expiredCompletion(current, now)
	}); err != nil {
		return true, fmt.Errorf("expire automation %s: %w", automation.ID, err)
	}
	return true, nil
}

// automationNotStarted reports whether starts_at lies in the future.
func automationNotStarted(automation types.BrainEntry, now time.Time) bool {
	if automation.StartsAt == "" {
		return false
	}
	start, err := time.Parse(time.RFC3339, automation.StartsAt)
	if err != nil {
		return false
	}
	return now.Before(start)
}

// expiredCompletion returns the write that completes an active automation
// whose expires_at is before now, or nil when nothing should change. It is
// evaluated against the entry as it currently stands, so a concurrent edit
// that moved expires_at forward or changed the status turns it into a no-op.
func expiredCompletion(current types.BrainEntry, now time.Time) *types.UpdateEntryRequest {
	if current.Status != "active" || current.ExpiresAt == "" {
		return nil
	}
	expires, err := time.Parse(time.RFC3339, current.ExpiresAt)
	if err != nil || !now.After(expires) {
		return nil
	}
	status := "completed"
	note := fmt.Sprintf("Expired: expires_at passed (%s)", current.ExpiresAt)
	return &types.UpdateEntryRequest{Status: &status, Note: &note}
}

// updateAutomationGuarded applies build's update to automation id, guarded by
// the revision build saw. build runs on the freshly read entry and returns nil
// when nothing should change. A conflicting concurrent write causes one
// re-read and re-check. A second conflict is returned, never forced.
func (s *AutomationService) updateAutomationGuarded(ctx context.Context, id string, build func(current types.BrainEntry) *types.UpdateEntryRequest) error {
	const attempts = 2
	for attempt := 0; attempt < attempts; attempt++ {
		current, err := s.brain.Recall(ctx, id)
		if err != nil {
			return err
		}
		req := build(*current)
		if req == nil {
			return nil
		}
		req.ExpectedRevision = current.Revision
		_, err = s.brain.Update(ctx, current.Path, *req)
		if err == nil {
			return nil
		}
		if !errors.Is(err, api.ErrConflict) {
			return err
		}
	}
	return fmt.Errorf("automation %s changed during a lifecycle update: %w", id, api.ErrConflict)
}
