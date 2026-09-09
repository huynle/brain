package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// opcodeStatusClient is the HTTP client used for OpenCode status checks.
// Short timeout to avoid blocking the poll loop.
var opcodeStatusClient = &http.Client{Timeout: 5 * time.Second}

// steerHoldMax bounds how long completion (and serve-process teardown) is
// held for an attachable OpenCode task whose driver exited while the
// session still reports busy — i.e. an injected/steered turn is finishing
// its work on the serve process. Package-level var so tests can shrink it.
var steerHoldMax = 10 * time.Minute

// sessionStatusForPort is the status probe used by the steered-turn hold
// (completion gate + serve teardown). Indirected so tests can stub session
// busy/idle without a real OpenCode server on localhost.
var sessionStatusForPort = checkOpencodeStatus

// steerFlusher performs the actual prompt_async re-poke. Indirected for tests.
var steerFlusher = postEmptyPrompt

// sessionAborter performs the actual /session/{id}/abort POST used by stall
// recovery to clear a busy wedge. Indirected for tests.
var sessionAborter = postAbort

// stalledNoteMarker is the exact prefix the runner appends to a task's body
// when it detects a silent-but-busy OpenCode session past the stall timeout.
// It MUST stay byte-for-byte identical to service.StalledMarker, which the
// service-side enrichAbandonmentState greps for to surface a resumable
// `stalled` abandonment signal. Duplicated here (rather than imported) to
// avoid a runner→service import cycle — the same duplicate-literal-with-
// cross-reference pattern the orphan-reaper marker uses (orphanReaperNoteText
// in runner.go, mirrored by service.OrphanReaperMarker). If this text
// changes, change service.StalledMarker with it.
const stalledNoteMarker = "*Stalled: runner detected a silent OpenCode session"

// pendingPermissionsForTask reports how many OpenCode permission prompts are
// outstanding for a task's instance. The stall recovery must never abort a
// session with real pending permissions. Indirected for tests; the default
// reads the runner's bridge-client permission cache. Returns 0 when there is
// no bridge client (e.g. a pull-mode runner) — see the stall recovery gating,
// which additionally requires a bridge client before it will abort.
var pendingPermissionsForTask = func(tr *TaskRunner, task RunningTask) int {
	if bc := tr.getBridgeClient(); bc != nil {
		return bc.PendingPermissionCount(task.InstanceID)
	}
	return 0
}

// postEmptyPrompt POSTs an empty-parts continuation to an OpenCode session so
// it starts the next turn and drains a queued steer/control prompt. OpenCode
// only delivers a queued prompt on a fresh turn; an empty {"parts":[]} body is
// the least-intrusive way to force the queue forward without injecting
// spurious text. Returns nil on a 2xx response; a non-2xx status or transport
// error yields an error so the caller can leave PendingSteer set for retry.
func postEmptyPrompt(port int, sessionID string) error {
	url := fmt.Sprintf("http://localhost:%d/session/%s/prompt_async", port, sessionID)
	resp, err := opcodeStatusClient.Post(url, "application/json", strings.NewReader(`{"parts":[]}`))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("prompt_async re-poke: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// postAbort POSTs to /session/{id}/abort on a local OpenCode instance to clear
// a busy wedge (proven in the incident to unstick an 18+ minute silent-busy
// session). Mirrors postEmptyPrompt's localhost POST plumbing. Returns nil on
// a 2xx response; a non-2xx status or transport error yields an error so the
// caller can decide whether to escalate.
func postAbort(port int, sessionID string) error {
	url := fmt.Sprintf("http://localhost:%d/session/%s/abort", port, sessionID)
	resp, err := opcodeStatusClient.Post(url, "application/json", strings.NewReader(`{}`))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused.
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("session abort: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// flushQueuedSteer re-pokes an OpenCode session so it starts the next turn
// and drains a steer/control prompt that was queued while the prior turn was
// ending via a tool call. OpenCode only delivers a queued prompt on a fresh
// turn; when the turn ended without the agent producing more output, no such
// turn starts on its own, so the runner drives it. Best-effort: a failure is
// logged and the PendingSteer flag is left set for a later retry.
func (tr *TaskRunner) flushQueuedSteer(task RunningTask) {
	if task.OpencodePort == 0 || task.SessionID == "" {
		return
	}
	if err := steerFlusher(task.OpencodePort, task.SessionID); err != nil {
		tr.logger.Printf("steer flush: task %s re-poke failed: %v (leaving PendingSteer set)", task.ID, err)
		return
	}
	tr.processMgr.SetPendingSteer(task.ID, false)
	tr.logger.Printf("steer flush: task %s re-poked session %s to drain queued steer", task.ID, task.SessionID)
}

// checkOpencodeStatus queries the OpenCode HTTP API to check if it's idle or busy.
// The /session/status endpoint returns a map of session IDs to statuses.
// An empty map {} means all sessions are idle. Sessions that are busy appear in the map.
// Returns "idle", "busy", or "unavailable".
func checkOpencodeStatus(port int) string {
	url := fmt.Sprintf("http://localhost:%d/session/status", port)
	resp, err := opcodeStatusClient.Get(url)
	if err != nil {
		return "unavailable"
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "unavailable"
	}

	// Response is a map of sessionID -> status object.
	// Empty map = all idle, any entries = at least one busy.
	var statusMap map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&statusMap); err != nil {
		return "unavailable"
	}

	if len(statusMap) == 0 {
		return "idle"
	}
	return "busy"
}

// idleDetectionThreshold returns the configured idle detection threshold,
// defaulting to 30 seconds if not set.
func (tr *TaskRunner) idleDetectionThreshold() time.Duration {
	if tr.config.IdleDetectionThreshold > 0 {
		return time.Duration(tr.config.IdleDetectionThreshold) * time.Millisecond
	}
	return 30 * time.Second
}

// stallTimeout returns the configured stall timeout, or 0 (disabled) if unset
// or negative.
func (tr *TaskRunner) stallTimeout() time.Duration {
	if tr.config.StallTimeout > 0 {
		return time.Duration(tr.config.StallTimeout) * time.Millisecond
	}
	return 0
}

// resolveCompleteOnIdle determines whether a task should be auto-completed on idle.
// If CompleteOnIdle is explicitly set, use that value.
// If DirectPrompt is set and CompleteOnIdle is not explicitly set, default to true.
func resolveCompleteOnIdle(completeOnIdle *bool, directPrompt string) bool {
	if completeOnIdle != nil {
		return *completeOnIdle
	}
	// Default to true when direct_prompt is set
	return directPrompt != ""
}

// checkIdleStatus iterates running tasks, checks their status based on
// executor type, and handles idle detection for tasks with CompleteOnIdle
// or direct_prompt.
//
// OpenCode tasks: HTTP polling via /session/status endpoint.
// Pi tasks: Process exit detection (Pi RPC processes exit when done;
// a running Pi process is always "busy").
func (tr *TaskRunner) checkIdleStatus(ctx context.Context) {
	allProcesses := tr.processMgr.GetAllRunning()
	threshold := tr.idleDetectionThreshold()

	for _, info := range allProcesses {
		if ctx.Err() != nil {
			return
		}

		task := info.Task

		// Branch on executor type.
		//
		// "pi" and "script" both use process-exit semantics: a running process
		// is always considered busy and completion is detected via process
		// exit in checkRunningTasks/CheckCompletion. They have no HTTP API to
		// poll, so calling checkOpencodeIdleStatus on them would always
		// observe "unavailable" (best case) or — worse — connect to whatever
		// happens to be listening on a guessed port and misinterpret the
		// response. The bug this prevents: script-executor automation tasks
		// (e.g. cron-triggered shell commands) being marked "blocked" by the
		// runner because the OpenCode HTTP poll never finds a session.
		switch task.ExecutorType {
		case "pi":
			tr.checkPiIdleStatus(ctx, info, threshold)
		case "script":
			tr.checkScriptIdleStatus(ctx, info, threshold)
		default:
			tr.checkOpencodeIdleStatus(ctx, task, threshold)
		}
	}
}

// checkScriptIdleStatus handles idle detection for script executor tasks.
// Script processes don't expose an HTTP endpoint. A running script process
// is always considered "busy" (the command is still executing).
// Completion is detected via process exit in checkRunningTasks/CheckCompletion.
//
// This function is intentionally a no-op for running script processes.
// GetAllRunning() already filters out exited processes, so process-exit
// completion is handled by the checkRunningTasks path instead.
func (tr *TaskRunner) checkScriptIdleStatus(ctx context.Context, info ProcessInfo, threshold time.Duration) {
	// Script processes are always "busy" while running — no HTTP idle
	// detection. Process exit/completion is handled by checkRunningTasks →
	// CheckCompletion. Parameters are unused but kept for symmetry with
	// checkPiIdleStatus.
	_ = ctx
	_ = info
	_ = threshold
}

// checkOpencodeIdleStatus handles idle detection for OpenCode tasks via HTTP polling.
func (tr *TaskRunner) checkOpencodeIdleStatus(ctx context.Context, task RunningTask, threshold time.Duration) {
	port := task.OpencodePort

	// Skip tasks without a discovered port
	if port == 0 {
		return
	}

	status := checkOpencodeStatus(port)

	switch status {
	case "idle":
		tr.advanceIdleTimer(ctx, task, threshold)

	case "busy":
		// The raw /session/status busy flag lingers after a question-tool
		// turn: the turn ended (per the tool contract) but the session is
		// still reported busy. Probe the transcript to distinguish a
		// genuinely-working agent from a wedged-busy question turn.
		if task.SessionID != "" {
			ended, lastActivity, ok := checkOpencodeTurnEnded(port, task.SessionID)
			if ok && !lastActivity.IsZero() {
				// Phase 4 consumes LastActivity for the stall timer; harmless now.
				tr.processMgr.UpdateLastActivity(task.ID, lastActivity)
			}
			if ok && ended {
				// Turn ended while status still busy.
				if task.PendingSteer {
					// A steer/control prompt was queued for this task's next
					// turn but the turn ended via a tool call, so OpenCode
					// never started a fresh turn to consume it. Re-poke the
					// session to drain it. flushQueuedSteer clears the flag on
					// success; leaves it set (for retry next tick) on failure.
					// Return early: the flush is expected to restart a turn,
					// so we give it a chance before the idle timer counts this
					// as idle. The flush only fires on this turn-ended edge
					// while PendingSteer is set, so it won't thrash every tick.
					tr.logger.Printf("idle detection: task %s turn ended while status busy with pending steer, flushing", task.ID)
					tr.flushQueuedSteer(task)
					return
				}
				// No queued steer — treat exactly like idle.
				tr.logger.Printf("idle detection: task %s turn ended while status busy, advancing idle timer", task.ID)
				tr.advanceIdleTimer(ctx, task, threshold)
				return
			}
		}
		// Genuinely busy (turn not ended, or we couldn't probe). Before the
		// default "clear idle timer" behavior, run the stall check: a session
		// that has emitted no new activity for the stall window while still
		// reporting busy AND with no pending permissions is wedged and must be
		// recovered. Gated entirely behind stallTimeout() > 0 so the disabled
		// path (StallTimeout==0) is untouched.
		if tr.stallTimeout() > 0 {
			if tr.handleStallCheck(ctx, task) {
				// Stall check consumed this tick (seeded the clock, recovered,
				// or escalated). Do not fall through to the idle-clear.
				return
			}
		}
		// Agent is genuinely working (or we can't probe) — clear idle timestamp.
		if task.IdleSince != "" {
			tr.processMgr.UpdateIdleSince(task.ID, "")
			tr.logger.Printf("idle detection: task %s back to busy, clearing idle timer", task.ID)
		}

	case "unavailable":
		// Skip — might be temporary (process starting up, network blip)
	}
}

// advanceIdleTimer runs the idle-timer start/advance logic shared by the
// "idle" status branch and the busy-but-turn-ended path: it records the
// first idle timestamp, and once the idle duration meets the threshold it
// drives handleIdleThresholdExceeded.
func (tr *TaskRunner) advanceIdleTimer(ctx context.Context, task RunningTask, threshold time.Duration) {
	if task.IdleSince == "" {
		// First idle detection — record the timestamp
		now := time.Now().UTC().Format(time.RFC3339)
		tr.processMgr.UpdateIdleSince(task.ID, now)
		tr.logger.Printf("idle detection: task %s first idle at %s", task.ID, now)
		return
	}
	// Already idle — check if threshold exceeded
	idleSince, err := time.Parse(time.RFC3339, task.IdleSince)
	if err != nil {
		tr.logger.Printf("idle detection: failed to parse IdleSince for %s: %v", task.ID, err)
		return
	}
	if time.Since(idleSince) >= threshold {
		tr.handleIdleThresholdExceeded(ctx, task)
	}
}

// handleStallCheck runs the silent-busy stall detector for a genuinely-busy
// OpenCode task (turn NOT ended). It returns true when it has consumed the
// tick — either by seeding the stall clock, performing bounded recovery, or
// escalating a still-stalled task to blocked — so the caller skips its normal
// busy idle-clear. Returns false when the task is not (yet) stalled, leaving
// today's busy behavior to run.
//
// Caller guarantees tr.stallTimeout() > 0. The recovery constraint is
// airtight: the abort only runs when pendingPermissionsForTask == 0, and only
// when a bridge client exists (permission state is otherwise unknown, so it is
// unsafe to abort). The surface (marker + metadata) is still written even
// without a bridge client so the task is resumable.
func (tr *TaskRunner) handleStallCheck(ctx context.Context, task RunningTask) bool {
	stall := tr.stallTimeout()

	// Can't judge staleness without a baseline — seed the clock and wait.
	if task.LastActivity.IsZero() {
		tr.processMgr.UpdateLastActivity(task.ID, time.Now().UTC())
		tr.logger.Printf("stall check: task %s seeding stall clock (no prior activity baseline)", task.ID)
		return true
	}

	// Not yet past the stall window — not stalled.
	if time.Since(task.LastActivity) < stall {
		return false
	}

	// A real permission prompt outstanding is legitimate waiting — never
	// abort, never mark. Fall through to today's busy behavior.
	if pendingPermissionsForTask(tr, task) > 0 {
		tr.logger.Printf("stall check: task %s past stall window but %d permission(s) pending — not stalled", task.ID, pendingPermissionsForTask(tr, task))
		return false
	}

	// Past the stall window with no pending permissions: the session is
	// wedged. If we already recovered once and it's still stalled, escalate
	// to blocked; otherwise perform bounded recovery.
	if task.StallRecovered {
		tr.logger.Printf("stall recovery: task %s still stalled after a prior recovery, escalating to blocked", task.ID)
		tr.escalateStall(ctx, task)
		return true
	}

	tr.recoverStall(ctx, task)
	return true
}

// recoverStall performs one bounded stall recovery: surface the stall
// (resumable marker + metadata), abort the wedged session to clear the busy
// flag (only when a bridge client exists), flush any queued steer, mark
// StallRecovered, and advance the stall clock so the next stall edge is a
// fresh window away.
func (tr *TaskRunner) recoverStall(ctx context.Context, task RunningTask) {
	stall := tr.stallTimeout()
	tr.logger.Printf("stall recovery: task %s silent-busy for %s with no pending permissions, recovering", task.ID, time.Since(task.LastActivity).Round(time.Second))

	// Surface: append the resumable marker and set stalled metadata. Uses the
	// exact stalledNoteMarker literal (== service.StalledMarker) so the
	// service side surfaces a `stalled` abandonment signal.
	note := fmt.Sprintf("\n\n---\n%s (no output for %s).*\n", stalledNoteMarker, stall.Round(time.Second))
	if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
		tr.logger.Printf("stall recovery: failed to append marker to %s: %v", task.ID, err)
	}
	if err := tr.client.UpdateMetadata(ctx, task.Path, map[string]interface{}{
		"stalled":       true,
		"stalled_since": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		tr.logger.Printf("stall recovery: failed to set stalled metadata on %s: %v", task.ID, err)
	}

	// Auto-recovery: abort the wedged session only when a bridge client exists
	// (permission state is otherwise unknown, so aborting is unsafe). The
	// surface above is written either way so the task stays resumable.
	if tr.getBridgeClient() != nil {
		if task.OpencodePort != 0 && task.SessionID != "" {
			if err := sessionAborter(task.OpencodePort, task.SessionID); err != nil {
				tr.logger.Printf("stall recovery: task %s session abort failed: %v", task.ID, err)
			} else {
				tr.logger.Printf("stall recovery: task %s aborted session %s to clear busy wedge", task.ID, task.SessionID)
			}
		}
		// Drain any steer that was queued while the session was wedged.
		if task.PendingSteer {
			tr.flushQueuedSteer(task)
		}
	} else {
		tr.logger.Printf("stall recovery: task %s has no bridge client, surfaced marker only (no auto-abort)", task.ID)
	}

	// Guard against re-firing every tick: advance the stall clock and record
	// that recovery ran, so the next stall edge is a fresh StallTimeout window
	// away and, if it still stalls, escalates to blocked.
	tr.processMgr.SetStallRecovered(task.ID, true)
	tr.processMgr.UpdateLastActivity(task.ID, time.Now().UTC())
}

// escalateStall marks a task blocked after a prior recovery failed to clear
// the stall within one further StallTimeout window. Mirrors the blocked-path
// teardown of handleIdleThresholdExceeded's else-branch.
func (tr *TaskRunner) escalateStall(ctx context.Context, task RunningTask) {
	note := "\n\n---\n*Marked blocked by runner: OpenCode session remained stalled after stall recovery (silent-busy past the stall timeout).*\n"
	if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
		tr.logger.Printf("stall recovery: failed to append escalation note to %s: %v", task.ID, err)
	}
	if err := tr.client.UpdateTaskStatus(ctx, task.Path, "blocked"); err != nil {
		tr.logger.Printf("stall recovery: failed to mark %s blocked: %v", task.ID, err)
		return
	}

	tr.processMgr.Remove(task.ID)

	tr.mu.Lock()
	tr.stats.Failed++
	if tr.processMgr.RunningCount() == 0 {
		tr.status = RunnerStatusPolling
	}
	tr.mu.Unlock()

	tr.cleanupTaskTmux(task)
	tr.cleanupTaskArtifacts(task)

	tr.emitEvent(RunnerEvent{
		Type:   EventTaskFailed,
		TaskID: task.ID,
	})
}

// checkPiIdleStatus handles idle detection for Pi executor tasks.
// Pi RPC processes don't expose an HTTP endpoint. A running Pi process
// is always considered "busy" (actively working on the prompt).
// Completion is detected via process exit in checkRunningTasks/CheckCompletion.
//
// This function is intentionally a no-op for running Pi processes.
// GetAllRunning() already filters out exited processes, so process-exit
// completion is handled by the checkRunningTasks path instead.
func (tr *TaskRunner) checkPiIdleStatus(ctx context.Context, info ProcessInfo, threshold time.Duration) {
	// Pi processes are always "busy" while running — no HTTP idle detection.
	// Process exit/completion is handled by checkRunningTasks → CheckCompletion.
}

// isTerminalStatus returns true if the given status is a terminal state
// that should not be overwritten by the idle detection logic.
func isTerminalStatus(status string) bool {
	switch status {
	case "completed", "validated", "blocked", "cancelled", "archived", "superseded":
		return true
	}
	return false
}

// handleIdleThresholdExceeded handles a task that has been idle longer than the threshold.
func (tr *TaskRunner) handleIdleThresholdExceeded(ctx context.Context, task RunningTask) {
	// Guard: re-fetch the task status from the Brain API before overwriting.
	// This prevents a race condition where the agent already marked the task
	// as completed (or another terminal status) via brain_update, but the
	// runner's idle detection fires and overwrites it.
	entry, err := tr.client.GetEntry(ctx, task.Path)
	if err != nil {
		// API error — log and proceed with existing behavior (graceful degradation)
		tr.logger.Printf("idle detection: failed to re-fetch task %s status: %v (proceeding with idle handling)", task.ID, err)
	} else if isTerminalStatus(entry.Status) {
		tr.logger.Printf("idle detection: task %s already in terminal status %q, skipping overwrite", task.ID, entry.Status)

		// Determine the appropriate event type based on the actual status
		eventType := EventTaskCompleted
		if entry.Status == "blocked" || entry.Status == "cancelled" {
			eventType = EventTaskFailed
		}

		// Create task result
		completedAt := time.Now()
		duration := completedAt.Sub(task.StartedAt).Milliseconds()
		exitCode := 0
		result := &TaskResult{
			TaskID:      task.ID,
			Status:      TaskResultCompleted,
			StartedAt:   task.StartedAt,
			CompletedAt: completedAt,
			Duration:    duration,
			ExitCode:    &exitCode,
		}
		if entry.Status == "blocked" || entry.Status == "cancelled" {
			result.Status = TaskResultBlocked
		}

		// Cleanup: remove from process manager, tmux, temp files
		tr.processMgr.Remove(task.ID)

		tr.mu.Lock()
		if entry.Status == "completed" || entry.Status == "validated" {
			tr.stats.Completed++
		} else {
			tr.stats.Failed++
		}
		tr.stats.TotalRuntime += duration
		if tr.processMgr.RunningCount() == 0 {
			tr.status = RunnerStatusPolling
		}
		tr.mu.Unlock()

		tr.cleanupTaskTmux(task)
		tr.cleanupTaskArtifacts(task)

		tr.emitEvent(RunnerEvent{
			Type:   eventType,
			Result: result,
			TaskID: task.ID,
		})
		return
	}

	if task.CompleteOnIdle {
		// Auto-complete the task
		tr.logger.Printf("idle detection: task %s idle threshold exceeded, marking completed", task.ID)

		// Append completion note
		note := "\n\n---\n*Auto-completed by runner: OpenCode agent went idle after completing work.*\n"
		if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
			tr.logger.Printf("idle detection: failed to append note to %s: %v", task.ID, err)
		}

		// Update task status to completed
		if err := tr.client.UpdateTaskStatus(ctx, task.Path, "completed"); err != nil {
			tr.logger.Printf("idle detection: failed to mark %s completed: %v", task.ID, err)
			return
		}

		// Create task result
		completedAt := time.Now()
		duration := completedAt.Sub(task.StartedAt).Milliseconds()
		exitCode := 0
		result := &TaskResult{
			TaskID:      task.ID,
			Status:      TaskResultCompleted,
			StartedAt:   task.StartedAt,
			CompletedAt: completedAt,
			Duration:    duration,
			ExitCode:    &exitCode,
		}

		// Remove from process manager
		tr.processMgr.Remove(task.ID)

		// Update stats
		tr.mu.Lock()
		tr.stats.Completed++
		tr.stats.TotalRuntime += duration
		if tr.processMgr.RunningCount() == 0 {
			tr.status = RunnerStatusPolling
		}
		tr.mu.Unlock()

		// Clean up tmux window
		tr.cleanupTaskTmux(task)

		// Cleanup temp files
		tr.cleanupTaskArtifacts(task)

		// Emit completion event
		tr.emitEvent(RunnerEvent{
			Type:   EventTaskCompleted,
			Result: result,
			TaskID: task.ID,
		})
	} else {
		// Not auto-complete — mark as blocked (agent went idle without completing)
		tr.logger.Printf("idle detection: task %s idle threshold exceeded, marking blocked", task.ID)

		note := "\n\n---\n*Marked blocked by runner: OpenCode agent went idle without completing the task.*\n"
		if err := tr.client.AppendToTask(ctx, task.Path, note); err != nil {
			tr.logger.Printf("idle detection: failed to append note to %s: %v", task.ID, err)
		}

		if err := tr.client.UpdateTaskStatus(ctx, task.Path, "blocked"); err != nil {
			tr.logger.Printf("idle detection: failed to mark %s blocked: %v", task.ID, err)
			return
		}

		// Remove from process manager
		tr.processMgr.Remove(task.ID)

		// Update stats
		tr.mu.Lock()
		tr.stats.Failed++
		if tr.processMgr.RunningCount() == 0 {
			tr.status = RunnerStatusPolling
		}
		tr.mu.Unlock()

		// Clean up tmux window
		tr.cleanupTaskTmux(task)

		// Cleanup temp files
		tr.cleanupTaskArtifacts(task)

		// Emit event
		tr.emitEvent(RunnerEvent{
			Type:   EventTaskFailed,
			TaskID: task.ID,
		})
	}
}
