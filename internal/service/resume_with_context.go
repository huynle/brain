package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Supervisor resume-with-context (Phase 3)
//
// A superset of ResumeTask that also carries a supervisor-authored context
// blob. Two delivery paths (ADR Decision 5):
//
//   1. LIVE INJECT — if the task's session is still live on a bridge instance,
//      inject the context into the running session via the LiveInjector. No
//      status flip, no relaunch. resume_mode=live_injected.
//   2. RELAUNCH — otherwise reuse the exact ResumeTask gates (idempotency,
//      terminal, abandonment, live-claim safety) then stamp EXTENDED resume
//      metadata so the Phase 4 runner rehydrates or reuses the session. The
//      API's resume_mode is advisory; the runner finalizes via CanResumeSession.
// =============================================================================

// LiveInjector delivers supervisor-authored context into a task's still-live
// agent session, bypassing a relaunch. Implementations locate the live
// instance through the same in-process control plumbing the goal steerer uses
// (instance registry + runner bridge), never via HTTP self-calls.
//
// InjectContext reports (injected, sessionID, err):
//   - injected=true, sessionID set: the context was delivered into a running
//     session; the caller should NOT relaunch or flip status.
//   - injected=false, nil err: graceful skip (no live instance, no session,
//     or an unsupported executor such as Pi) — the caller falls through to
//     the relaunch path.
//   - err != nil: delivery was attempted and failed.
//
// A nil LiveInjector on TaskServiceImpl means live-inject is unavailable and
// ResumeTaskWithContext always relaunches.
type LiveInjector interface {
	InjectContext(ctx context.Context, projectID, taskID, injectedContext string) (bool, string, error)
}

// SetLiveInjector wires the live-session injector used by
// ResumeTaskWithContext. Nil-safe: leaving it unset makes every
// resume-with-context call take the relaunch path. Set once at apiserver
// startup (see internal/apiserver/server.go), mirroring how the goal steerer
// is wired via WithGoalSteerer.
func (s *TaskServiceImpl) SetLiveInjector(inj LiveInjector) {
	s.liveInjector = inj
}

// resumeGateDecision is the outcome of the shared resume gate: either a
// short-circuit (proceed=false, Reason populated on the result) or a green
// light to stamp resume metadata (proceed=true). The loaded+enriched task is
// returned so callers don't re-fetch.
type resumeGateDecision struct {
	proceed bool
	task    *types.ResolvedTask
}

// runResumeGate loads the task and applies the idempotency, terminal,
// abandonment, and live-claim-safety gates shared by ResumeTask and
// ResumeTaskWithContext. On a short-circuit it populates result.Reason and
// returns proceed=false. On a clean gate it releases the stale claim + acked
// dispatch lease and returns proceed=true. A missing task yields an error
// whose text contains "not found" so the handler maps it to 404.
//
// This is a faithful extraction of ResumeTask's gate block — the two callers
// MUST observe identical behavior, so ResumeTask now delegates here.
func (s *TaskServiceImpl) runResumeGate(ctx context.Context, projectID, taskID string, force bool, result *types.ResumeTaskResult) (resumeGateDecision, error) {
	task, err := s.GetTask(ctx, projectID, taskID)
	if err != nil {
		return resumeGateDecision{}, fmt.Errorf("resume: %w", err)
	}
	if task == nil {
		return resumeGateDecision{}, fmt.Errorf("resume: task not found: %s/%s", projectID, taskID)
	}

	result.TaskID = taskID
	result.PriorStatus = task.Status
	result.PriorSessionsCount = len(task.Sessions)
	result.AbandonReason = task.AbandonReason

	// Idempotency: already resumed and waiting for the runner → no-op.
	if task.Status == "pending" && task.ResumeRequested {
		result.Reason = "resume already requested; runner will pick up on next poll"
		return resumeGateDecision{task: task}, nil
	}

	// Terminal statuses are outside the resume gate — use Trigger instead.
	switch task.Status {
	case "completed", "validated", "cancelled", "superseded", "archived":
		if !force {
			result.Reason = fmt.Sprintf("task status %q is terminal; use trigger to re-run", task.Status)
			return resumeGateDecision{task: task}, nil
		}
	}

	// Abandonment gate. Force bypasses (but never the live-claim safety below).
	if !task.IsAbandoned && !force {
		result.Reason = fmt.Sprintf("task is not abandoned (status=%q); use trigger or force=true", task.Status)
		return resumeGateDecision{task: task}, nil
	}

	// Live-claim safety: refuse to release a claim held by an ONLINE runner —
	// even with force. Otherwise clean the stale claim + acked lease so the
	// runner sees a fresh slate on re-claim.
	if claim, err := s.storage.GetClaim(ctx, projectID, taskID); err == nil && claim != nil {
		runner, rerr := s.storage.GetRunner(ctx, claim.RunnerID)
		if rerr == nil && runner != nil && runner.Status == "online" {
			result.Reason = fmt.Sprintf(
				"task is claimed by online runner %q since enrichment view; abort that runner or wait for its lease to lapse before resuming",
				claim.RunnerID,
			)
			return resumeGateDecision{task: task}, nil
		}
		if _, err := s.storage.ReleaseClaim(ctx, projectID, taskID, claim.RunnerID); err != nil {
			slog.Debug("resume: release claim failed (continuing)",
				"project", projectID, "task_id", taskID, "error", err)
		}
	}
	if _, err := s.storage.ClearDispatchLease(ctx, projectID, taskID); err != nil {
		slog.Debug("resume: clear dispatch lease failed (continuing)",
			"project", projectID, "task_id", taskID, "error", err)
	}

	return resumeGateDecision{proceed: true, task: task}, nil
}

// ResumeTaskWithContext is the supervisor context-injection resume. See the
// package header for the two-path decision tree.
func (s *TaskServiceImpl) ResumeTaskWithContext(ctx context.Context, projectID, taskID string, opts *types.ResumeWithContextOptions) (*types.ResumeWithContextResult, error) {
	if opts == nil {
		opts = &types.ResumeWithContextOptions{}
	}
	// injected_context is REQUIRED — the endpoint's entire purpose. Return an
	// error (handler maps to 400) rather than a Resumed=false result, so the
	// client can distinguish "bad request" from a legitimate no-op.
	if strings.TrimSpace(opts.InjectedContext) == "" {
		return nil, fmt.Errorf("resume-with-context: injected_context is required")
	}

	result := &types.ResumeWithContextResult{}

	// LIVE INJECT — try the running session first. Nil injector or a graceful
	// skip (not live / unsupported executor) falls through to relaunch. Only a
	// delivery error is surfaced. We probe the injector BEFORE the gate so a
	// still-live session is nudged even though enrichment would classify the
	// task as abandoned (the claim can lapse while the process keeps running).
	if s.liveInjector != nil {
		injected, sessionID, err := s.liveInjector.InjectContext(ctx, projectID, taskID, opts.InjectedContext)
		if err != nil {
			return nil, fmt.Errorf("resume-with-context: live inject: %w", err)
		}
		if injected {
			result.TaskID = taskID
			result.Resumed = true
			result.ResumeMode = types.ResumeModeLiveInjected
			result.InjectedLive = true
			result.TargetSessionID = sessionID
			// Best-effort: populate PriorStatus/sessions for parity with the
			// relaunch result. Ignore load errors — the inject already
			// succeeded and is the source of truth here.
			if task, terr := s.GetTask(ctx, projectID, taskID); terr == nil && task != nil {
				result.PriorStatus = task.Status
				result.PriorSessionsCount = len(task.Sessions)
			}
			slog.Info("resume-with-context: injected into live session",
				"project", projectID, "task_id", taskID, "session_id", sessionID)
			return result, nil
		}
	}

	// RELAUNCH — reuse the shared gate.
	decision, err := s.runResumeGate(ctx, projectID, taskID, opts.Force, &result.ResumeTaskResult)
	if err != nil {
		return nil, err
	}
	if !decision.proceed {
		return result, nil
	}
	task := decision.task

	// Compute the ADVISORY intended mode. same_session requires:
	//   - prefer_same_session
	//   - executor override empty or opencode
	//   - the task's effective executor is opencode
	//   - a stored prior session id exists
	// Anything else → rehydrate. The runner makes the authoritative call.
	mode := types.ResumeModeRehydrate
	var targetSession string
	effectiveExecutor := task.Executor
	if opts.ExecutorOverride != "" {
		effectiveExecutor = opts.ExecutorOverride
	}
	if effectiveExecutor == "" {
		effectiveExecutor = "opencode" // default executor
	}
	if opts.PreferSameSession &&
		(opts.ExecutorOverride == "" || opts.ExecutorOverride == "opencode") &&
		effectiveExecutor == "opencode" {
		if sid := mostRecentSessionID(task.Sessions); sid != "" {
			mode = types.ResumeModeSameSession
			targetSession = sid
		}
	}

	// Stamp extended resume metadata. status=pending so the runner picks it up.
	now := time.Now().UTC().Format(time.RFC3339)
	meta := map[string]interface{}{
		"status":                     "pending",
		"resume_requested":           true,
		"resume_requested_at":        now,
		"resume_mode":                mode,
		"resume_injected_context":    opts.InjectedContext,
		"resume_prefer_same_session": opts.PreferSameSession,
	}
	if opts.ExecutorOverride != "" {
		meta["resume_executor_override"] = opts.ExecutorOverride
	}
	if _, err := s.storage.MergeMetadata(ctx, task.Path, meta); err != nil {
		return nil, fmt.Errorf("resume-with-context: update metadata: %w", err)
	}

	result.Resumed = true
	result.ResumeMode = mode
	result.TargetSessionID = targetSession
	slog.Info("resume-with-context: task resumed (relaunch)",
		"project", projectID, "task_id", taskID,
		"prior_status", result.PriorStatus,
		"resume_mode", mode,
		"prefer_same_session", opts.PreferSameSession,
		"executor_override", opts.ExecutorOverride,
		"force", opts.Force,
	)
	return result, nil
}

// mostRecentSessionID returns the session id with the latest Timestamp, or the
// first stable one if timestamps are absent/equal. Empty string when the map
// is empty. Used to pick the "session of record" for same_session intent.
func mostRecentSessionID(sessions map[string]types.SessionInfo) string {
	best := ""
	bestTS := ""
	for sid, info := range sessions {
		if best == "" || info.Timestamp > bestTS {
			best = sid
			bestTS = info.Timestamp
		}
	}
	return best
}

// ResumeFeatureWithContext fans out ResumeTaskWithContext across every task in
// the feature, mirroring ResumeFeature: per-feature lock, terminal-status batch
// guard (unless force), per-task errors folded into skipped results. The same
// injected_context is applied to every task. Truncation is the handler's job.
func (s *TaskServiceImpl) ResumeFeatureWithContext(ctx context.Context, projectID, featureID string, opts *types.ResumeWithContextOptions) (*types.ResumeWithContextFeatureResult, error) {
	if opts == nil {
		opts = &types.ResumeWithContextOptions{}
	}
	if strings.TrimSpace(opts.InjectedContext) == "" {
		return nil, fmt.Errorf("resume-with-context feature: injected_context is required")
	}
	sanitizedProjectID := strings.TrimSpace(projectID)
	sanitizedFeatureID := strings.TrimSpace(featureID)
	if sanitizedProjectID == "" {
		return nil, fmt.Errorf("resume-with-context feature: projectID is required")
	}
	if sanitizedFeatureID == "" {
		return nil, fmt.Errorf("resume-with-context feature: featureID is required")
	}

	// Enumerate BEFORE taking any lock (unknown features must not leave a
	// mutex entry — see ResumeFeature).
	featureTasks, err := s.getFeatureTasksFromFilesystem(sanitizedProjectID, sanitizedFeatureID)
	if err != nil {
		return nil, fmt.Errorf("resume-with-context feature: enumerate tasks: %w", err)
	}
	if len(featureTasks) == 0 {
		return nil, fmt.Errorf("resume-with-context feature: not found — feature %q has no tasks in project %q", sanitizedFeatureID, sanitizedProjectID)
	}

	release, err := acquireResumeFeatureLock(ctx, sanitizedProjectID+"|"+sanitizedFeatureID)
	if err != nil {
		return nil, err
	}
	defer release()

	result := &types.ResumeWithContextFeatureResult{
		FeatureID: sanitizedFeatureID,
		Results:   make([]types.ResumeWithContextResult, 0, len(featureTasks)),
	}

	for _, task := range featureTasks {
		if task.ID == "" {
			continue
		}
		// Terminal-status batch guard (bypassed by force), matching ResumeFeature.
		if !opts.Force && terminalStatuses[task.Status] {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, ctxErr
			}
			result.Results = append(result.Results, types.ResumeWithContextResult{
				ResumeTaskResult: types.ResumeTaskResult{
					TaskID:      task.ID,
					Resumed:     false,
					PriorStatus: task.Status,
					Reason:      fmt.Sprintf("terminal_status_excluded_from_batch (%s)", task.Status),
				},
			})
			result.TotalSkipped++
			continue
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		taskResult, err := s.ResumeTaskWithContext(ctx, sanitizedProjectID, task.ID, opts)
		if err != nil {
			slog.Warn("resume-with-context feature: per-task failed",
				"project", sanitizedProjectID, "feature", sanitizedFeatureID,
				"task_id", task.ID, "error", err)
			result.Results = append(result.Results, types.ResumeWithContextResult{
				ResumeTaskResult: types.ResumeTaskResult{
					TaskID:  task.ID,
					Resumed: false,
					Reason:  "internal_error",
				},
			})
			result.TotalSkipped++
			continue
		}
		result.Results = append(result.Results, *taskResult)
		if taskResult.Resumed {
			result.TotalResumed++
		} else {
			result.TotalSkipped++
		}
	}

	slog.Info("resume-with-context feature: batch complete",
		"project", sanitizedProjectID, "feature", sanitizedFeatureID,
		"total_resumed", result.TotalResumed, "total_skipped", result.TotalSkipped,
		"force", opts.Force)
	return result, nil
}
