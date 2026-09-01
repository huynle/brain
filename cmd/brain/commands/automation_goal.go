package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/markdown"
)

// =============================================================================
// Automation Goal Command
// =============================================================================

// GoalFlags holds flags for the `brain automation goal` subcommands.
type GoalFlags struct {
	Project string // --project
	Feature string // --feature
	// FeatureSet records that --feature was passed at all, so `edit` can tell
	// "leave the scope alone" from "--feature \"\"", which clears it.
	FeatureSet    bool
	Title         string   // --title
	Content       string   // --content
	TriggerSource string   // --trigger-source (task|feature|both)
	SessionMode   string   // --session-mode (continue|fresh)
	Agent         string   // --agent
	Model         string   // --model
	Executor      string   // --executor
	Workdir       string   // --workdir
	Status        string   // --status
	Criteria      []string // --criteria (repeatable)
	Validate      []string // --validate (repeatable)
	Format        string   // --format (json|table)
	Limit         int      // --limit
	Quiet         bool     // -q, --quiet
}

// AutomationGoalCommand implements the Command interface for goal automation
// management. A goal is an automation entry whose trigger/action drive a
// deterministic in-process reconcile loop.
type AutomationGoalCommand struct {
	Subcommand string // set, list, show, edit, run, reconcile, pause, resume, archive, clear, validate
	Project    string // first positional (project)
	GoalID     string // second positional (goalId); for `set` this holds the goal objective text
	Config     *UnifiedConfig
	Flags      *GoalFlags
	Out        io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *AutomationGoalCommand) Type() string {
	return "automation goal"
}

// Execute runs the automation goal command.
func (c *AutomationGoalCommand) Execute() error {
	out := c.out()

	switch c.Subcommand {
	case "set":
		return c.executeSet(out)
	case "list", "":
		return c.executeList(out)
	case "show":
		return c.executeShow(out)
	case "edit":
		return c.executeEdit(out)
	case "run", "reconcile":
		return c.executeRun(out)
	case "pause":
		return c.executeStatus(out, "blocked", "paused")
	case "resume":
		return c.executeStatus(out, "active", "resumed")
	case "archive":
		return c.executeStatus(out, "archived", "archived")
	case "clear":
		return c.executeStatus(out, "archived", "cleared")
	case "validate":
		return c.executeValidate(out)
	default:
		return fmt.Errorf("unknown automation goal subcommand: %q\nRun 'brain help automation goal' for usage", c.Subcommand)
	}
}

// out returns the output writer, defaulting to os.Stdout.
func (c *AutomationGoalCommand) out() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stdout
}

// getAPIClient returns the injected client or creates one from config.
func (c *AutomationGoalCommand) getAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	c.apiClient = runner.NewAPIClient(c.Config.Runner)
	return c.apiClient
}

// =============================================================================
// Subcommand: set (create a goal)
// =============================================================================

func (c *AutomationGoalCommand) executeSet(out io.Writer) error {
	// For `set`, GoalID holds the goal objective text.
	objective := strings.TrimSpace(c.GoalID)
	if objective == "" {
		return fmt.Errorf("usage: brain automation goal set <project> \"<goal objective>\" [flags]")
	}
	if c.Project == "" {
		return fmt.Errorf("usage: brain automation goal set <project> \"<goal objective>\" [flags]")
	}

	// Generate a stable goal ID: slug from the objective + short ID suffix.
	goalID := slugify(objective) + "-" + markdown.GenerateShortID()

	title := objective
	if c.Flags.Title != "" {
		title = c.Flags.Title
	}

	req := types.CreateGoalRequest{
		Project:   c.Project,
		FeatureID: c.Flags.Feature,
		Title:     title,
		Content:   c.Flags.Content,
		Config: types.GoalConfig{
			ID:            goalID,
			Criteria:      strings.Join(c.Flags.Criteria, "\n"),
			Validation:    strings.Join(c.Flags.Validate, "\n"),
			Workdir:       c.Flags.Workdir,
			TriggerSource: c.Flags.TriggerSource,
		},
		Action: types.AutomationAction{
			Type:        "prompt",
			Agent:       c.Flags.Agent,
			Model:       c.Flags.Model,
			SessionMode: c.Flags.SessionMode,
		},
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	goal, err := client.CreateGoal(ctx, req)
	if err != nil {
		return fmt.Errorf("create goal: %w", err)
	}

	if c.Flags.Format == "json" {
		return writeJSON(out, goal)
	}

	fmt.Fprintf(out, "Goal created: %s (entry %s)\n", goal.GoalID, goal.EntryID)
	if goal.Title != "" {
		fmt.Fprintf(out, "  Title:   %s\n", goal.Title)
	}
	if goal.Project != "" {
		fmt.Fprintf(out, "  Project: %s\n", goal.Project)
	}
	return nil
}

// =============================================================================
// Subcommand: list
// =============================================================================

func (c *AutomationGoalCommand) executeList(out io.Writer) error {
	project := c.Project
	if project == "" {
		project = c.Flags.Project
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	goals, err := client.ListGoals(ctx, project, c.Flags.Feature)
	if err != nil {
		return fmt.Errorf("list goals: %w", err)
	}

	if c.Flags.Format == "json" {
		return writeJSON(out, goals)
	}

	if len(goals) == 0 {
		fmt.Fprintln(out, "No goals found")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Create one with:")
		fmt.Fprintln(out, "  brain automation goal set <project> \"<goal objective>\"")
		return nil
	}

	fmt.Fprintln(out, "Goals")
	fmt.Fprintln(out, strings.Repeat("─", 110))
	fmt.Fprintf(out, "%-22s %-28s %-15s %-15s %-10s %s\n",
		"ID", "Title", "Project", "Feature", "Status", "Trigger")
	fmt.Fprintln(out, strings.Repeat("─", 110))

	for _, g := range goals {
		trigger := ""
		if g.Config != nil {
			trigger = g.Config.NormalizedTriggerSource()
		}
		project := g.Project
		if project == "" {
			project = "(global)"
		}
		feature := g.FeatureID
		if feature == "" {
			feature = "-"
		}
		fmt.Fprintf(out, "%-22s %-28s %-15s %-15s %-10s %s\n",
			truncate(g.GoalID, 22),
			truncate(g.Title, 28),
			truncate(project, 15),
			truncate(feature, 15),
			g.Status,
			trigger,
		)
	}

	fmt.Fprintln(out, strings.Repeat("─", 110))
	if !c.Flags.Quiet {
		fmt.Fprintf(out, "Total: %d goal(s)\n", len(goals))
	}
	return nil
}

// =============================================================================
// Subcommand: show
// =============================================================================

func (c *AutomationGoalCommand) executeShow(out io.Writer) error {
	if c.GoalID == "" {
		return fmt.Errorf("usage: brain automation goal show <project> <goalId>")
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	goal, err := c.findGoal(ctx, client)
	if err != nil {
		return err
	}

	// Best-effort progress fetch; non-fatal if it fails.
	progress, progErr := client.GoalProgress(ctx, c.GoalID)

	if c.Flags.Format == "json" {
		payload := struct {
			Goal     *types.GoalSummary          `json:"goal"`
			Progress *types.GoalProgressResponse `json:"progress,omitempty"`
		}{Goal: goal, Progress: progress}
		return writeJSON(out, payload)
	}

	fmt.Fprintf(out, "Goal: %s\n", goal.GoalID)
	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintf(out, "  Entry:   %s\n", goal.EntryID)
	fmt.Fprintf(out, "  Title:   %s\n", goal.Title)
	if goal.Project != "" {
		fmt.Fprintf(out, "  Project: %s\n", goal.Project)
	}
	if goal.FeatureID != "" {
		fmt.Fprintf(out, "  Feature: %s\n", goal.FeatureID)
	}
	fmt.Fprintf(out, "  Status:  %s\n", goal.Status)
	if goal.Config != nil {
		fmt.Fprintf(out, "  Trigger: %s\n", goal.Config.NormalizedTriggerSource())
		if goal.Config.Criteria != "" {
			fmt.Fprintf(out, "  Criteria:\n    %s\n", strings.ReplaceAll(goal.Config.Criteria, "\n", "\n    "))
		}
		if goal.Config.Validation != "" {
			fmt.Fprintf(out, "  Validation:\n    %s\n", strings.ReplaceAll(goal.Config.Validation, "\n", "\n    "))
		}
		if goal.Config.Workdir != "" {
			fmt.Fprintf(out, "  Workdir: %s\n", goal.Config.Workdir)
		}
	}
	if goal.Action != nil {
		if goal.Action.Agent != "" {
			fmt.Fprintf(out, "  Agent:   %s\n", goal.Action.Agent)
		}
		if goal.Action.Model != "" {
			fmt.Fprintf(out, "  Model:   %s\n", goal.Action.Model)
		}
	}

	fmt.Fprintln(out)
	if progErr != nil {
		fmt.Fprintf(out, "Progress: (unavailable: %v)\n", progErr)
		return nil
	}
	writeProgress(out, progress)
	return nil
}

// =============================================================================
// Subcommand: edit
// =============================================================================

func (c *AutomationGoalCommand) executeEdit(out io.Writer) error {
	if c.GoalID == "" {
		return fmt.Errorf("usage: brain automation goal edit <project> <goalId> [flags]")
	}

	req := types.UpdateGoalRequest{}
	if c.Flags.Title != "" {
		req.Title = strPtr(c.Flags.Title)
	}
	if c.Flags.Content != "" {
		req.Content = strPtr(c.Flags.Content)
	}
	if c.Flags.Status != "" {
		req.Status = strPtr(c.Flags.Status)
	}
	if c.Flags.FeatureSet {
		req.FeatureID = strPtr(strings.TrimSpace(c.Flags.Feature))
	}
	if len(c.Flags.Criteria) > 0 {
		req.Criteria = strPtr(strings.Join(c.Flags.Criteria, "\n"))
	}
	if len(c.Flags.Validate) > 0 {
		req.Validation = strPtr(strings.Join(c.Flags.Validate, "\n"))
	}
	if c.Flags.Workdir != "" {
		req.Workdir = strPtr(c.Flags.Workdir)
	}
	if c.Flags.TriggerSource != "" {
		req.TriggerSource = strPtr(c.Flags.TriggerSource)
	}
	if c.Flags.Agent != "" || c.Flags.Model != "" || c.Flags.SessionMode != "" {
		req.Action = &types.AutomationAction{
			Type:        "prompt",
			Agent:       c.Flags.Agent,
			Model:       c.Flags.Model,
			SessionMode: c.Flags.SessionMode,
		}
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	goal, err := client.UpdateGoal(ctx, c.GoalID, req)
	if err != nil {
		return fmt.Errorf("update goal: %w", err)
	}

	if c.Flags.Format == "json" {
		return writeJSON(out, goal)
	}

	fmt.Fprintf(out, "Goal updated: %s\n", goal.GoalID)
	if goal.Title != "" {
		fmt.Fprintf(out, "  Title:  %s\n", goal.Title)
	}
	fmt.Fprintf(out, "  Status: %s\n", goal.Status)
	return nil
}

// =============================================================================
// Subcommand: run / reconcile
// =============================================================================

func (c *AutomationGoalCommand) executeRun(out io.Writer) error {
	if c.GoalID == "" {
		return fmt.Errorf("usage: brain automation goal run <project> <goalId>")
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	audit, err := client.RunGoal(ctx, c.GoalID)
	if err != nil {
		return fmt.Errorf("run goal: %w", err)
	}

	if c.Flags.Format == "json" {
		return writeJSON(out, audit)
	}

	fmt.Fprintf(out, "Reconcile: %s\n", c.GoalID)
	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintf(out, "  Decision: %s\n", string(audit.Decision))
	fmt.Fprintf(out, "  Reason:   %s\n", audit.Reason)
	if audit.GeneratedTaskID != "" {
		fmt.Fprintf(out, "  Task:     %s\n", audit.GeneratedTaskID)
	}
	if len(audit.LinkedTasks) > 0 {
		fmt.Fprintf(out, "  Linked tasks: %d\n", len(audit.LinkedTasks))
	}
	return nil
}

// =============================================================================
// Subcommand: pause / resume / archive / clear (status changes)
// =============================================================================

func (c *AutomationGoalCommand) executeStatus(out io.Writer, status, verb string) error {
	if c.GoalID == "" {
		return fmt.Errorf("usage: brain automation goal %s <project> <goalId>", c.Subcommand)
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	goal, err := client.UpdateGoal(ctx, c.GoalID, types.UpdateGoalRequest{Status: strPtr(status)})
	if err != nil {
		return fmt.Errorf("%s goal: %w", verb, err)
	}

	if c.Flags.Format == "json" {
		return writeJSON(out, goal)
	}

	fmt.Fprintf(out, "Goal %s %s (status: %s)\n", c.GoalID, verb, status)
	return nil
}

// =============================================================================
// Subcommand: validate (read-only progress/health check)
// =============================================================================

func (c *AutomationGoalCommand) executeValidate(out io.Writer) error {
	if c.GoalID == "" {
		return fmt.Errorf("usage: brain automation goal validate <project> <goalId>")
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	progress, err := client.GoalProgress(ctx, c.GoalID)
	if err != nil {
		return fmt.Errorf("validate goal: %w", err)
	}

	if c.Flags.Format == "json" {
		return writeJSON(out, progress)
	}

	fmt.Fprintf(out, "Validation: %s\n", c.GoalID)
	fmt.Fprintln(out, strings.Repeat("─", 60))
	writeProgress(out, progress)

	met := progress.Total > 0 && progress.Completed == progress.Total && progress.Blocked == 0
	fmt.Fprintln(out)
	if met {
		fmt.Fprintln(out, "Validation: criteria appear MET (all linked tasks complete)")
	} else if progress.Total == 0 {
		fmt.Fprintln(out, "Validation: criteria NOT met (no linked tasks yet)")
	} else if progress.Blocked > 0 {
		fmt.Fprintf(out, "Validation: criteria NOT met (%d blocked task(s))\n", progress.Blocked)
	} else {
		fmt.Fprintf(out, "Validation: criteria NOT met (%d/%d complete)\n", progress.Completed, progress.Total)
	}
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// findGoal lists goals and returns the one whose GoalID matches c.GoalID.
func (c *AutomationGoalCommand) findGoal(ctx context.Context, client *runner.APIClient) (*types.GoalSummary, error) {
	project := c.Project
	if project == "" {
		project = c.Flags.Project
	}
	goals, err := client.ListGoals(ctx, project, c.Flags.Feature)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	for i := range goals {
		if goals[i].GoalID == c.GoalID {
			return &goals[i], nil
		}
	}
	return nil, fmt.Errorf("goal not found: %q", c.GoalID)
}

// writeProgress prints a goal's linked-task progress summary.
func writeProgress(out io.Writer, p *types.GoalProgressResponse) {
	if p == nil {
		return
	}
	fmt.Fprintln(out, "Progress")
	fmt.Fprintf(out, "  Total:       %d\n", p.Total)
	fmt.Fprintf(out, "  Pending:     %d\n", p.Pending)
	fmt.Fprintf(out, "  In Progress: %d\n", p.InProgress)
	fmt.Fprintf(out, "  Completed:   %d\n", p.Completed)
	fmt.Fprintf(out, "  Blocked:     %d\n", p.Blocked)
	if p.GoalStatus != "" {
		fmt.Fprintf(out, "  Goal:        %s\n", p.GoalStatus)
	}
	// Only set for a feature-scoped goal; see types.GoalProgressResponse.
	if p.FeatureStatus != "" {
		fmt.Fprintf(out, "  Feature:     %s\n", p.FeatureStatus)
	}
}

// writeJSON marshals v as indented JSON to out.
func writeJSON(out io.Writer, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	fmt.Fprintln(out, string(data))
	return nil
}

// strPtr returns a pointer to the given string.
func strPtr(s string) *string { return &s }

// slugify converts arbitrary text to a lowercase, dash-separated slug suitable
// for use as a goal ID prefix.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "goal"
	}
	// Cap slug length to keep IDs readable.
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	return slug
}
