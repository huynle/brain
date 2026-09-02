package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// actionTask generates the task a reminder's "task" action asks for.
//
// Shaped after AutomationService.createTask, including what it deliberately
// does NOT set.
func (s *ReminderService) actionTask(ctx context.Context, e types.BrainEntry) (string, error) {
	cfg := e.Reminder
	if cfg == nil {
		return "", fmt.Errorf("reminder entry %s has no config", e.ID)
	}
	prompt := strings.TrimSpace(cfg.Prompt)
	if prompt == "" {
		// BuildReminderEntry refuses this at creation; reaching it here means
		// the file was hand-edited. Fail loudly rather than dispatching an
		// agent with an empty instruction.
		return "", fmt.Errorf("reminder %s has action=task but no prompt", cfg.ID)
	}

	generated := true
	req := types.CreateEntryRequest{
		Type:  "task",
		Title: fmt.Sprintf("Reminder: %s", e.Title),
		// Content reaches the file body; DirectPrompt is what the executor's
		// prompt builder actually reads. Both, or the agent gets an empty
		// instruction while the file looks right. Content additionally cannot
		// be empty — POST /entries rejects that.
		Content:      prompt,
		DirectPrompt: prompt,
		// pending, not the entries default of active: only a pending task is
		// dispatchable by the scheduler.
		Status:    "pending",
		Project:   e.ProjectID,
		FeatureID: e.FeatureID,
		Generated: &generated,
		// Ties the task back to the reminder, and makes a duplicate visible.
		GeneratedBy:  "reminder:" + cfg.ID,
		GeneratedKey: fmt.Sprintf("reminder:%s:%s", cfg.ID, cfg.RemindAt),

		Agent:         cfg.Agent,
		Model:         cfg.Model,
		Executor:      cfg.Executor,
		ExecutionMode: cfg.ExecutionMode,
		TargetWorkdir: cfg.TargetWorkdir,

		// Origin provenance is deliberately NOT set, matching automation- and
		// goal-generated tasks. ResolveMachineAffinity defaults to
		// "preferred" whenever an origin machine is known, which is +75 in
		// scoreMachine — stamping this process would pin every
		// reminder-generated task to the API host.
	}

	resp, err := s.brain.Save(ctx, req)
	if err != nil {
		return "", fmt.Errorf("create reminder task: %w", err)
	}
	if resp == nil {
		return "", nil
	}
	return resp.ID, nil
}
