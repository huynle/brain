package service

import (
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

// ReconcileDecision is the outcome of the deterministic reconcile decision.
//
// The reconcile core inspects a goal's config and its linked task states and
// produces exactly one of these decisions. The decision is pure (no I/O, no
// LLM) and deterministic given the same inputs.
type ReconcileDecision string

const (
	// ReconcileComplete means every linked task counts as complete; the goal
	// is satisfied and can be marked done.
	ReconcileComplete ReconcileDecision = "complete"
	// ReconcileBlock means linked work is blocked with nothing active; the
	// goal is stuck and should surface as blocked.
	ReconcileBlock ReconcileDecision = "block"
	// ReconcileNeedWork means more work must be generated (no linked tasks, or
	// only un-started pending work remains with nothing active/blocked).
	ReconcileNeedWork ReconcileDecision = "need_work"
	// ReconcileNoop means work is already in progress; the reconcile loop
	// should do nothing and wait.
	ReconcileNoop ReconcileDecision = "noop"
)

// LinkedTaskSnapshot is a serializable snapshot of a goal's linked task,
// captured for the reconcile audit record.
type LinkedTaskSnapshot struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

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
