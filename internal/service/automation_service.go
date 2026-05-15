package service

import (
	"context"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
)

// AutomationService evaluates automation entries against events.
type AutomationService struct {
	brain *BrainServiceImpl
}

// NewAutomationService creates an automation evaluator backed by brain entries.
func NewAutomationService(brain *BrainServiceImpl) *AutomationService {
	return &AutomationService{brain: brain}
}

// Start subscribes to EventHub events until the context is cancelled.
func (s *AutomationService) Start(ctx context.Context, hub *realtime.EventHub) {
	if s == nil || hub == nil {
		return
	}

	ch, unsub := hub.Subscribe(realtime.EventFilter{})
	defer unsub()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	_ = s.CheckScheduled(ctx, time.Now().UTC())

	seen := make(map[string]struct{})
	process := func(evt types.Event) {
		if evt.ID != "" {
			if _, ok := seen[evt.ID]; ok {
				return
			}
			seen[evt.ID] = struct{}{}
		}
		_ = s.HandleEvent(ctx, evt)
	}

	for _, evt := range hub.Replay("") {
		process(evt)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			_ = s.CheckScheduled(ctx, now.UTC())
		case evt, ok := <-ch:
			if !ok {
				return
			}
			process(evt)
		}
	}
}

// CheckScheduled evaluates cron automation entries at the provided time.
func (s *AutomationService) CheckScheduled(ctx context.Context, now time.Time) error {
	if s == nil || s.brain == nil {
		return nil
	}

	automations, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:   "automation",
		Status: "active",
		Limit:  1000,
	})
	if err != nil {
		return fmt.Errorf("list active automations: %w", err)
	}

	for _, automation := range automations.Entries {
		if automation.Trigger == nil || automation.Action == nil || automation.Trigger.Type != "cron" {
			continue
		}
		if automation.Trigger.Schedule == "" {
			continue
		}

		schedule, err := cron.Parse(automation.Trigger.Schedule)
		if err != nil {
			continue
		}
		if !schedule.Matches(now.UTC()) {
			continue
		}

		generatedKey := fmt.Sprintf("automation:cron:%s:%s", automation.ID, now.UTC().Format("200601021504"))
		if err := s.createTask(ctx, automation, types.Event{ProjectID: automation.ProjectID}, generatedKey); err != nil {
			return err
		}
	}

	return nil
}

// HandleEvent evaluates automations for a single event.
func (s *AutomationService) HandleEvent(ctx context.Context, evt types.Event) error {
	if s == nil || s.brain == nil {
		return nil
	}

	automations, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:   "automation",
		Status: "active",
		Limit:  1000,
	})
	if err != nil {
		return fmt.Errorf("list active automations: %w", err)
	}

	for _, automation := range automations.Entries {
		if !automationMatchesEvent(automation, evt) {
			continue
		}

		if err := s.createTask(ctx, automation, evt, ""); err != nil {
			return err
		}
	}

	return nil
}

func automationMatchesEvent(automation types.BrainEntry, evt types.Event) bool {
	if automation.Trigger == nil || automation.Action == nil {
		return false
	}
	switch automation.Trigger.Type {
	case "event":
		return automationMatchesNamedEvent(automation, evt)
	case "session":
		return automationMatchesSession(automation, evt)
	default:
		return false
	}
}

func automationMatchesNamedEvent(automation types.BrainEntry, evt types.Event) bool {
	if !types.MatchEventPattern(automation.Trigger.Event, evt.Type) {
		return false
	}
	if automation.ProjectID != "" && automation.ProjectID != evt.ProjectID {
		if automation.Trigger.Filter["project"] != "*" && automation.Trigger.Filter["project_id"] != "*" {
			return false
		}
	}
	return matchAutomationFilters(automation.Trigger.Filter, evt)
}

func automationMatchesSession(automation types.BrainEntry, evt types.Event) bool {
	if evt.Type != types.EventRunnerSessionDiscovered {
		return false
	}
	if automation.ProjectID != "" && automation.ProjectID != evt.ProjectID {
		if automation.Trigger.Filter["project"] != "*" && automation.Trigger.Filter["project_id"] != "*" {
			return false
		}
	}
	return matchAutomationFilters(automation.Trigger.Filter, evt)
}

func matchAutomationFilters(filters map[string]string, evt types.Event) bool {
	for key, expected := range filters {
		if expected == "*" {
			continue
		}

		actual := getEventField(evt, key)
		if key == "project" {
			actual = evt.ProjectID
		}
		if actual != expected {
			return false
		}
	}
	return true
}

func (s *AutomationService) createTask(ctx context.Context, automation types.BrainEntry, evt types.Event, generatedKeyOverride string) error {
	project := automation.ProjectID
	if project == "" {
		project = evt.ProjectID
	}
	if skip, err := s.shouldSkipTaskGeneration(ctx, project, automation); err != nil {
		return err
	} else if skip {
		return nil
	}

	generated := true
	generatedKey := automationGeneratedKey(automation, evt)
	if generatedKeyOverride != "" {
		generatedKey = generatedKeyOverride
	}
	if generatedKey != "" {
		exists, err := s.generatedTaskExists(ctx, project, generatedKey)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	}

	req := types.CreateEntryRequest{
		Type:           "task",
		Title:          fmt.Sprintf("Automation: %s", automation.ID),
		Content:        automation.Action.DirectPrompt,
		Status:         "pending",
		Project:        project,
		Generated:      &generated,
		GeneratedBy:    fmt.Sprintf("automation:%s", automation.ID),
		GeneratedKey:   generatedKey,
		DirectPrompt:   automation.Action.DirectPrompt,
		Agent:          automation.Action.Agent,
		Model:          automation.Action.Model,
		ExecutionMode:  automation.Action.ExecutionMode,
		CompleteOnIdle: automation.Action.CompleteOnIdle,
	}

	if automation.Action.Type == "script" {
		req.Executor = "script"
		req.Content = automation.Action.Command
		req.DirectPrompt = automation.Action.Command
	}

	_, err := s.brain.Save(ctx, req)
	if err != nil {
		return fmt.Errorf("create automation task: %w", err)
	}
	return nil
}

func (s *AutomationService) shouldSkipTaskGeneration(ctx context.Context, project string, automation types.BrainEntry) (bool, error) {
	if automation.Trigger == nil || (automation.Trigger.MaxConcurrent <= 0 && automation.Trigger.Cooldown == "") {
		return false, nil
	}

	tasks, err := s.listAutomationGeneratedTasks(ctx, project, automation.ID)
	if err != nil {
		return false, err
	}

	if automation.Trigger.MaxConcurrent > 0 && countRunnableGeneratedTasks(tasks) >= automation.Trigger.MaxConcurrent {
		return true, nil
	}

	if automation.Trigger.Cooldown != "" && cooldownActive(tasks, automation.Trigger.Cooldown, types.TimeNowUTC()) {
		return true, nil
	}

	return false, nil
}

func (s *AutomationService) listAutomationGeneratedTasks(ctx context.Context, project, automationID string) ([]types.BrainEntry, error) {
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   1000,
	})
	if err != nil {
		return nil, fmt.Errorf("list automation generated tasks: %w", err)
	}

	generatedBy := fmt.Sprintf("automation:%s", automationID)
	tasks := make([]types.BrainEntry, 0)
	for _, task := range resp.Entries {
		if task.GeneratedBy == generatedBy {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

func countRunnableGeneratedTasks(tasks []types.BrainEntry) int {
	count := 0
	for _, task := range tasks {
		switch task.Status {
		case "pending", "in_progress", "active":
			count++
		}
	}
	return count
}

func cooldownActive(tasks []types.BrainEntry, cooldown string, now time.Time) bool {
	duration, err := time.ParseDuration(cooldown)
	if err != nil {
		return false
	}

	lastGenerated, ok := latestGeneratedTaskTime(tasks)
	if !ok {
		return false
	}

	return now.UTC().Sub(lastGenerated) < duration
}

func latestGeneratedTaskTime(tasks []types.BrainEntry) (time.Time, bool) {
	var latest time.Time
	found := false
	for _, task := range tasks {
		if task.Created == "" {
			continue
		}
		created, err := time.Parse(time.RFC3339, task.Created)
		if err != nil {
			continue
		}
		if !found || created.After(latest) {
			latest = created
			found = true
		}
	}
	return latest, found
}

func (s *AutomationService) generatedTaskExists(ctx context.Context, project, generatedKey string) (bool, error) {
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:    "task",
		Project: project,
		Limit:   1000,
	})
	if err != nil {
		return false, fmt.Errorf("list generated tasks: %w", err)
	}
	for _, task := range resp.Entries {
		if task.GeneratedKey == generatedKey {
			return true, nil
		}
	}
	return false, nil
}

func automationGeneratedKey(automation types.BrainEntry, evt types.Event) string {
	if automation.Trigger == nil || automation.Trigger.OncePer == "" {
		return ""
	}
	return fmt.Sprintf("automation:%s:%s", automation.ID, getEventField(evt, automation.Trigger.OncePer))
}
