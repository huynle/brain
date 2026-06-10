package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
)

// AutomationService evaluates automation entries against events.
type AutomationService struct {
	brain        *BrainServiceImpl
	pauseChecker automationPauseChecker
}

type automationPauseChecker interface {
	IsAutomationsPaused() bool
}

// NewAutomationService creates an automation evaluator backed by brain entries.
func NewAutomationService(brain *BrainServiceImpl) *AutomationService {
	return &AutomationService{brain: brain}
}

// SetPauseChecker lets API runner pause state suppress automation task generation.
func (s *AutomationService) SetPauseChecker(checker automationPauseChecker) {
	if s == nil {
		return
	}
	s.pauseChecker = checker
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
		if s.isAutomationPaused(automation, types.Event{}) {
			_, err := s.createRunAudit(ctx, automationRunAudit{
				automation: automation,
				evt:        types.Event{ProjectID: automation.ProjectID},
				project:    automation.ProjectID,
				status:     "skipped",
				skipReason: "paused",
			})
			if err != nil {
				return err
			}
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
		if s.isAutomationPaused(automation, evt) {
			project := automation.ProjectID
			if project == "" {
				project = evt.ProjectID
			}
			_, err := s.createRunAudit(ctx, automationRunAudit{
				automation: automation,
				evt:        evt,
				project:    project,
				status:     "skipped",
				skipReason: "paused",
			})
			if err != nil {
				return err
			}
			continue
		}

		if err := s.createTask(ctx, automation, evt, ""); err != nil {
			return err
		}
	}

	return nil
}

func (s *AutomationService) isAutomationPaused(automation types.BrainEntry, evt types.Event) bool {
	if s == nil || s.pauseChecker == nil {
		return false
	}
	return s.pauseChecker.IsAutomationsPaused()
}

func automationMatchesEvent(automation types.BrainEntry, evt types.Event) bool {
	if automation.Trigger == nil || automation.Action == nil {
		return false
	}
	switch automation.Trigger.Type {
	case "event":
		return automationMatchesNamedEvent(automation, evt)
	case "webhook":
		return automationMatchesWebhook(automation, evt)
	case "session":
		return automationMatchesSession(automation, evt)
	default:
		return false
	}
}

func automationMatchesNamedEvent(automation types.BrainEntry, evt types.Event) bool {
	if !automation.Trigger.MatchesEvent(evt.Type) {
		return false
	}
	if automation.ProjectID != "" && automation.ProjectID != evt.ProjectID {
		if automation.Trigger.Filter["project"] != "*" && automation.Trigger.Filter["project_id"] != "*" {
			return false
		}
	}
	return matchAutomationFilters(automation.Trigger.Filter, evt)
}

func automationMatchesWebhook(automation types.BrainEntry, evt types.Event) bool {
	if evt.Type != "webhook.received" {
		return false
	}
	if automation.ProjectID != "" && automation.ProjectID != evt.ProjectID {
		if automation.Trigger.Filter["project"] != "*" && automation.Trigger.Filter["project_id"] != "*" {
			return false
		}
	}
	incomingPath := getEventField(evt, "webhook_path")
	if normalizeWebhookPath(incomingPath) != normalizeWebhookPath(automation.Trigger.Webhook) {
		return false
	}
	return matchAutomationFilters(automation.Trigger.Filter, evt)
}

func normalizeWebhookPath(path string) string {
	return strings.Trim(path, "/")
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
	for key, expr := range filters {
		actual := getEventField(evt, key)
		if key == "project" {
			actual = evt.ProjectID
		}
		if !types.MatchFilterValue(actual, expr) {
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
	if skip, reason, err := s.shouldSkipTaskGeneration(ctx, project, automation); err != nil {
		return err
	} else if skip {
		_, err := s.createRunAudit(ctx, automationRunAudit{
			automation: automation,
			evt:        evt,
			project:    project,
			status:     "skipped",
			skipReason: reason,
		})
		if err != nil {
			return err
		}
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
			_, err := s.createRunAudit(ctx, automationRunAudit{
				automation:   automation,
				evt:          evt,
				project:      project,
				status:       "skipped",
				generatedKey: generatedKey,
				skipReason:   "dedup",
			})
			if err != nil {
				return err
			}
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
		SessionMode:    automation.Action.SessionMode,
		CompleteOnIdle: automation.Action.CompleteOnIdle,
	}

	if automation.Action.Type == "script" {
		req.Executor = "script"
		req.Content = automation.Action.Command
		req.DirectPrompt = automation.Action.Command
	}

	taskResp, err := s.brain.Save(ctx, req)
	if err != nil {
		return fmt.Errorf("create automation task: %w", err)
	}
	runID, err := s.createRunAudit(ctx, automationRunAudit{
		automation:   automation,
		evt:          evt,
		project:      project,
		status:       "queued",
		generatedKey: generatedKey,
		taskIDs:      []string{taskResp.ID},
	})
	if err != nil {
		return err
	}
	if runID != "" {
		_, err = s.brain.Update(ctx, taskResp.ID, types.UpdateEntryRequest{AutomationRunID: &runID})
		if err != nil {
			return fmt.Errorf("link automation task to run audit: %w", err)
		}
	}
	return nil
}

type automationRunAudit struct {
	automation   types.BrainEntry
	evt          types.Event
	project      string
	status       string
	generatedKey string
	taskIDs      []string
	errorText    string
	skipReason   string
}

func (s *AutomationService) createRunAudit(ctx context.Context, audit automationRunAudit) (string, error) {
	if s == nil || s.brain == nil {
		return "", nil
	}
	started := types.TimeNowUTC().UTC()
	triggerType := "manual"
	triggerEvent := audit.evt.Type
	if audit.automation.Trigger != nil {
		triggerType = audit.automation.Trigger.Type
		if triggerEvent == "" {
			switch audit.automation.Trigger.Type {
			case "event":
				triggerEvent = audit.automation.Trigger.Event
			case "cron":
				triggerEvent = audit.automation.Trigger.Schedule
			case "webhook":
				triggerEvent = audit.automation.Trigger.Webhook
			case "session":
				triggerEvent = audit.automation.Trigger.Event
				if triggerEvent == "" {
					triggerEvent = types.EventRunnerSessionDiscovered
				}
			}
		}
	}

	var content strings.Builder
	content.WriteString("## Automation Run Audit\n\n")
	content.WriteString(fmt.Sprintf("automation_id: %s\n", audit.automation.ID))
	content.WriteString(fmt.Sprintf("automation_path: %s\n", audit.automation.Path))
	content.WriteString(fmt.Sprintf("project: %s\n", audit.project))
	content.WriteString(fmt.Sprintf("trigger_type: %s\n", triggerType))
	if triggerEvent != "" {
		content.WriteString(fmt.Sprintf("trigger_event: %s\n", triggerEvent))
	}
	if audit.evt.ID != "" {
		content.WriteString(fmt.Sprintf("source_event_id: %s\n", audit.evt.ID))
	}
	if audit.generatedKey != "" {
		content.WriteString(fmt.Sprintf("dedup_key: %s\n", audit.generatedKey))
	}
	content.WriteString(fmt.Sprintf("started_at: %s\n", started.Format(time.RFC3339)))
	content.WriteString(fmt.Sprintf("completed_at: %s\n", started.Format(time.RFC3339)))
	content.WriteString("duration_ms: 0\n")
	if audit.skipReason != "" {
		content.WriteString(fmt.Sprintf("skip_reason: %s\n", audit.skipReason))
	}
	if audit.errorText != "" {
		content.WriteString(fmt.Sprintf("error: %s\n", audit.errorText))
	}
	content.WriteString("\n### Trigger Payload Summary\n")
	content.WriteString(summarizeAutomationEvent(audit.evt))
	content.WriteString("\n### Generated Tasks\n")
	if len(audit.taskIDs) == 0 {
		content.WriteString("- none\n")
	} else {
		for _, id := range audit.taskIDs {
			content.WriteString(fmt.Sprintf("- %s\n", id))
		}
	}

	resp, err := s.brain.Save(ctx, types.CreateEntryRequest{
		Type:    "automation_run",
		Title:   fmt.Sprintf("Automation Run: %s", audit.automation.ID),
		Content: content.String(),
		Status:  audit.status,
		Project: audit.project,
	})
	if err != nil {
		return "", fmt.Errorf("create automation run audit: %w", err)
	}
	return resp.ID, nil
}

func summarizeAutomationEvent(evt types.Event) string {
	lines := make([]string, 0, 8)
	if evt.Type != "" {
		lines = append(lines, fmt.Sprintf("- type: %s", evt.Type))
	}
	if evt.ProjectID != "" {
		lines = append(lines, fmt.Sprintf("- project_id: %s", evt.ProjectID))
	}
	if evt.TaskID != "" {
		lines = append(lines, fmt.Sprintf("- task_id: %s", evt.TaskID))
	}
	if evt.FeatureID != "" {
		lines = append(lines, fmt.Sprintf("- feature_id: %s", evt.FeatureID))
	}
	if evt.FromStatus != "" {
		lines = append(lines, fmt.Sprintf("- from_status: %s", evt.FromStatus))
	}
	if evt.ToStatus != "" {
		lines = append(lines, fmt.Sprintf("- to_status: %s", evt.ToStatus))
	}
	for _, key := range []string{"session_id", "webhook_path", "runner_id"} {
		if value := getEventField(evt, key); value != "" {
			lines = append(lines, fmt.Sprintf("- %s: %s", key, value))
		}
	}
	if len(lines) == 0 {
		return "- none\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

func (s *AutomationService) shouldSkipTaskGeneration(ctx context.Context, project string, automation types.BrainEntry) (bool, string, error) {
	if automation.Trigger == nil || (automation.Trigger.MaxConcurrent <= 0 && automation.Trigger.Cooldown == "") {
		return false, "", nil
	}

	tasks, err := s.listAutomationGeneratedTasks(ctx, project, automation.ID)
	if err != nil {
		return false, "", err
	}

	if automation.Trigger.MaxConcurrent > 0 && countRunnableGeneratedTasks(tasks) >= automation.Trigger.MaxConcurrent {
		return true, "max_concurrent", nil
	}

	if automation.Trigger.Cooldown != "" && cooldownActive(tasks, automation.Trigger.Cooldown, types.TimeNowUTC()) {
		return true, "cooldown", nil
	}

	return false, "", nil
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
