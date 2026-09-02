package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
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

// automationProjectPauseChecker is an optional extension implemented by
// pause checkers that support per-project scoping. When present, the
// automation service consults it before falling back to the global check.
type automationProjectPauseChecker interface {
	IsAutomationsPausedForProject(projectID string) bool
}

// NewAutomationService creates an automation evaluator backed by brain entries.
func NewAutomationService(brain *BrainServiceImpl) *AutomationService {
	return &AutomationService{brain: brain}
}

// RunAutomationNow manually triggers one automation through the exact task-
// generation path the cron/event dispatchers use (createTask), so a manual
// run can never behave differently from a scheduled one. The dedup key is
// uniquified per invocation, and the pause gate is intentionally NOT applied
// — a manual run is an explicit user override. Returns the created task id,
// or "" when generation was skipped (e.g. max_concurrent reached; the skip
// is recorded in the run audit).
func (s *AutomationService) RunAutomationNow(ctx context.Context, pathOrID string) (string, error) {
	entry, err := s.brain.Recall(ctx, pathOrID)
	if err != nil {
		return "", err
	}
	if entry.Type != "automation" {
		return "", fmt.Errorf("entry %s is not an automation (type %q)", pathOrID, entry.Type)
	}
	if entry.Action == nil {
		return "", fmt.Errorf("automation %s has no action", entry.ID)
	}
	evt := types.Event{
		Type:      "manual",
		Source:    "api",
		Timestamp: time.Now().UTC(),
		ProjectID: entry.ProjectID,
	}
	if types.NormalizeAutomationActionType(entry.Action.Type) ==
		types.AutomationActionUpdate {
		// A manual run of an update automation has no event to scope it,
		// and an unscoped bulk write is the one thing this action must
		// never do. Refuse rather than guess a target.
		return "", fmt.Errorf(
			"automation %s has an update action: it applies to the feature its "+
				"trigger names, so there is nothing for a manual run to act on",
			entry.ID,
		)
	}
	key := fmt.Sprintf("automation:manual:%s:%d", entry.ID, time.Now().UTC().UnixNano())
	return s.createTask(ctx, *entry, evt, key)
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
		// Goal automations are driven exclusively by the goal reconcile loop
		// (see automationMatchesEvent for the event-path guard). Without this
		// a Goal!=nil entry carrying a cron trigger would double-dispatch:
		// once through the reconcile engine and once through this generic
		// task-generation path.
		if isGoalAutomation(automation) {
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
		// Evaluate the cron schedule in the automation's configured timezone.
		// Empty or invalid timezone falls back to UTC (see pkg/cron.LoadTimezone).
		loc := cron.LoadTimezone(automation.Trigger.Timezone)
		if !schedule.Matches(now.In(loc)) {
			continue
		}

		generatedKey := fmt.Sprintf("automation:cron:%s:%s", automation.ID, now.UTC().Format("200601021504"))
		if _, err := s.createTask(ctx, automation, types.Event{ProjectID: automation.ProjectID}, generatedKey); err != nil {
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

		// An `update` action is applied HERE, in process. Every other
		// action type ends in "queue a task for a runner to pick up",
		// which is right for work that needs a shell or a model — and
		// wrong for a bookkeeping write, which would then need a runner
		// online, an executor enabled, and a round trip through the
		// queue to set a status the API could set itself.
		if types.NormalizeAutomationActionType(automation.Action.Type) ==
			types.AutomationActionUpdate {
			if err := s.applyUpdateAction(ctx, automation, evt); err != nil {
				return err
			}
			continue
		}

		if _, err := s.createTask(ctx, automation, evt, ""); err != nil {
			return err
		}
	}

	return nil
}

func (s *AutomationService) isAutomationPaused(automation types.BrainEntry, evt types.Event) bool {
	if s == nil || s.pauseChecker == nil {
		return false
	}
	// Prefer per-project pause state: users setting "autos: off" on a single
	// project must not have that decision overridden by the global check.
	projectID := automation.ProjectID
	if projectID == "" {
		projectID = evt.ProjectID
	}
	if projectID != "" {
		if scoped, ok := s.pauseChecker.(automationProjectPauseChecker); ok {
			// scoped checker already folds global state into its per-project
			// answer (see RunnerServiceImpl.IsAutomationsPausedForProject),
			// so its result is final — no further global check needed.
			return scoped.IsAutomationsPausedForProject(projectID)
		}
	}
	return s.pauseChecker.IsAutomationsPaused()
}

func automationMatchesEvent(automation types.BrainEntry, evt types.Event) bool {
	if automation.Trigger == nil || automation.Action == nil {
		return false
	}
	// Goal automations are driven by the goal reconcile loop, not by generic
	// task generation. They are stored as automation entries with an event
	// trigger, so without this they matched here too and every goal trigger
	// produced TWO tasks: the reconcile engine's (correctly keyed
	// "goal:<id>:<state>" and deduped) and a bare duplicate from this path
	// with no generated_key at all — so nothing ever deduped it and a
	// long-lived goal accumulated redundant work on every task completion.
	if isGoalAutomation(automation) {
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

// isGoalAutomation reports whether an entry is a goal rather than an ordinary
// automation. Either marker is enough: GeneratedBy is what the goal builder
// stamps, and a non-nil Goal config identifies hand-written or migrated
// entries that predate it.
func isGoalAutomation(entry types.BrainEntry) bool {
	return entry.GeneratedBy == types.GoalGeneratedBy || entry.Goal != nil
}

func automationMatchesNamedEvent(automation types.BrainEntry, evt types.Event) bool {
	if !globalAutomationMatchesProjectEvent(automation, evt) {
		return false
	}
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
	if !globalAutomationMatchesProjectEvent(automation, evt) {
		return false
	}
	if evt.Type != types.EventWebhookReceived {
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
	if !globalAutomationMatchesProjectEvent(automation, evt) {
		return false
	}
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

func globalAutomationMatchesProjectEvent(automation types.BrainEntry, evt types.Event) bool {
	if automation.ProjectID != "" || evt.ProjectID == "" {
		return true
	}
	if automation.Trigger == nil {
		return false
	}
	return automation.Trigger.Filter["project"] == "*" || automation.Trigger.Filter["project_id"] == "*"
}

func matchAutomationFilters(filters map[string]string, evt types.Event) bool {
	for key, expr := range filters {
		actual := getEventField(evt, key)
		if key == "project" {
			actual = evt.ProjectID
		}
		// checkout_mode default, with one deliberate exception.
		//
		// CheckFeatureCompletion is the authority on feature completion: it
		// reads every task in the feature, folds their checkout_mode, and
		// always stamps the result. An API-sourced event missing the field
		// is therefore a pre-fold legacy event, and defaulting it to "ai"
		// keeps those working after an upgrade.
		//
		// A RUNNER-sourced feature.completed is a different animal. The
		// runner's feature tracker emits its own completion signal from a
		// local view of the tasks it happens to be executing, and never
		// folds anything. Treating that absence as "ai" meant every feature
		// completion fired the AI checkout *in addition to* the correctly
		// folded one — two checkout agents racing to merge the same
		// feature, and a project configured for deterministic merges
		// silently getting an LLM one anyway.
		if key == "checkout_mode" && actual == "" {
			if evt.Source == types.EventSourceRunner {
				return false
			}
			actual = "ai"
		}
		if !types.MatchFilterValue(actual, expr) {
			return false
		}
	}
	return true
}

func (s *AutomationService) createTask(ctx context.Context, automation types.BrainEntry, evt types.Event, generatedKeyOverride string) (string, error) {
	project := automation.ProjectID
	if project == "" {
		project = evt.ProjectID
	}
	if skip, reason, err := s.shouldSkipTaskGeneration(ctx, project, automation); err != nil {
		return "", err
	} else if skip {
		_, err := s.createRunAudit(ctx, automationRunAudit{
			automation: automation,
			evt:        evt,
			project:    project,
			status:     "skipped",
			skipReason: reason,
		})
		if err != nil {
			return "", err
		}
		return "", nil
	}

	generated := true
	generatedKey := automationGeneratedKey(automation, evt)
	if generatedKeyOverride != "" {
		generatedKey = generatedKeyOverride
	}
	if generatedKey != "" {
		exists, err := s.generatedTaskExists(ctx, project, generatedKey)
		if err != nil {
			return "", err
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
				return "", err
			}
			return "", nil
		}
	}

	prompt := renderAutomationTemplate(automation.Action.DirectPrompt, project, evt)
	agent := firstNonEmpty(automation.Agent, automation.Action.Agent)
	model := firstNonEmpty(automation.Model, automation.Action.Model)
	executor := firstNonEmpty(automation.Executor, automation.Action.Executor)
	executionMode := firstNonEmpty(automation.ExecutionMode, automation.Action.ExecutionMode)
	targetWorkdir := firstNonEmpty(automation.TargetWorkdir, automation.Action.TargetWorkdir)

	// A global automation has one workdir for every project it serves, which
	// cannot be right for more than one of them. The built-in feature
	// checkout is exactly this shape: registered once, then expected to run
	// git in whichever repo the completed feature was built in. Fall back to
	// the feature's own tasks, which do know their repo.
	//
	// Without this the generated checkout task inherited nothing, defaulted
	// to /tmp, and died on "not a git repository" — the feature was built
	// but never merged.
	if targetWorkdir == "" && evt.FeatureID != "" {
		targetWorkdir = s.workdirFromFeatureTasks(ctx, project, evt.FeatureID)
	}
	req := types.CreateEntryRequest{
		Type:           "task",
		Title:          fmt.Sprintf("Automation: %s", automation.ID),
		Content:        prompt,
		Status:         "pending",
		Project:        project,
		Generated:      &generated,
		GeneratedBy:    fmt.Sprintf("automation:%s", automation.ID),
		GeneratedKey:   generatedKey,
		DirectPrompt:   prompt,
		Agent:          agent,
		Model:          model,
		Executor:       executor,
		ExecutionMode:  executionMode,
		SessionMode:    automation.Action.SessionMode,
		CompleteOnIdle: automationCompleteOnIdle(automation.Action.CompleteOnIdle),
		TargetWorkdir:  targetWorkdir,

		// Origin provenance is deliberately NOT set. A generated task has no
		// human caller and no origin machine — stamping this process's
		// identity would be stamping the API host, pinning server-generated
		// work to the API box. Left empty, machine_affinity resolves to
		// "none" and placement behaves exactly as it did before origin
		// tracking existed.

		// Git / merge settings flow from the automation entry onto the task
		// it generates. Without this the built-in feature-checkout task knew
		// its merge target only as prose inside the prompt — the structured
		// fields the executor actually reads when merging were empty, so an
		// automated checkout could not land the branch it was created to
		// land. Anything left unset on the automation stays unset here and
		// falls back to task_defaults downstream, as before.
		Workdir:            automation.Workdir,
		GitRemote:          automation.GitRemote,
		MergeTargetBranch:  automation.MergeTargetBranch,
		MergePolicy:        automation.MergePolicy,
		MergeStrategy:      automation.MergeStrategy,
		RemoteBranchPolicy: automation.RemoteBranchPolicy,
		OpenPRBeforeMerge:  automation.OpenPRBeforeMerge,
		CheckoutMode:       automation.CheckoutMode,
	}

	if types.NormalizeAutomationActionType(automation.Action.Type) == types.AutomationActionScript {
		command := renderAutomationTemplate(automation.Action.Command, project, evt)
		req.Executor = "script"
		req.Content = command
		req.DirectPrompt = command
		if req.ExecutionMode == "" {
			req.ExecutionMode = "current_branch"
		}
		if req.TargetWorkdir == "" {
			req.TargetWorkdir = "/tmp"
		}
	}

	if evt.FeatureID != "" {
		req.FeatureID = evt.FeatureID
	}

	taskResp, err := s.brain.Save(ctx, req)
	if err != nil {
		return "", fmt.Errorf("create automation task: %w", err)
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
		return "", err
	}
	if runID != "" {
		_, err = s.brain.Update(ctx, taskResp.ID, types.UpdateEntryRequest{AutomationRunID: &runID})
		if err != nil {
			return "", fmt.Errorf("link automation task to run audit: %w", err)
		}
	}
	return taskResp.ID, nil
}

// workdirFromFeatureTasks returns the repo a feature's work happened in, by
// reading the tasks that make up the feature.
//
// Prefers target_workdir (the repo root the runner was pointed at) over
// workdir (which may be a worktree path specific to one task). Returns ""
// when nothing is set, leaving the caller's existing defaults in play.
//
// Generated tasks are skipped: a previous automation's task carries the
// fallback we are trying to compute, so including them would let a bad
// value (e.g. /tmp) propagate to every later checkout.
func (s *AutomationService) workdirFromFeatureTasks(ctx context.Context, project, featureID string) string {
	if s == nil || s.brain == nil || project == "" || featureID == "" {
		return ""
	}
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:      "task",
		Project:   project,
		FeatureID: featureID,
		Limit:     100,
	})
	if err != nil || resp == nil {
		return ""
	}
	var workdirFallback string
	for _, t := range resp.Entries {
		if t.Generated != nil && *t.Generated {
			continue
		}
		if t.TargetWorkdir != "" {
			return t.TargetWorkdir
		}
		if workdirFallback == "" && t.Workdir != "" {
			workdirFallback = t.Workdir
		}
	}
	return workdirFallback
}

func automationCompleteOnIdle(value *bool) *bool {
	if value != nil {
		return value
	}
	defaultValue := true
	return &defaultValue
}

func renderAutomationTemplate(input, project string, evt types.Event) string {
	if input == "" {
		return ""
	}
	tmpl, err := template.New("automation-project").Option("missingkey=error").Parse(input)
	if err != nil {
		return input
	}
	// EventProjectID surfaces the source project of the triggering event,
	// which can differ from Project/ProjectID for cross-project automations
	// (those using filter.project: "*"). Project/ProjectID always reflect
	// the project that owns the automation entry and the generated task.
	data := struct {
		Project        string
		ProjectID      string
		EventProjectID string
		FeatureID      string
		TaskID         string
		TaskPath       string
		TaskTitle      string
		FromStatus     string
		ToStatus       string
	}{
		Project:        project,
		ProjectID:      project,
		EventProjectID: evt.ProjectID,
		FeatureID:      evt.FeatureID,
		TaskID:         evt.TaskID,
		TaskPath:       evt.TaskPath,
		TaskTitle:      evt.TaskTitle,
		FromStatus:     evt.FromStatus,
		ToStatus:       evt.ToStatus,
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return input
	}
	return buf.String()
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
	fmt.Fprintf(&content, "automation_id: %s\n", audit.automation.ID)
	fmt.Fprintf(&content, "automation_path: %s\n", audit.automation.Path)
	fmt.Fprintf(&content, "project: %s\n", audit.project)
	fmt.Fprintf(&content, "trigger_type: %s\n", triggerType)
	if triggerEvent != "" {
		fmt.Fprintf(&content, "trigger_event: %s\n", triggerEvent)
	}
	if audit.evt.ID != "" {
		fmt.Fprintf(&content, "source_event_id: %s\n", audit.evt.ID)
	}
	if audit.generatedKey != "" {
		fmt.Fprintf(&content, "dedup_key: %s\n", audit.generatedKey)
	}
	fmt.Fprintf(&content, "started_at: %s\n", started.Format(time.RFC3339))
	fmt.Fprintf(&content, "completed_at: %s\n", started.Format(time.RFC3339))
	content.WriteString("duration_ms: 0\n")
	if audit.skipReason != "" {
		fmt.Fprintf(&content, "skip_reason: %s\n", audit.skipReason)
	}
	if audit.errorText != "" {
		fmt.Fprintf(&content, "error: %s\n", audit.errorText)
	}
	content.WriteString("\n### Trigger Payload Summary\n")
	content.WriteString(summarizeAutomationEvent(audit.evt))
	content.WriteString("\n### Generated Tasks\n")
	if len(audit.taskIDs) == 0 {
		content.WriteString("- none\n")
	} else {
		for _, id := range audit.taskIDs {
			fmt.Fprintf(&content, "- %s\n", id)
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

// bulkUpdateActionLimit mirrors the server-side bulk cap. Explicit-mode
// bulk updates are truncated at 100 without reporting it, so an update
// action asks for no more than it can be sure was applied.
const bulkUpdateActionLimit = 100

// applyUpdateAction runs an `update` automation in process.
//
// The only action type that does not end in a queued task. It exists for
// bookkeeping writes — archiving a feature the moment its work is done —
// where routing through the task queue would mean the change needs an
// online runner, an enabled executor and a round trip, to set a status the
// API server is already holding the transaction for.
//
// ─── Scope is taken from the EVENT, never from the automation ────
//
// The target is always {project, feature_id, type: task}, with both ids
// read off the triggering event. An automation cannot widen that, because
// nothing in it is consulted: there is no filter field to get wrong.
//
// Both ids must be non-empty and the write is refused otherwise. That is
// not defensive noise — `storage.ListOptions` appends its WHERE clause
// only for a non-empty value, so an empty feature id is not "no matching
// tasks", it is NO CONSTRAINT: the bulk update would rewrite the first 100
// tasks of the project, or of the brain. Every gate upstream would report
// the filter as constrained while it matched everything.
//
// A skip is audited like any other, so an automation that never fires has
// a reason on the record rather than a silence.
func (s *AutomationService) applyUpdateAction(
	ctx context.Context,
	automation types.BrainEntry,
	evt types.Event,
) error {
	project := automation.ProjectID
	if project == "" {
		project = evt.ProjectID
	}

	skip := func(reason string) error {
		_, err := s.createRunAudit(ctx, automationRunAudit{
			automation: automation,
			evt:        evt,
			project:    project,
			status:     "skipped",
			skipReason: reason,
		})
		return err
	}

	status := strings.TrimSpace(automation.Action.SetStatus)
	if status == "" {
		return skip("update action has no set_status")
	}
	if !types.IsValidEntryStatus(status) {
		return skip(fmt.Sprintf("update action has an unknown status %q", status))
	}
	if project == "" {
		return skip("update action has no project to scope to")
	}
	if evt.FeatureID == "" {
		return skip("update action fired on an event with no feature")
	}

	// ─── Why this lists first instead of writing a filter ────────
	//
	// The dedup that protects every other action type lives in
	// `createTask` (once_per → generated_key → generatedTaskExists), and
	// this path deliberately does not go through it. Without a stand-in,
	// a filter-mode write would be a LOOP: it sets every task in the
	// feature to `archived`, each write emits task.status_changed,
	// CheckFeatureCompletion counts archived as done and re-emits
	// feature.completed, and this automation fires again — forever,
	// because re-archiving an archived task is still a write.
	//
	// Listing first and writing only the tasks that are NOT already at the
	// target status breaks it at the source rather than papering over it
	// with a dedup key: the second pass finds nothing to change, writes
	// nothing, and therefore emits nothing. It also keeps the audit honest
	// — "3 updated" means three tasks moved.
	taskType := "task"
	listed, err := s.brain.List(ctx, types.ListEntriesRequest{
		Project:   project,
		Type:      taskType,
		FeatureID: evt.FeatureID,
		Limit:     bulkUpdateActionLimit,
	})
	if err != nil {
		return err
	}
	entries := make([]types.BulkUpdateEntry, 0, len(listed.Entries))
	for _, e := range listed.Entries {
		if e.Status == status {
			continue
		}
		entries = append(entries, types.BulkUpdateEntry{
			Path:    e.Path,
			Updates: types.UpdateEntryRequest{Status: &status},
		})
	}
	if len(entries) == 0 {
		return skip(fmt.Sprintf("every task is already %s", status))
	}

	resp, err := s.brain.BulkUpdate(ctx, types.BulkUpdateRequest{
		Entries: entries,
	})
	if err != nil {
		_, auditErr := s.createRunAudit(ctx, automationRunAudit{
			automation: automation,
			evt:        evt,
			project:    project,
			status:     "failed",
			skipReason: err.Error(),
		})
		if auditErr != nil {
			return auditErr
		}
		return err
	}

	// The bulk endpoint caps at 100 per call, and in explicit mode it does
	// so SILENTLY — no `truncated` flag. A feature with more tasks than
	// that is drained by the next firing, which is exactly why the loop
	// guard above is "skip when nothing needs changing" rather than a
	// fire-once key: a fire-once key would strand the remainder forever.
	note := fmt.Sprintf("updated %d/%d to %s", resp.Updated, resp.Total, status)
	if len(entries) == bulkUpdateActionLimit {
		note += fmt.Sprintf(" (capped at %d — the next firing takes the rest)",
			bulkUpdateActionLimit)
	}
	_, err = s.createRunAudit(ctx, automationRunAudit{
		automation: automation,
		evt:        evt,
		project:    project,
		status:     "success",
		skipReason: note,
	})
	return err
}
