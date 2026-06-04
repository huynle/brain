package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// ErrGoalNotFound is returned by the goal API when no active goal automation
// matches the requested goal ID. Aliases types.ErrGoalNotFound so both api and
// service callers share one sentinel.
var ErrGoalNotFound = types.ErrGoalNotFound

// ReconcileDecision is the outcome of the deterministic reconcile decision.
//
// The reconcile core inspects a goal's config and its linked task states and
// produces exactly one of these decisions. The decision is pure (no I/O, no
// LLM) and deterministic given the same inputs. It aliases types.ReconcileDecision
// so the goal API contract lives in the shared types package.
type ReconcileDecision = types.ReconcileDecision

const (
	// ReconcileComplete means every linked task counts as complete; the goal
	// is satisfied and can be marked done.
	ReconcileComplete = types.ReconcileComplete
	// ReconcileBlock means linked work is blocked with nothing active; the
	// goal is stuck and should surface as blocked.
	ReconcileBlock = types.ReconcileBlock
	// ReconcileNeedWork means more work must be generated (no linked tasks, or
	// only un-started pending work remains with nothing active/blocked).
	ReconcileNeedWork = types.ReconcileNeedWork
	// ReconcileNoop means work is already in progress; the reconcile loop
	// should do nothing and wait.
	ReconcileNoop = types.ReconcileNoop
)

// LinkedTaskSnapshot is a serializable snapshot of a goal's linked task,
// captured for the reconcile audit record. Aliases types.LinkedTaskSnapshot.
type LinkedTaskSnapshot = types.LinkedTaskSnapshot

// linkedTaskSnapshot maps linked tasks to their serializable snapshot form.
//
// It always returns a non-nil slice (empty for empty/nil input) so the result
// marshals as a JSON array ([]) rather than null in the audit record.
func linkedTaskSnapshot(tasks []types.ResolvedTask) []LinkedTaskSnapshot {
	out := make([]LinkedTaskSnapshot, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, LinkedTaskSnapshot{
			ID:     t.ID,
			Title:  t.Title,
			Status: t.Status,
		})
	}
	return out
}

// statusSet builds a membership set from a list of statuses, falling back to
// the provided defaults when the list is empty.
func statusSet(statuses, fallback []string) map[string]bool {
	src := statuses
	if len(src) == 0 {
		src = fallback
	}
	set := make(map[string]bool, len(src))
	for _, s := range src {
		set[s] = true
	}
	return set
}

// decideReconcile computes the reconcile decision from goal config + linked
// task states. It is deterministic, with no I/O and no LLM. It honors
// cfg.CompleteStatuses and cfg.BlockedStatuses, falling back to
// defaultCompleteStatuses / defaultBlockedStatuses when those are empty.
//
// Decision precedence (fixed, evaluated in this exact order):
//  1. need_work  — total == 0 (no linked tasks; work must be generated)
//  2. complete   — total > 0 && completed == total (all linked tasks complete)
//  3. noop       — inProgress > 0 (work active; nothing to do)
//  4. block      — blocked > 0 (blocked work, none in progress)
//  5. need_work  — otherwise (pending work remains with no active task)
func decideReconcile(cfg types.GoalConfig, tasks []types.ResolvedTask) (ReconcileDecision, string) {
	completeSet := statusSet(cfg.CompleteStatuses, defaultCompleteStatuses)
	blockedSet := statusSet(cfg.BlockedStatuses, defaultBlockedStatuses)

	total := len(tasks)
	var completed, blocked, inProgress, pending int
	for _, t := range tasks {
		if completeSet[t.Status] {
			completed++
		}
		if blockedSet[t.Status] {
			blocked++
		}
		switch t.Status {
		case "in_progress":
			inProgress++
		case "pending":
			pending++
		}
	}

	switch {
	case total == 0:
		return ReconcileNeedWork, "no linked tasks; work must be generated"
	case completed == total:
		return ReconcileComplete, fmt.Sprintf("all %d linked task(s) complete", total)
	case inProgress > 0:
		return ReconcileNoop, fmt.Sprintf("%d task(s) in progress; nothing to do", inProgress)
	case blocked > 0:
		return ReconcileBlock, fmt.Sprintf("%d task(s) blocked, none in progress", blocked)
	default:
		return ReconcileNeedWork, fmt.Sprintf("pending work remains with no active task (%d pending of %d)", pending, total)
	}
}

// =============================================================================
// Reconcile orchestration
// =============================================================================

// GoalReconcileAudit is the auditable record of a single reconcile decision.
//
// It captures the inputs (triggering event, linked task snapshot) and the
// outputs (decision, reason, optionally generated task) of a deterministic
// reconcile so the decision can be replayed, inspected, and mirrored onto the
// goal entry. Aliases types.GoalReconcileAudit so the goal API contract lives
// in the shared types package.
type GoalReconcileAudit = types.GoalReconcileAudit

// GoalService orchestrates the deterministic in-process reconcile loop for a
// single goal automation entry: it computes the decision, generates runner
// work when needed, persists an audit record, and mirrors the audit onto the
// goal entry.
//
// It also acts as an EventHub event handler: Start subscribes to the hub and
// HandleEvent drives the reconcile for every goal automation whose trigger
// matches an incoming task/feature lifecycle event.
type GoalService struct {
	brain *BrainServiceImpl
	tasks FeatureTaskLister
	store *storage.StorageLayer
}

// NewGoalService constructs a GoalService from its collaborators.
func NewGoalService(brain *BrainServiceImpl, tasks FeatureTaskLister, store *storage.StorageLayer) *GoalService {
	return &GoalService{brain: brain, tasks: tasks, store: store}
}

// Reconcile computes the deterministic decision for a goal automation entry
// against its linked tasks, persists an audit record, mirrors the audit to the
// goal entry, and (when need_work) generates a runner task.
//
// The audit InsertEvent is treated as critical (its failure returns an error).
// Entry mirroring (metadata + notes) is best-effort and never fails Reconcile.
func (s *GoalService) Reconcile(ctx context.Context, goal types.BrainEntry, evt types.Event) (*GoalReconcileAudit, error) {
	if goal.Goal == nil {
		return nil, fmt.Errorf("goal reconcile: entry %q is not a goal automation (Goal config is nil)", goal.ID)
	}

	// 1. Gather linked tasks (nil lister => empty list).
	var tasks []types.ResolvedTask
	if s.tasks != nil {
		var err error
		tasks, err = s.tasks.GetTasksByFeature(ctx, goal.ProjectID, goal.FeatureID)
		if err != nil {
			return nil, fmt.Errorf("goal reconcile: list tasks for feature %q: %w", goal.FeatureID, err)
		}
	}

	// 2. Decide.
	decision, reason := decideReconcile(*goal.Goal, tasks)

	// 3. Build the audit record.
	triggering := evt.Type
	if triggering == "" {
		triggering = "manual"
	}
	audit := GoalReconcileAudit{
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		GoalID:          goal.Goal.ID,
		Project:         goal.ProjectID,
		FeatureID:       goal.FeatureID,
		TriggeringEvent: triggering,
		EventID:         evt.ID,
		Decision:        decision,
		Reason:          reason,
		LinkedTasks:     linkedTaskSnapshot(tasks),
	}

	// 4. Generate work when needed (deduped while an open task exists).
	if decision == ReconcileNeedWork && goal.Action != nil {
		generatedKey := fmt.Sprintf("goal:%s:need_work", goal.Goal.ID)
		open, err := s.goalGeneratedTaskOpen(ctx, goal.ProjectID, generatedKey)
		if err != nil {
			return nil, fmt.Errorf("goal reconcile: check open generated task: %w", err)
		}
		if !open {
			id, err := s.generateGoalTask(ctx, goal, generatedKey)
			if err != nil {
				return nil, fmt.Errorf("goal reconcile: generate task: %w", err)
			}
			audit.GeneratedTaskID = id
		}
	}

	// 5. Persist the audit record (critical).
	payload, err := json.Marshal(audit)
	if err != nil {
		return nil, fmt.Errorf("goal reconcile: marshal audit: %w", err)
	}
	if s.store != nil {
		if _, err := s.store.InsertEvent(ctx, types.EventGoalReconcile, string(payload), "", "goal"); err != nil {
			return nil, fmt.Errorf("goal reconcile: persist audit: %w", err)
		}
	}

	// 6. Mirror onto the goal entry (best-effort).
	s.mirrorAudit(ctx, goal, audit, string(payload))

	return &audit, nil
}

// generateGoalTask creates a runner task entry for a need_work decision and
// returns its generated id. It mirrors the AutomationService.createTask shape.
func (s *GoalService) generateGoalTask(ctx context.Context, goal types.BrainEntry, generatedKey string) (string, error) {
	if s.brain == nil {
		return "", fmt.Errorf("brain service is nil")
	}

	generated := true
	req := types.CreateEntryRequest{
		Type:           "task",
		Title:          fmt.Sprintf("Goal: %s", goal.Title),
		Content:        goal.Action.DirectPrompt,
		Status:         "pending",
		Project:        goal.ProjectID,
		FeatureID:      goal.FeatureID,
		Generated:      &generated,
		GeneratedBy:    types.GoalGeneratedBy,
		GeneratedKey:   generatedKey,
		DirectPrompt:   goal.Action.DirectPrompt,
		Agent:          goal.Action.Agent,
		Model:          goal.Action.Model,
		ExecutionMode:  goal.Action.ExecutionMode,
		SessionMode:    goal.Action.SessionMode,
		CompleteOnIdle: goal.Action.CompleteOnIdle,
		TargetWorkdir:  goal.Goal.Workdir,
	}

	if goal.Action.Type == "script" {
		req.Executor = "script"
		req.Content = goal.Action.Command
		req.DirectPrompt = goal.Action.Command
	}

	resp, err := s.brain.Save(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.ID, nil
}

// goalGeneratedTaskOpen reports whether a goal-generated task with the given
// generated key is still open (pending/in_progress/active). Used to dedup
// need_work generation while prior work remains incomplete.
func (s *GoalService) goalGeneratedTaskOpen(ctx context.Context, project, generatedKey string) (bool, error) {
	if s.brain == nil || generatedKey == "" {
		return false, nil
	}
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   1000,
	})
	if err != nil {
		return false, fmt.Errorf("list generated tasks: %w", err)
	}
	for _, task := range resp.Entries {
		if task.GeneratedKey != generatedKey {
			continue
		}
		switch task.Status {
		case "pending", "in_progress", "active":
			return true, nil
		}
	}
	return false, nil
}

// mirrorAudit writes the audit onto the goal entry: last_reconcile metadata
// (DB-only) and a "## Reconciliation Notes" markdown section. Both are
// best-effort; failures are logged to stderr and never fail Reconcile.
func (s *GoalService) mirrorAudit(ctx context.Context, goal types.BrainEntry, audit GoalReconcileAudit, payload string) {
	if s.brain == nil || goal.ID == "" {
		return
	}

	// Append the notes section FIRST. Update re-indexes the file from disk,
	// which rewrites the DB metadata column (preserving only a fixed set of
	// runtime keys that does NOT include last_reconcile). Writing the metadata
	// after the append avoids the re-index clobbering it.
	taskLine := ""
	if audit.GeneratedTaskID != "" {
		taskLine = fmt.Sprintf(" (generated task %s)", audit.GeneratedTaskID)
	}
	note := fmt.Sprintf("## Reconciliation Notes\n\n- %s — **%s**: %s%s",
		audit.Timestamp, audit.Decision, audit.Reason, taskLine)
	if _, err := s.brain.Update(ctx, goal.ID, types.UpdateEntryRequest{Append: &note}); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: goal reconcile: mirror notes to %q: %v\n", goal.ID, err)
	}

	// last_reconcile is a DB-only audit mirror (not in durableMetadataFields,
	// so it stays out of the on-disk frontmatter).
	if _, err := s.brain.UpdateMetadata(ctx, goal.ID, map[string]interface{}{
		"last_reconcile": payload,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: goal reconcile: mirror metadata to %q: %v\n", goal.ID, err)
	}
}

// =============================================================================
// Event dispatch (EventHub handler)
// =============================================================================

// Start subscribes to the EventHub and drives the goal reconcile loop for
// every incoming event until the context is cancelled. It mirrors how
// AutomationService.Start consumes the hub: it replays buffered events on
// startup, dedupes by event ID, and routes each event through HandleEvent.
//
// Start is the wiring entrypoint registered in server.go alongside the other
// event handlers (TriggerService / AutomationService).
func (s *GoalService) Start(ctx context.Context, hub *realtime.EventHub) {
	if s == nil || hub == nil {
		return
	}

	ch, unsub := hub.Subscribe(realtime.EventFilter{})
	defer unsub()

	slog.Info("goal service event handler started")

	seen := make(map[string]struct{})
	process := func(evt types.Event) {
		if evt.ID != "" {
			if _, ok := seen[evt.ID]; ok {
				return
			}
			seen[evt.ID] = struct{}{}
		}
		if err := s.HandleEvent(ctx, evt); err != nil {
			slog.Warn("goal service: handle event error",
				"event_id", evt.ID,
				"event_type", evt.Type,
				"error", err,
			)
		}
	}

	for _, evt := range hub.Replay("") {
		process(evt)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("goal service event handler stopped")
			return
		case evt, ok := <-ch:
			if !ok {
				slog.Info("goal service: event channel closed")
				return
			}
			process(evt)
		}
	}
}

// HandleEvent reconciles every active goal automation whose trigger matches the
// incoming event. It lists active goal automation entries (type=automation,
// generated_by=brain-goal), filters those whose trigger matches the event, and
// runs the deterministic Reconcile for each match.
//
// A reconcile failure for one goal does not abort the others: errors are
// logged and the first error is returned so a single misbehaving goal cannot
// block the rest of the batch.
func (s *GoalService) HandleEvent(ctx context.Context, evt types.Event) error {
	if s == nil || s.brain == nil {
		return nil
	}

	goals, err := s.listActiveGoals(ctx)
	if err != nil {
		return fmt.Errorf("goal handle event: list goals: %w", err)
	}

	var firstErr error
	for _, goal := range goals {
		if goal.Goal == nil {
			continue
		}
		if !goalMatchesEvent(goal, evt) {
			continue
		}
		if _, err := s.Reconcile(ctx, goal, evt); err != nil {
			slog.Warn("goal service: reconcile error",
				"goal_id", goal.Goal.ID,
				"entry_id", goal.ID,
				"event_id", evt.ID,
				"event_type", evt.Type,
				"error", err,
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("reconcile goal %q: %w", goal.Goal.ID, err)
			}
		}
	}

	return firstErr
}

// listActiveGoals returns active goal automation entries (type=automation,
// generated_by=brain-goal, tagged GoalTag) with their Goal config populated.
func (s *GoalService) listActiveGoals(ctx context.Context) ([]types.BrainEntry, error) {
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:   "automation",
		Status: "active",
		Tags:   types.GoalTag,
		Limit:  1000,
	})
	if err != nil {
		return nil, err
	}

	out := make([]types.BrainEntry, 0, len(resp.Entries))
	for _, entry := range resp.Entries {
		if entry.GeneratedBy != types.GoalGeneratedBy {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}

// goalMatchesEvent reports whether a goal automation's trigger matches the
// event. Goal triggers are always type=event (built by buildGoalTrigger), so we
// match the event pattern plus the trigger filters, reusing the shared
// automation filter matcher.
func goalMatchesEvent(goal types.BrainEntry, evt types.Event) bool {
	if goal.Trigger == nil {
		return false
	}
	if !goal.Trigger.MatchesEvent(evt.Type) {
		return false
	}
	return matchAutomationFilters(goal.Trigger.Filter, evt)
}
