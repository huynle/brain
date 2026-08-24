package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Goal API surface
//
// These methods expose goal automations over the API: create/update/list/run a
// goal, fetch goal-scoped linked-task progress, and fetch reconcile audit
// history. They sit on top of the existing reconcile core (Reconcile), the goal
// automation builder (BuildGoalAutomation), and the brain/store collaborators.
// =============================================================================

// CreateGoalRequest is the input for creating a goal automation over the API.
// Aliases types.CreateGoalRequest (the shared API contract).
type CreateGoalRequest = types.CreateGoalRequest

// GoalSummary is a serializable view of a goal automation entry returned by the
// goal API (list/create/update). Aliases types.GoalSummary.
type GoalSummary = types.GoalSummary

// GoalProgressResponse reports goal-scoped linked-task progress derived from the
// goal's feature tasks. Aliases types.GoalProgressResponse.
type GoalProgressResponse = types.GoalProgressResponse

// CreateGoal builds a goal automation entry from the request and persists it via
// the brain service, returning a summary of the created goal.
//
// When Config.ID is empty the server derives a slug from the title (lowercase,
// [a-z0-9-], dashes collapsed) and de-dupes it against existing goal IDs with
// -2/-3… suffixes. Creation fails only when both the ID and the title are
// empty.
func (s *GoalService) CreateGoal(ctx context.Context, req CreateGoalRequest) (*GoalSummary, error) {
	if s == nil || s.brain == nil {
		return nil, fmt.Errorf("goal create: brain service is nil")
	}

	if strings.TrimSpace(req.Config.ID) == "" {
		if strings.TrimSpace(req.Title) == "" {
			return nil, fmt.Errorf("goal create: goal id and title are both empty")
		}
		id, err := s.generateGoalID(ctx, req.Title)
		if err != nil {
			return nil, fmt.Errorf("goal create: generate goal id: %w", err)
		}
		req.Config.ID = id
	}

	entry, err := BuildGoalAutomation(GoalInput{
		Project:   req.Project,
		FeatureID: req.FeatureID,
		Title:     req.Title,
		Content:   req.Content,
		Config:    req.Config,
		Action:    req.Action,
	})
	if err != nil {
		return nil, fmt.Errorf("goal create: %w", err)
	}

	createReq := types.CreateEntryRequest{
		Type:        entry.Type,
		Title:       entry.Title,
		Content:     entry.Content,
		Status:      entry.Status,
		Project:     entry.ProjectID,
		FeatureID:   entry.FeatureID,
		Tags:        entry.Tags,
		GeneratedBy: entry.GeneratedBy,
		Trigger:     entry.Trigger,
		Action:      entry.Action,
		Goal:        entry.Goal,
	}

	resp, err := s.brain.Save(ctx, createReq)
	if err != nil {
		return nil, fmt.Errorf("goal create: save: %w", err)
	}

	return &GoalSummary{
		EntryID:   resp.ID,
		GoalID:    entry.Goal.ID,
		Title:     entry.Title,
		Project:   entry.ProjectID,
		FeatureID: entry.FeatureID,
		Status:    entry.Status,
		Config:    entry.Goal,
		Action:    entry.Action,
		Trigger:   entry.Trigger,
	}, nil
}

// generateGoalID derives a unique goal id from a title: slugified, then
// de-duped against existing goal IDs (any status) with -2/-3… suffixes.
func (s *GoalService) generateGoalID(ctx context.Context, title string) (string, error) {
	base := goalV1Slug(title)

	existing, err := s.listGoalEntries(ctx, "")
	if err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(existing))
	for _, g := range existing {
		if g.Goal != nil && g.Goal.ID != "" {
			taken[g.Goal.ID] = true
		}
	}

	if !taken[base] {
		return base, nil
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[candidate] {
			return candidate, nil
		}
	}
}

// UpdateGoalRequest is the input for updating an existing goal automation.
// Provided fields are merged onto the existing entry's goal config/action; nil
// fields are left unchanged. The goal's trigger is rebuilt from the merged
// config so trigger source / status changes take effect. Aliases
// types.UpdateGoalRequest.
type UpdateGoalRequest = types.UpdateGoalRequest

// UpdateGoal merges the request onto the existing goal entry (located by goal
// ID), rebuilds the goal automation (config/action/trigger), and persists the
// changes via the brain service.
func (s *GoalService) UpdateGoal(ctx context.Context, goalID string, req UpdateGoalRequest) (*GoalSummary, error) {
	if s == nil || s.brain == nil {
		return nil, fmt.Errorf("goal update: brain service is nil")
	}

	existing, err := s.findGoalByID(ctx, goalID)
	if err != nil {
		return nil, err
	}

	// Merge config from the existing goal config.
	cfg := *existing.Goal
	if req.Criteria != nil {
		cfg.Criteria = *req.Criteria
	}
	if req.Validation != nil {
		cfg.Validation = *req.Validation
	}
	if req.Workdir != nil {
		cfg.Workdir = *req.Workdir
	}
	if req.TriggerSource != nil {
		cfg.TriggerSource = *req.TriggerSource
	}
	if req.TaskID != nil {
		cfg.TaskID = *req.TaskID
	}
	if req.CompleteStatuses != nil {
		cfg.CompleteStatuses = *req.CompleteStatuses
	}
	if req.BlockedStatuses != nil {
		cfg.BlockedStatuses = *req.BlockedStatuses
	}
	if req.Steering != nil {
		steering := *req.Steering
		cfg.Steering = &steering
	}

	// Merge action.
	action := types.AutomationAction{}
	if existing.Action != nil {
		action = *existing.Action
	}
	if req.Action != nil {
		action = *req.Action
	}

	title := existing.Title
	if req.Title != nil {
		title = *req.Title
	}
	content := existing.Content
	if req.Content != nil {
		content = *req.Content
	}

	// Rebuild the goal automation so the trigger reflects merged config.
	rebuilt, err := BuildGoalAutomation(GoalInput{
		Project:   existing.ProjectID,
		FeatureID: existing.FeatureID,
		Title:     title,
		Content:   content,
		Config:    cfg,
		Action:    action,
	})
	if err != nil {
		return nil, fmt.Errorf("goal update: rebuild: %w", err)
	}

	update := types.UpdateEntryRequest{
		Title:   &rebuilt.Title,
		Content: &rebuilt.Content,
		Trigger: rebuilt.Trigger,
		Action:  rebuilt.Action,
		Goal:    rebuilt.Goal,
	}
	if req.Status != nil {
		update.Status = req.Status
	}

	updated, err := s.brain.Update(ctx, existing.ID, update)
	if err != nil {
		return nil, fmt.Errorf("goal update: save: %w", err)
	}

	status := rebuilt.Status
	if req.Status != nil {
		status = *req.Status
	} else if updated != nil && updated.Status != "" {
		status = updated.Status
	}

	return &GoalSummary{
		EntryID:   existing.ID,
		GoalID:    rebuilt.Goal.ID,
		Title:     rebuilt.Title,
		Project:   existing.ProjectID,
		FeatureID: existing.FeatureID,
		Status:    status,
		Config:    rebuilt.Goal,
		Action:    rebuilt.Action,
		Trigger:   rebuilt.Trigger,
	}, nil
}

// defaultGoalListStatuses is the status set returned by goal listing when no
// explicit status filter is given: everything a user still cares about.
// Archived goals are hidden unless requested (?status=archived or all).
var defaultGoalListStatuses = map[string]bool{
	"active":    true,
	"blocked":   true,
	"completed": true,
}

// ListGoals returns goal automation summaries with the default status set
// (active + blocked + completed; archived hidden), optionally filtered by
// project and/or feature ID.
func (s *GoalService) ListGoals(ctx context.Context, project, featureID string) ([]GoalSummary, error) {
	return s.ListGoalsFiltered(ctx, project, featureID, "")
}

// ListGoalsFiltered returns goal automation summaries filtered by project,
// feature ID, and status. Status semantics:
//
//	""         -> default set (active, blocked, completed; archived hidden)
//	"all"      -> every status
//	"<status>" -> exact match (e.g. "archived", "blocked")
func (s *GoalService) ListGoalsFiltered(ctx context.Context, project, featureID, status string) ([]GoalSummary, error) {
	if s == nil || s.brain == nil {
		return nil, fmt.Errorf("goal list: brain service is nil")
	}

	status = strings.TrimSpace(strings.ToLower(status))
	queryStatus := status
	if status == "" || status == "all" {
		queryStatus = ""
	}

	goals, err := s.listGoalEntries(ctx, queryStatus)
	if err != nil {
		return nil, fmt.Errorf("goal list: %w", err)
	}

	out := make([]GoalSummary, 0, len(goals))
	for _, g := range goals {
		if status == "" && !defaultGoalListStatuses[g.Status] {
			continue
		}
		if project != "" && g.ProjectID != project {
			continue
		}
		if featureID != "" && g.FeatureID != featureID {
			continue
		}
		out = append(out, goalSummaryFromEntry(g))
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].GoalID < out[j].GoalID
	})

	return out, nil
}

// DeleteGoal permanently deletes the goal automation entry identified by its
// goal ID (any status). Returns ErrGoalNotFound for unknown goals.
func (s *GoalService) DeleteGoal(ctx context.Context, goalID string) error {
	if s == nil || s.brain == nil {
		return fmt.Errorf("goal delete: brain service is nil")
	}

	goal, err := s.findGoalByID(ctx, goalID)
	if err != nil {
		return err
	}
	if err := s.brain.Delete(ctx, goal.ID); err != nil {
		return fmt.Errorf("goal delete: %w", err)
	}
	return nil
}

// RunGoal triggers a manual reconcile for the goal identified by goalID and
// returns the resulting audit record.
func (s *GoalService) RunGoal(ctx context.Context, goalID string) (*GoalReconcileAudit, error) {
	if s == nil || s.brain == nil {
		return nil, fmt.Errorf("goal run: brain service is nil")
	}

	goal, err := s.findGoalByID(ctx, goalID)
	if err != nil {
		return nil, err
	}

	audit, err := s.Reconcile(ctx, *goal, types.Event{})
	if err != nil {
		return nil, fmt.Errorf("goal run: reconcile: %w", err)
	}
	return audit, nil
}

// GoalProgress computes goal-scoped linked-task progress by reusing the
// feature-task lister and feature status computation.
func (s *GoalService) GoalProgress(ctx context.Context, goalID string) (*GoalProgressResponse, error) {
	if s == nil || s.brain == nil {
		return nil, fmt.Errorf("goal progress: brain service is nil")
	}

	goal, err := s.findGoalByID(ctx, goalID)
	if err != nil {
		return nil, err
	}

	tasks, err := s.linkedTasksForGoal(ctx, *goal)
	if err != nil {
		return nil, fmt.Errorf("goal progress: list tasks: %w", err)
	}

	stats := computeTaskStats(tasks)
	return &GoalProgressResponse{
		GoalID:        goal.Goal.ID,
		EntryID:       goal.ID,
		Project:       goal.ProjectID,
		FeatureID:     goal.FeatureID,
		TaskID:        goalTaskScope(goal.Goal),
		FeatureStatus: ComputeFeatureStatus(tasks),
		Total:         stats.Total,
		Pending:       stats.Pending,
		InProgress:    stats.InProgress,
		Completed:     stats.Completed,
		Blocked:       stats.Blocked,
		Tasks:         linkedTaskSnapshot(tasks),
	}, nil
}

// Bounds for the over-fetch that lets goal-scoped audit filtering see past
// other goals' reconcile traffic.
//
// goalAuditMaxScan matches storage.GetEventsByType's own cap. An earlier value
// of 5000 was simply unreachable — that method clamps any limit above 1000 — so
// the constant and its comment described a window four times wider than the
// query could ever return. Naming the real bound keeps the two from disagreeing;
// if the storage cap moves, this is the line to move with it.
const (
	goalAuditOverfetch = 40
	goalAuditMaxScan   = 1000
)

// GoalAuditHistory returns the reconcile audit history for a goal, newest
// first. It reads goal.reconcile events from the event log and filters them to
// the requested goal ID. A non-positive limit defaults to 50.
func (s *GoalService) GoalAuditHistory(ctx context.Context, goalID string, limit int) ([]GoalReconcileAudit, error) {
	if s == nil {
		return nil, fmt.Errorf("goal audit: service is nil")
	}
	if s.store == nil {
		return []GoalReconcileAudit{}, nil
	}
	if limit <= 0 {
		limit = 50
	}

	// Confirm the goal exists before reporting on its history. This was the one
	// goal method that skipped findGoalByID — GoalProgress, RunGoal, UpdateGoal
	// and DeleteGoal all call it and all surface ErrGoalNotFound — so a typo'd
	// or deleted goal id rendered "No reconcile audit records found for goal X",
	// which is exactly what a real goal that has never reconciled looks like.
	if _, err := s.findGoalByID(ctx, goalID); err != nil {
		return nil, err
	}

	// Over-fetch so goal-scoped filtering still satisfies the requested limit.
	//
	// goal.reconcile events are stored for ALL goals in one stream and filtered
	// to this goal afterwards, so the window has to be wide enough to reach past
	// every other goal's traffic. The reconcile ticker fires every 5 minutes per
	// active goal, so on an instance with a handful of goals the old limit*10
	// covered well under a day — and a goal that last reconciled before that
	// reported no history at all. Same filter-after-limit shape as the
	// automation_runs and list(filename:) fixes; bounded for the same reason.
	// The effective window is 1000 rows, which is where GetEventsByType clamps
	// regardless of what we ask for.
	scan := limit * goalAuditOverfetch
	if scan > goalAuditMaxScan {
		scan = goalAuditMaxScan
	}
	rows, err := s.store.GetEventsByType(ctx, types.EventGoalReconcile, scan)
	if err != nil {
		return nil, fmt.Errorf("goal audit: query events: %w", err)
	}

	out := make([]GoalReconcileAudit, 0, limit)
	for _, row := range rows {
		var audit GoalReconcileAudit
		if err := json.Unmarshal([]byte(row.Payload), &audit); err != nil {
			continue
		}
		if audit.GoalID != goalID {
			continue
		}
		out = append(out, audit)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// findGoalByID locates a goal automation entry by its goal ID, regardless of
// entry status. Status-agnostic lookup is what makes the lifecycle work:
// paused (blocked), completed, and archived goals must remain reachable so
// they can be resumed, re-run, inspected, or deleted. Event dispatch stays
// active-only via listActiveGoals.
func (s *GoalService) findGoalByID(ctx context.Context, goalID string) (*types.BrainEntry, error) {
	goalID = strings.TrimSpace(goalID)
	if goalID == "" {
		return nil, fmt.Errorf("goal: missing goal id")
	}
	goals, err := s.listGoalEntries(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("goal: list goals: %w", err)
	}
	for i := range goals {
		if goals[i].Goal != nil && goals[i].Goal.ID == goalID {
			g := goals[i]
			return &g, nil
		}
	}
	return nil, fmt.Errorf("goal %q: %w", goalID, ErrGoalNotFound)
}

// goalSummaryFromEntry maps a goal automation entry to its API summary.
func goalSummaryFromEntry(e types.BrainEntry) GoalSummary {
	goalID := ""
	if e.Goal != nil {
		goalID = e.Goal.ID
	}
	return GoalSummary{
		EntryID:   e.ID,
		GoalID:    goalID,
		Title:     e.Title,
		Project:   e.ProjectID,
		FeatureID: e.FeatureID,
		Status:    e.Status,
		Config:    e.Goal,
		Action:    e.Action,
		Trigger:   e.Trigger,
	}
}
