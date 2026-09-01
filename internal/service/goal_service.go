package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
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
	// ReconcileSteer means work was in progress and the reconcile loop
	// steered the live agent session(s) toward the goal.
	ReconcileSteer = types.ReconcileSteer
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
// work when needed, steers live agent sessions when work is already running,
// persists an audit record, and mirrors the audit onto the goal entry.
//
// It also acts as an EventHub event handler: Start subscribes to the hub and
// HandleEvent drives the reconcile for every goal automation whose trigger
// matches an incoming task/feature lifecycle event. A periodic ticker
// (goalReconcileInterval) re-checks every active goal so a goal makes
// progress even when no lifecycle event fires.
type GoalService struct {
	brain *BrainServiceImpl
	tasks FeatureTaskLister
	store *storage.StorageLayer

	// steerer delivers steering prompts into live agent sessions. Nil means
	// steering is silently disabled (existing wiring/tests keep working).
	steerer SessionSteerer
	// pauseChecker is the same runner pause gate AutomationService consults;
	// when paused, goal task generation and steering are skipped. Optional.
	pauseChecker automationPauseChecker
	// now is injectable time for steering-cooldown tests.
	now func() time.Time

	// reconcileMu serializes Reconcile per goal ID. Three callers converge on
	// Reconcile (event dispatch, the periodic ticker, and manual goal_run);
	// without this, two overlapping runs can both pass the cooldown check and
	// double-steer a session, or both miss the open generated task and create
	// duplicates.
	reconcileMu sync.Mutex
	goalLocks   map[string]*sync.Mutex
}

// lockGoal returns the per-goal mutex, creating it on first use. Goal count is
// small and goals are long-lived, so entries are never evicted.
func (s *GoalService) lockGoal(goalID string) *sync.Mutex {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	if s.goalLocks == nil {
		s.goalLocks = make(map[string]*sync.Mutex)
	}
	mu, ok := s.goalLocks[goalID]
	if !ok {
		mu = &sync.Mutex{}
		s.goalLocks[goalID] = mu
	}
	return mu
}

// GoalServiceOption customizes optional GoalService collaborators.
type GoalServiceOption func(*GoalService)

// WithGoalSteerer wires a SessionSteerer so noop-with-work-in-progress
// reconciles steer the live sessions toward the goal.
func WithGoalSteerer(steerer SessionSteerer) GoalServiceOption {
	return func(s *GoalService) { s.steerer = steerer }
}

// WithGoalPauseChecker wires the runner pause gate; when automations are
// paused, goal task generation and steering are skipped.
func WithGoalPauseChecker(checker automationPauseChecker) GoalServiceOption {
	return func(s *GoalService) { s.pauseChecker = checker }
}

// goalReconcileInterval is how often the periodic ticker re-reconciles every
// active goal. Package-level so tests can shrink it.
var goalReconcileInterval = 5 * time.Minute

// NewGoalService constructs a GoalService from its collaborators. Optional
// collaborators (steerer, pause checker) are supplied via options so existing
// call sites keep working unchanged.
func NewGoalService(brain *BrainServiceImpl, tasks FeatureTaskLister, store *storage.StorageLayer, opts ...GoalServiceOption) *GoalService {
	s := &GoalService{brain: brain, tasks: tasks, store: store, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Reconcile computes the deterministic decision for a goal automation entry
// against its linked tasks, persists an audit record, mirrors the audit to the
// goal entry, and acts on the decision: need_work generates a runner task,
// noop-with-steering nudges the live sessions, and complete terminates the
// goal (entry status -> completed).
//
// The audit InsertEvent is critical when no side effect happened yet; once a
// task was generated, an audit failure only logs a warning so the side effect
// is never reported as a failure. Entry mirroring (metadata + notes) is
// best-effort and never fails Reconcile.
func (s *GoalService) Reconcile(ctx context.Context, goal types.BrainEntry, evt types.Event) (*GoalReconcileAudit, error) {
	if goal.Goal == nil {
		return nil, fmt.Errorf("goal reconcile: entry %q is not a goal automation (Goal config is nil)", goal.ID)
	}

	// Serialize per goal: event dispatch, the periodic ticker, and manual
	// goal_run may overlap; running them concurrently defeats the steering
	// cooldown and the open-generated-task dedup.
	mu := s.lockGoal(goal.Goal.ID)
	mu.Lock()
	defer mu.Unlock()

	// 1. Gather linked tasks per the goal's scope (task > feature > project).
	tasks, err := s.linkedTasksForGoal(ctx, goal)
	if err != nil {
		return nil, fmt.Errorf("goal reconcile: list linked tasks: %w", err)
	}

	// 2. Decide.
	decision, reason := decideReconcile(*goal.Goal, tasks)

	// 3. Build the audit record.
	triggering := evt.Type
	if triggering == "" {
		triggering = "manual"
	}
	audit := GoalReconcileAudit{
		Timestamp:       s.nowUTC().Format(time.RFC3339),
		GoalID:          goal.Goal.ID,
		Project:         goal.ProjectID,
		FeatureID:       goal.FeatureID,
		TriggeringEvent: triggering,
		EventID:         evt.ID,
		Decision:        decision,
		Reason:          reason,
		LinkedTasks:     linkedTaskSnapshot(tasks),
	}

	// The runner pause gate suppresses both task generation and steering —
	// mirroring how AutomationService skips paused task generation.
	paused := s.isGoalPaused(goal)

	// 4. Generate work when needed (deduped while an open task exists).
	if decision == ReconcileNeedWork && goal.Action != nil {
		if paused {
			audit.Reason = reason + "; task generation skipped: paused"
		} else {
			generatedKey := fmt.Sprintf("goal:%s:need_work", goal.Goal.ID)
			open, err := s.goalGeneratedTaskOpen(ctx, goal.ProjectID, generatedKey)
			if err != nil {
				return nil, fmt.Errorf("goal reconcile: check open generated task: %w", err)
			}
			if !open {
				id, err := s.generateGoalTask(ctx, goal, generatedKey, tasks)
				if err != nil {
					return nil, fmt.Errorf("goal reconcile: generate task: %w", err)
				}
				audit.GeneratedTaskID = id
			}
		}
	}

	// 5. Steer live sessions when work is already in progress. Best-effort:
	// steering failures are counted as skips and never fail the reconcile.
	if decision == ReconcileNoop && s.steerer != nil && goal.Goal.SteeringEnabled() {
		switch {
		case paused:
			audit.Reason = reason + "; steering skipped: paused"
		case s.steeringCooldownElapsed(ctx, goal, s.nowUTC()):
			steered, skipped := s.steerInProgressTasks(ctx, goal, tasks)
			audit.Decision = ReconcileSteer
			audit.SessionsSteered = steered
			audit.SessionsSkipped = skipped
			audit.Reason = fmt.Sprintf("%s; steered %d live session(s), skipped %d", reason, steered, skipped)
		}
	}

	// 6. Termination: a fully complete goal flips its entry to "completed" so
	// it stops matching event dispatch (active-only) but stays visible in
	// lists and can be reactivated via PATCH status=active.
	if decision == ReconcileComplete && goal.Status == "active" && s.brain != nil && goal.ID != "" {
		completed := "completed"
		if _, err := s.brain.Update(ctx, goal.ID, types.UpdateEntryRequest{Status: &completed}); err != nil {
			return nil, fmt.Errorf("goal reconcile: mark goal completed: %w", err)
		}
		audit.Reason = reason + "; goal marked completed"
	}

	// 7. Persist the audit record. Critical unless a task was already
	// generated — then the side effect wins and the failure only warns.
	payload, err := json.Marshal(audit)
	if err != nil {
		return nil, fmt.Errorf("goal reconcile: marshal audit: %w", err)
	}
	if s.store != nil {
		if _, err := s.store.InsertEvent(ctx, types.EventGoalReconcile, string(payload), "", "goal"); err != nil {
			if audit.GeneratedTaskID == "" {
				return nil, fmt.Errorf("goal reconcile: persist audit: %w", err)
			}
			slog.Warn("goal reconcile: persist audit failed after task generation",
				"goal_id", goal.Goal.ID, "generated_task_id", audit.GeneratedTaskID, "error", err)
		}
	}

	// 8. Mirror onto the goal entry (best-effort).
	s.mirrorAudit(ctx, goal, audit, string(payload))

	return &audit, nil
}

// nowUTC returns the current UTC time via the injectable clock.
func (s *GoalService) nowUTC() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

// isGoalPaused consults the optional runner pause gate for the goal's
// project, mirroring AutomationService.isAutomationPaused.
func (s *GoalService) isGoalPaused(goal types.BrainEntry) bool {
	if s == nil || s.pauseChecker == nil {
		return false
	}
	if goal.ProjectID != "" {
		if scoped, ok := s.pauseChecker.(automationProjectPauseChecker); ok {
			// The scoped checker folds global state into its per-project
			// answer (see RunnerServiceImpl.IsAutomationsPausedForProject).
			return scoped.IsAutomationsPausedForProject(goal.ProjectID)
		}
	}
	return s.pauseChecker.IsAutomationsPaused()
}

// goalProjectTaskLister is the optional lister capability consulted for
// project-scoped goals (no task_id, no feature_id). TaskServiceImpl
// implements it.
type goalProjectTaskLister interface {
	GetTasks(ctx context.Context, projectID string) (*types.TaskListResponse, error)
}

// goalSingleTaskGetter is the optional capability consulted for task-scoped
// goals. TaskServiceImpl implements it.
type goalSingleTaskGetter interface {
	GetTask(ctx context.Context, projectID, taskID string) (*types.ResolvedTask, error)
}

// linkedTasksForGoal resolves the goal's linked tasks according to its scope:
//
//   - Goal.TaskID set   -> that single task (in any feature)
//   - entry FeatureID   -> the feature's tasks (previous behavior)
//   - otherwise         -> ALL project tasks
//
// The project fallback fixes the old featureless mismatch where
// GetTasksByFeature(project, "") linked only tasks with an EMPTY feature_id
// while the trigger matched the whole project.
func (s *GoalService) linkedTasksForGoal(ctx context.Context, goal types.BrainEntry) ([]types.ResolvedTask, error) {
	if s.tasks == nil {
		return nil, nil
	}

	// Task scope: one task, whatever feature it lives in.
	if taskID := goalTaskScope(goal.Goal); taskID != "" {
		if getter, ok := s.tasks.(goalSingleTaskGetter); ok {
			t, err := getter.GetTask(ctx, goal.ProjectID, taskID)
			if err != nil {
				// A missing scope target means no linked tasks (need_work),
				// not a reconcile failure.
				if strings.Contains(err.Error(), "not found") {
					return nil, nil
				}
				return nil, fmt.Errorf("get task %q: %w", taskID, err)
			}
			return []types.ResolvedTask{*t}, nil
		}
		// Narrow listers: fall back to scanning the feature scope.
		all, err := s.tasks.GetTasksByFeature(ctx, goal.ProjectID, goal.FeatureID)
		if err != nil {
			return nil, err
		}
		for _, t := range all {
			if t.ID == taskID {
				return []types.ResolvedTask{t}, nil
			}
		}
		return nil, nil
	}

	// Feature scope.
	if goal.FeatureID != "" {
		return s.tasks.GetTasksByFeature(ctx, goal.ProjectID, goal.FeatureID)
	}

	// Project scope: every task in the project.
	if lister, ok := s.tasks.(goalProjectTaskLister); ok {
		resp, err := lister.GetTasks(ctx, goal.ProjectID)
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, nil
		}
		return resp.Tasks, nil
	}

	// Legacy fallback for narrow listers (matches only empty feature_id).
	return s.tasks.GetTasksByFeature(ctx, goal.ProjectID, "")
}

// goalTaskScope returns the trimmed task scope of a goal config, or empty.
func goalTaskScope(cfg *types.GoalConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.TaskID)
}

// generateGoalTask creates a runner task entry for a need_work decision and
// returns its generated id. It mirrors the AutomationService.createTask shape
// (executor + git/merge propagation) and composes a goal-aware prompt so the
// agent actually sees the criteria, validation, and linked-task state instead
// of only the raw action prompt.
func (s *GoalService) generateGoalTask(ctx context.Context, goal types.BrainEntry, generatedKey string, tasks []types.ResolvedTask) (string, error) {
	if s.brain == nil {
		return "", fmt.Errorf("brain service is nil")
	}

	prompt := buildGoalTaskPrompt(goal, tasks)

	generated := true
	req := types.CreateEntryRequest{
		Type:           "task",
		Title:          fmt.Sprintf("Goal: %s", goal.Title),
		Content:        prompt,
		Status:         "pending",
		Project:        goal.ProjectID,
		FeatureID:      goal.FeatureID,
		Generated:      &generated,
		GeneratedBy:    types.GoalGeneratedBy,
		GeneratedKey:   generatedKey,
		DirectPrompt:   prompt,
		Agent:          goal.Action.Agent,
		Model:          goal.Action.Model,
		Executor:       goal.Action.Executor,
		ExecutionMode:  goal.Action.ExecutionMode,
		SessionMode:    goal.Action.SessionMode,
		CompleteOnIdle: goal.Action.CompleteOnIdle,
		TargetWorkdir:  firstNonEmpty(goal.Goal.Workdir, goal.Action.TargetWorkdir),

		// Origin provenance is deliberately NOT set. A generated task has no
		// human caller and no origin machine — stamping this process's
		// identity would be stamping the API host, pinning server-generated
		// work to the API box. Left empty, machine_affinity resolves to
		// "none" and placement behaves exactly as it did before origin
		// tracking existed.

		// Git / merge settings flow from the goal entry onto the generated
		// task, exactly as AutomationService.createTask does — without this
		// the structured fields the executor reads when merging were empty.
		Workdir:            goal.Workdir,
		GitRemote:          goal.GitRemote,
		MergeTargetBranch:  goal.MergeTargetBranch,
		MergePolicy:        goal.MergePolicy,
		MergeStrategy:      goal.MergeStrategy,
		RemoteBranchPolicy: goal.RemoteBranchPolicy,
		OpenPRBeforeMerge:  goal.OpenPRBeforeMerge,
		CheckoutMode:       goal.CheckoutMode,
	}

	if types.NormalizeAutomationActionType(goal.Action.Type) == types.AutomationActionScript {
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

// buildGoalTaskPrompt composes the generated task's prompt: the goal action's
// DirectPrompt (if any), a "## Goal" section with title/criteria/validation,
// a linked-task status snapshot, and an explicit instruction that the task
// exists to achieve the goal and must end with a self-assessment against the
// criteria.
func buildGoalTaskPrompt(goal types.BrainEntry, tasks []types.ResolvedTask) string {
	cfg := goal.Goal
	var b strings.Builder

	if goal.Action != nil && strings.TrimSpace(goal.Action.DirectPrompt) != "" {
		b.WriteString(strings.TrimSpace(goal.Action.DirectPrompt))
		b.WriteString("\n\n")
	}

	b.WriteString("## Goal\n\n")
	fmt.Fprintf(&b, "**%s**", goal.Title)
	if cfg != nil && cfg.ID != "" {
		fmt.Fprintf(&b, " (goal id: %s)", cfg.ID)
	}
	b.WriteString("\n")
	if cfg != nil && strings.TrimSpace(cfg.Criteria) != "" {
		b.WriteString("\n### Success criteria\n\n")
		b.WriteString(strings.TrimSpace(cfg.Criteria))
		b.WriteString("\n")
	}
	if cfg != nil && strings.TrimSpace(cfg.Validation) != "" {
		b.WriteString("\n### Validation\n\n")
		b.WriteString(strings.TrimSpace(cfg.Validation))
		b.WriteString("\n")
	}

	b.WriteString("\n### Linked task status\n\n")
	if len(tasks) == 0 {
		b.WriteString("- (no linked tasks yet)\n")
	} else {
		for _, t := range tasks {
			fmt.Fprintf(&b, "- %s — %s (%s)\n", t.ID, t.Title, t.Status)
		}
	}

	b.WriteString("\nThis task exists to achieve the goal above. ")
	b.WriteString("When you finish, end with a self-assessment against each success criterion, ")
	b.WriteString("stating explicitly whether the goal is met and what (if anything) remains.")
	return b.String()
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

	// Capture the steering cooldown stamp BEFORE the notes append: the append
	// re-indexes the file, and last_steered_at is not a preserved runtime key,
	// so the pre-append DB value is about to be clobbered. It is re-written
	// below together with last_reconcile.
	steeredAt := ""
	if audit.Decision == ReconcileSteer && audit.SessionsSteered > 0 {
		steeredAt = audit.Timestamp
	} else if prev, ok := s.lastSteeredAt(ctx, goal.ID); ok {
		steeredAt = prev.UTC().Format(time.RFC3339)
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

	// last_reconcile / last_steered_at are DB-only audit mirrors (not in
	// durableMetadataFields, so they stay out of the on-disk frontmatter).
	fields := map[string]interface{}{
		"last_reconcile": payload,
	}
	if steeredAt != "" {
		fields[lastSteeredAtMetadataKey] = steeredAt
	}
	if _, err := s.brain.UpdateMetadata(ctx, goal.ID, fields); err != nil {
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
// A periodic ticker (goalReconcileInterval) additionally re-reconciles every
// active goal so steering and stale-state recovery happen even when no
// lifecycle event fires.
//
// Start is the wiring entrypoint registered in server.go alongside the other
// event handlers (TriggerService / AutomationService).
func (s *GoalService) Start(ctx context.Context, hub *realtime.EventHub) {
	if s == nil || hub == nil {
		return
	}

	ch, unsub := hub.Subscribe(realtime.EventFilter{})
	defer unsub()
	ticker := time.NewTicker(goalReconcileInterval)
	defer ticker.Stop()

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
		case <-ticker.C:
			if err := s.ReconcileAllActive(ctx); err != nil {
				slog.Warn("goal service: periodic reconcile error", "error", err)
			}
		case evt, ok := <-ch:
			if !ok {
				slog.Info("goal service: event channel closed")
				return
			}
			process(evt)
		}
	}
}

// ReconcileAllActive reconciles every active goal automation through the
// standard Reconcile path with a synthetic "periodic" trigger. A failure for
// one goal does not abort the others; the first error is returned.
func (s *GoalService) ReconcileAllActive(ctx context.Context) error {
	if s == nil || s.brain == nil {
		return nil
	}

	goals, err := s.listActiveGoals(ctx)
	if err != nil {
		return fmt.Errorf("goal periodic reconcile: list goals: %w", err)
	}

	var firstErr error
	for _, goal := range goals {
		if goal.Goal == nil {
			continue
		}
		if _, err := s.Reconcile(ctx, goal, types.Event{Type: goalPeriodicTrigger}); err != nil {
			slog.Warn("goal service: periodic reconcile error",
				"goal_id", goal.Goal.ID,
				"entry_id", goal.ID,
				"error", err,
			)
			if firstErr == nil {
				firstErr = fmt.Errorf("reconcile goal %q: %w", goal.Goal.ID, err)
			}
		}
	}
	return firstErr
}

// goalPeriodicTrigger is the synthetic triggering-event label the periodic
// ticker stamps into reconcile audits.
const goalPeriodicTrigger = "periodic"

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
// Event dispatch and the periodic ticker use this — paused (blocked),
// completed, and archived goals never reconcile automatically.
func (s *GoalService) listActiveGoals(ctx context.Context) ([]types.BrainEntry, error) {
	return s.listGoalEntries(ctx, "active")
}

// listGoalEntries returns goal automation entries filtered by exact status;
// an empty status returns goals of EVERY status. This is the status-agnostic
// lookup that keeps paused/completed/archived goals reachable by the API
// (find/update/resume) — the old active-only funnel made a paused goal
// unrecoverable (every endpoint 404'd on it).
func (s *GoalService) listGoalEntries(ctx context.Context, status string) ([]types.BrainEntry, error) {
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:   "automation",
		Status: status,
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
//
// This live matcher is AUTHORITATIVE over the stored trigger filter: the
// to_status gate only applies to events that actually carry a status.
// feature.completed events never set ToStatus (see event_service /
// feature_tracker emitters), so enforcing the stored to_status filter against
// them made every feature-sourced goal dead — MatchFilterValue("", "in:…") is
// false. Stored entries created before this fix need no migration; the gate
// is simply skipped here for status-less events while task.status_changed
// keeps it.
func goalMatchesEvent(goal types.BrainEntry, evt types.Event) bool {
	if goal.Trigger == nil {
		return false
	}
	if !goal.Trigger.MatchesEvent(evt.Type) {
		return false
	}
	filters := goal.Trigger.Filter
	if evt.ToStatus == "" && evt.Type != types.EventTaskStatusChanged {
		if _, ok := filters["to_status"]; ok {
			pruned := make(map[string]string, len(filters))
			for k, v := range filters {
				if k == "to_status" {
					continue
				}
				pruned[k] = v
			}
			filters = pruned
		}
	}
	return matchAutomationFilters(filters, evt)
}
