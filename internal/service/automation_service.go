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
	projects     automationProjectLister
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

// automationProjectLister enumerates the projects a wildcard automation fans
// out over. Optional and injected the same way the pause checker is, so the
// automation service keeps working (single unscoped run, as before) in tests
// and callers that never wire it.
type automationProjectLister interface {
	ListProjects(ctx context.Context) ([]string, error)
}

// NewAutomationService creates an automation evaluator backed by brain entries.
func NewAutomationService(brain *BrainServiceImpl) *AutomationService {
	return &AutomationService{brain: brain}
}

// RunAutomationNow manually triggers one automation through the exact task-
// generation path the cron/event dispatchers use (createTask), so a manual
// run can never behave differently from a scheduled one. The dedup key is
// uniquified per invocation, and the pause gate is intentionally NOT applied
// — a manual run is an explicit user override. Returns the created task ids,
// or an empty slice when generation was skipped (e.g. max_concurrent reached;
// the skip is recorded in the run audit).
//
// `project` scopes the run. It matters only for a GLOBAL automation, where it
// is the sole way a caller can say which project the run is for: an automation
// entry that owns a project always wins over it, exactly as createTask
// resolves the pair. The PWA lists global automations inside every project's
// Automations tab, so "Run now" on the built-in Dream Consolidation from the
// hindsight tab has always meant "dream for hindsight" — the project was
// simply dropped on the floor, and the run landed in `default` with an empty
// {{.Project}}. An empty project against an any-project automation fans out
// the same way the cron path does, so the two cannot drift.
func (s *AutomationService) RunAutomationNow(ctx context.Context, pathOrID, project string) ([]string, error) {
	entry, err := s.brain.Recall(ctx, pathOrID)
	if err != nil {
		return nil, err
	}
	if entry.Type != "automation" {
		return nil, fmt.Errorf("entry %s is not an automation (type %q)", pathOrID, entry.Type)
	}
	if entry.Action == nil {
		return nil, fmt.Errorf("automation %s has no action", entry.ID)
	}
	if types.NormalizeAutomationActionType(entry.Action.Type) ==
		types.AutomationActionUpdate {
		// A manual run of an update automation has no event to scope it,
		// and an unscoped bulk write is the one thing this action must
		// never do. Refuse rather than guess a target.
		return nil, fmt.Errorf(
			"automation %s has an update action: it applies to the feature its "+
				"trigger names, so there is nothing for a manual run to act on",
			entry.ID,
		)
	}

	var projects []string
	switch {
	case entry.ProjectID != "":
		// The entry owns a project; a caller-supplied one cannot override it.
		projects = []string{entry.ProjectID}
	case project != "":
		projects = []string{project}
	default:
		projects, err = s.scheduledTargetProjects(ctx, *entry)
		if err != nil {
			return nil, err
		}
	}

	taskIDs := make([]string, 0, len(projects))
	for _, proj := range projects {
		evt := types.Event{
			Type:      "manual",
			Source:    "api",
			Timestamp: time.Now().UTC(),
			ProjectID: proj,
		}
		// A global automation reaches createTask with an empty ProjectID, so
		// the event is what carries the scope — the same hand-off the cron
		// fan-out makes.
		key := fmt.Sprintf("automation:manual:%s:%s:%d", entry.ID, proj, time.Now().UTC().UnixNano())
		taskID, err := s.createTask(ctx, *entry, evt, key)
		if err != nil {
			return nil, err
		}
		if taskID != "" {
			taskIDs = append(taskIDs, taskID)
		}
	}
	return taskIDs, nil
}

// SetPauseChecker lets API runner pause state suppress automation task generation.
func (s *AutomationService) SetPauseChecker(checker automationPauseChecker) {
	if s == nil {
		return
	}
	s.pauseChecker = checker
}

// SetProjectLister supplies the project enumeration a cron automation scoped
// to all projects (filter.project: "*") needs to fan out. Without it such an
// automation keeps its historical single unscoped run.
func (s *AutomationService) SetProjectLister(lister automationProjectLister) {
	if s == nil {
		return
	}
	s.projects = lister
}

// scheduledTargetProjects resolves which projects one cron automation fires
// for on this tick.
//
// `filter.project: "*"` means "any project" — but until now only the EVENT
// path read it that way (see automationMatchesNamedEvent). A cron trigger has
// no event, so on this path the wildcard had no reader at all: a global
// automation carrying it generated exactly ONE task with an empty project,
// which storage files under `default` and which renders {{.Project}} as the
// empty string. The built-in Dream Consolidation entry is precisely that
// shape, which is why no project ever got a dream from it — the generated
// task asked the agent to consolidate "project " and saved the result to the
// wrong project.
//
// The concurrency and cooldown guards survive the fan-out unchanged because
// shouldSkipTaskGeneration lists generated tasks per project: `max_concurrent:
// 1` means one in flight PER PROJECT, not one across all of them, which is
// what a per-project automation wants.
func (s *AutomationService) scheduledTargetProjects(ctx context.Context, automation types.BrainEntry) ([]string, error) {
	// An automation that owns a project is scoped to it, wildcard or not.
	if automation.ProjectID != "" {
		return []string{automation.ProjectID}, nil
	}
	if !automationTargetsAllProjects(automation) {
		// A global automation with no wildcard keeps the single unscoped
		// run it has always had. Widening every global cron automation to
		// every project is not this function's call to make.
		return []string{""}, nil
	}
	if s.projects == nil {
		return nil, fmt.Errorf(
			"automation %s targets all projects but no project lister is wired", automation.ID)
	}
	projects, err := s.projects.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects for automation %s: %w", automation.ID, err)
	}
	return projects, nil
}

// automationTargetsAllProjects reports whether an automation's trigger filter
// carries the any-project wildcard, under either spelling.
func automationTargetsAllProjects(automation types.BrainEntry) bool {
	if automation.Trigger == nil {
		return false
	}
	return automation.Trigger.Filter["project"] == "*" ||
		automation.Trigger.Filter["project_id"] == "*"
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

	var firstErr error
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

		// One cron automation can now fire for many projects. A failure
		// resolving them is remembered, not returned: this loop is the
		// only thing that runs EVERY cron automation, and letting one bad
		// entry abort the sweep starves all the others on every tick.
		projects, err := s.scheduledTargetProjects(ctx, automation)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		for _, project := range projects {
			// The per-project event is what scopes everything downstream:
			// the pause dial consulted, the project the generated task and
			// its audit land in, and the {{.Project}} the prompt renders.
			evt := types.Event{ProjectID: project}

			// The pause gate is checked HERE, after the schedule match,
			// and not before it. A paused automation whose gate ran first
			// wrote a "skipped: paused" run audit on EVERY tick of the
			// one-minute ticker, whether or not the cron was due — 1440
			// audit entries a day per paused cron automation, which buried
			// every real run in the history the PWA renders. Post-match, a
			// skip audit is written only when the automation actually had
			// work to do, which is the only case where "it was paused"
			// tells the reader anything.
			if s.isAutomationPaused(automation, evt) {
				if _, err := s.createRunAudit(ctx, automationRunAudit{
					automation: automation,
					evt:        evt,
					project:    project,
					status:     "skipped",
					skipReason: "paused",
				}); err != nil && firstErr == nil {
					firstErr = err
				}
				continue
			}

			// The project belongs in the dedup key even though
			// generatedTaskExists already scopes its lookup by project:
			// a fan-out generates N tasks for one (automation, minute),
			// and a key that cannot tell them apart is one storage change
			// away from collapsing them into one.
			generatedKey := fmt.Sprintf("automation:cron:%s:%s:%s",
				automation.ID, project, now.UTC().Format("200601021504"))
			if _, err := s.createTask(ctx, automation, evt, generatedKey); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
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
		if !automationTargetsAllProjects(automation) {
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
		if !automationTargetsAllProjects(automation) {
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
		if !automationTargetsAllProjects(automation) {
			return false
		}
	}
	return matchAutomationFilters(automation.Trigger.Filter, evt)
}

func globalAutomationMatchesProjectEvent(automation types.BrainEntry, evt types.Event) bool {
	if automation.ProjectID != "" || evt.ProjectID == "" {
		return true
	}
	return automationTargetsAllProjects(automation)
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
	workdir := automation.Workdir
	gitRemote := automation.GitRemote

	// A global automation has one workdir for every project it serves, which
	// cannot be right for more than one of them. The built-in feature
	// checkout is exactly this shape: registered once, then expected to run
	// git in whichever repo the completed feature was built in. Fall back to
	// the feature's own tasks, which do know their repo.
	//
	// Without this the generated checkout task inherited nothing, defaulted
	// to /tmp, and died on "not a git repository" — the feature was built
	// but never merged. An AI-mode checkout runs in worktree mode, which the
	// runner rejects with "workdir_unavailable" unless it has a local repo
	// path (target_workdir/workdir) or a git_remote to clone, so all three
	// are inherited from the feature when the automation entry omits them.
	if evt.FeatureID != "" && (targetWorkdir == "" || workdir == "" || gitRemote == "") {
		featureGit := s.gitContextFromFeatureTasks(ctx, project, evt.FeatureID)
		if targetWorkdir == "" {
			targetWorkdir = featureGit.TargetWorkdir
		}
		if workdir == "" {
			workdir = featureGit.Workdir
		}
		if gitRemote == "" {
			gitRemote = featureGit.GitRemote
		}
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
		Workdir:            workdir,
		GitRemote:          gitRemote,
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
		// No /tmp default. An empty TargetWorkdir lets the runner's normal
		// chain resolve (task workdir → git remote clone → the runner's
		// configured work dir); pinning /tmp OVERRIDES that chain with a
		// directory that is guaranteed not to be a git repository, so every
		// git-based script — the built-in simple feature checkout above all
		// — died on "not a git repository" instead of running where the
		// runner would otherwise have put it.
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
	return workdirFromFeatureEntries(resp.Entries)
}

// gitContextFromFeatureTasks returns the full repo context a feature's work
// happened in, by reading the tasks that make up the feature. It is the
// context-loading wrapper around gitContextFromFeatureEntries, mirroring
// workdirFromFeatureTasks, so the automation path inherits target_workdir,
// workdir, and git_remote by exactly the same rule the manual checkout
// endpoint uses.
func (s *AutomationService) gitContextFromFeatureTasks(ctx context.Context, project, featureID string) featureGitContext {
	if s == nil || s.brain == nil || project == "" || featureID == "" {
		return featureGitContext{}
	}
	resp, err := s.brain.List(ctx, types.ListEntriesRequest{
		Type:      "task",
		Project:   project,
		FeatureID: featureID,
		Limit:     100,
	})
	if err != nil || resp == nil {
		return featureGitContext{}
	}
	return gitContextFromFeatureEntries(resp.Entries)
}

// workdirFromFeatureEntries is the pure half of workdirFromFeatureTasks, so
// the manual checkout endpoint (which already has the feature's tasks in
// hand, read straight off the filesystem) resolves a workdir by exactly the
// same rule as the automation path.
func workdirFromFeatureEntries(entries []types.BrainEntry) string {
	var workdirFallback string
	for _, t := range entries {
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

// featureGitContext is the repo context a checkout task needs to resolve a
// working directory. In worktree mode the executor requires at least one of
// these to be non-empty; without them it rejects the dispatch with
// "workdir_unavailable" and the checkout task loops as pending forever.
type featureGitContext struct {
	TargetWorkdir string
	Workdir       string
	GitRemote     string
}

// gitContextFromFeatureEntries resolves the repo context a feature's work
// actually happened in, so a generated checkout task can be pointed at the
// same git repo the feature was built in.
//
// TargetWorkdir/Workdir follow the same preference as workdirFromFeatureEntries
// (target_workdir wins, workdir is the fallback). GitRemote is collected
// independently: a worktree-mode checkout can resolve either from a local repo
// path (target_workdir/workdir) OR from git_remote + repo_cache_dir, so
// carrying the remote too lets the runner clone when no local path is valid on
// the machine that ends up executing.
//
// Generated tasks are skipped for the same reason as workdirFromFeatureEntries:
// a prior automation's task carries the fallback we are computing, so including
// it would let a bad value propagate to every later checkout.
func gitContextFromFeatureEntries(entries []types.BrainEntry) featureGitContext {
	var ctx featureGitContext
	for _, t := range entries {
		if t.Generated != nil && *t.Generated {
			continue
		}
		if ctx.TargetWorkdir == "" && t.TargetWorkdir != "" {
			ctx.TargetWorkdir = t.TargetWorkdir
		}
		if ctx.Workdir == "" && t.Workdir != "" {
			ctx.Workdir = t.Workdir
		}
		if ctx.GitRemote == "" && t.GitRemote != "" {
			ctx.GitRemote = t.GitRemote
		}
	}
	return ctx
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
	// summary describes what a SUCCESSFUL run did. Distinct from
	// skipReason because readers treat a non-empty skip reason as proof
	// the run did nothing (see runOutcome in the PWA) — a success note
	// there would misreport real work as a skip.
	summary string
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
	if audit.summary != "" {
		fmt.Fprintf(&content, "summary: %s\n", audit.summary)
	}
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

// bulkUpdateActionMaxPasses bounds the drain loop below. A feature with
// more tasks than one page holds needs several passes; a bug that stopped
// them making progress must not spin forever.
const bulkUpdateActionMaxPasses = 20

// updateActionSourceStatuses are the ONLY statuses an update action will
// rewrite.
//
// Terminal ones, exclusively. "Archive a finished feature" must never mean
// "archive work that has not run yet", and the way it would is not
// hypothetical: `feature.completed` fires ONE pass over the automation
// list, and the built-in feature-checkout automation handles that same
// event by creating a `pending` checkout task stamped with the same
// feature id. Whichever of the two automations the list happens to yield
// first — decided by `modified DESC`, i.e. by which entry was written
// last — decides whether this action then archives the checkout task that
// was created microseconds earlier. An archived task never dispatches, so
// the merge it existed to perform silently never happens, and the
// built-in's `once_per: feature_id` dedup means it never gets a second
// task either.
//
// Restricting the source set removes the race outright rather than
// ordering around it: nothing that has yet to run is eligible.
var updateActionSourceStatuses = []string{"completed", "validated", "cancelled"}

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

	// `skip_reason` is what readers key off to render a run as SKIPPED —
	// the PWA's runOutcome checks `error` first, then `skip_reason`, and
	// never looks at the entry status. So a note only belongs there when
	// the run really was a skip: putting a success summary in it made an
	// archive that moved three tasks render as "skipped: updated 3/3",
	// and putting a failure there hid the failure behind the same glyph.
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
	fail := func(err error) error {
		_, auditErr := s.createRunAudit(ctx, automationRunAudit{
			automation: automation,
			evt:        evt,
			project:    project,
			status:     "failed",
			errorText:  err.Error(),
		})
		if auditErr != nil {
			return auditErr
		}
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

	// ─── Why this pages per SOURCE STATUS ────────────────────────
	//
	// A bare {project, feature, type} list is capped at 100 and ordered
	// `modified DESC` — and writing a task rewrites its file, so the rows
	// this pass just changed sort to the FRONT of the next one. A feature
	// with more tasks than one page would serve the same first hundred
	// forever and never reach the tail.
	//
	// Pinning each pass to a source status makes every write leave that
	// pass's match set, which is the same reasoning the feature-level
	// bulk verbs use. It also gives the loop its own termination: a pass
	// that returns nothing is done.
	taskType := "task"
	var updated, total int
	for _, source := range updateActionSourceStatuses {
		if source == status {
			continue // already there; nothing to move
		}
		for pass := 0; pass < bulkUpdateActionMaxPasses; pass++ {
			listed, err := s.brain.List(ctx, types.ListEntriesRequest{
				Project:   project,
				Type:      taskType,
				FeatureID: evt.FeatureID,
				Status:    source,
				Limit:     bulkUpdateActionLimit,
			})
			if err != nil {
				return fail(err)
			}
			if len(listed.Entries) == 0 {
				break
			}
			entries := make([]types.BulkUpdateEntry, 0, len(listed.Entries))
			for _, e := range listed.Entries {
				entries = append(entries, types.BulkUpdateEntry{
					Path:    e.Path,
					Updates: types.UpdateEntryRequest{Status: &status},
				})
			}
			resp, err := s.brain.BulkUpdate(ctx, types.BulkUpdateRequest{
				Entries: entries,
			})
			if err != nil {
				return fail(err)
			}
			updated += resp.Updated
			total += resp.Total
			// A pass that changed nothing cannot make progress on the
			// next one either — bail rather than spin to the cap.
			if resp.Updated == 0 {
				break
			}
		}
	}

	if total == 0 {
		return skip(fmt.Sprintf("no finished task to move to %s", status))
	}
	// A success carries its summary in the audit BODY (taskIDs are for
	// generated tasks, which this action never makes), leaving both
	// `skip_reason` and `error` empty so every reader classifies it as a
	// real run rather than a skip.
	_, err := s.createRunAudit(ctx, automationRunAudit{
		automation: automation,
		evt:        evt,
		project:    project,
		status:     "success",
		summary:    fmt.Sprintf("updated %d/%d to %s", updated, total, status),
	})
	return err
}
