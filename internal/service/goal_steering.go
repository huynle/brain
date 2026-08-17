package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Goal steering
//
// When a goal reconcile lands on "noop because work is in progress", steering
// turns that wait into an active nudge: each in-progress linked task's live
// agent session receives a prompt restating the goal (title, criteria,
// validation) with an instruction to self-assess and correct course now.
// Steering is best-effort per task and never fails the reconcile.
// =============================================================================

// SteerResult reports the outcome of steering one task's live session.
type SteerResult struct {
	// Steered is true when a steering prompt was delivered to the session.
	Steered bool
	// Unsupported is true when the task's executor cannot receive prompt
	// injections (e.g. a Pi RPC process has no prompt endpoint). The task is
	// skipped gracefully rather than treated as an error.
	Unsupported bool
	// Reason carries human-readable context for skips and failures.
	Reason string
}

// SessionSteerer delivers a steering prompt to the live agent session serving
// a task. Implementations locate the session through the same in-process
// control plumbing the /control handlers use (instance registry + runner
// bridge) — never through HTTP self-calls.
//
// The (SteerResult, error) contract: an error means delivery was attempted
// and failed; a non-error result with Steered=false describes a graceful skip
// (no live instance, no session yet, unsupported executor).
type SessionSteerer interface {
	SteerTask(ctx context.Context, projectID, taskID, prompt string) (SteerResult, error)
}

// lastSteeredAtMetadataKey is the DB-only metadata key holding the RFC3339
// timestamp of the goal's most recent steering injection (cooldown state).
const lastSteeredAtMetadataKey = "last_steered_at"

// buildGoalSteeringPrompt renders the prompt injected into a live session:
// the goal identity, criteria, validation, and an explicit course-correct +
// self-assess instruction.
func buildGoalSteeringPrompt(goal types.BrainEntry) string {
	cfg := goal.Goal
	var b strings.Builder
	b.WriteString("## Goal check-in\n\n")
	fmt.Fprintf(&b, "This task exists to achieve the goal %q", goal.Title)
	if cfg != nil && cfg.ID != "" {
		fmt.Fprintf(&b, " (goal id: %s)", cfg.ID)
	}
	b.WriteString(".\n")
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
	b.WriteString("\nSelf-assess your progress against the criteria above and correct course NOW. ")
	b.WriteString("Only complete this task if the goal is truly met; otherwise keep working toward it.")
	return b.String()
}

// steerInProgressTasks injects the goal steering prompt into every
// in_progress linked task's live session. Best-effort per task: failures are
// counted as skips and never returned as an error.
func (s *GoalService) steerInProgressTasks(ctx context.Context, goal types.BrainEntry, tasks []types.ResolvedTask) (steered, skipped int) {
	prompt := buildGoalSteeringPrompt(goal)
	for _, t := range tasks {
		if t.Status != "in_progress" {
			continue
		}
		res, err := s.steerer.SteerTask(ctx, goal.ProjectID, t.ID, prompt)
		switch {
		case err != nil:
			skipped++
			slog.Warn("goal steering: steer task failed",
				"goal_id", goal.Goal.ID, "task_id", t.ID, "error", err)
		case res.Steered:
			steered++
		default:
			skipped++
			slog.Debug("goal steering: task skipped",
				"goal_id", goal.Goal.ID, "task_id", t.ID,
				"unsupported", res.Unsupported, "reason", res.Reason)
		}
	}
	return steered, skipped
}

// steeringCooldownElapsed reports whether the goal's steering cooldown has
// elapsed, reading the last_steered_at DB metadata mirror. Missing/invalid
// state counts as elapsed so a fresh goal can steer immediately.
func (s *GoalService) steeringCooldownElapsed(ctx context.Context, goal types.BrainEntry, now time.Time) bool {
	last, ok := s.lastSteeredAt(ctx, goal.ID)
	if !ok {
		return true
	}
	cooldown := time.Duration(goal.Goal.SteeringCooldownMinutes()) * time.Minute
	return now.Sub(last) >= cooldown
}

// lastSteeredAt reads the goal entry's last_steered_at metadata timestamp.
func (s *GoalService) lastSteeredAt(ctx context.Context, entryID string) (time.Time, bool) {
	if s.store == nil || entryID == "" {
		return time.Time{}, false
	}
	row, err := s.store.GetNoteByShortID(ctx, entryID)
	if err != nil || row == nil || row.Metadata == "" {
		return time.Time{}, false
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(row.Metadata), &meta); err != nil {
		return time.Time{}, false
	}
	raw, _ := meta[lastSteeredAtMetadataKey].(string)
	if raw == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}
