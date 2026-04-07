package commands

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Automation Command
// =============================================================================

// AutomationFlags holds flags for the automation command.
type AutomationFlags struct {
	Project string // --project
	Format  string // --format (json, short, etc.)
	Limit   int    // --limit
	Quiet   bool   // -q, --quiet
}

// AutomationCommand implements the Command interface for automation management.
type AutomationCommand struct {
	Subcommand string // create, list, test, enable, disable, history
	IDOrName   string // automation ID for enable/disable/history
	Config     *UnifiedConfig
	Flags      *AutomationFlags
	Out        io.Writer
	In         io.Reader // injectable for testing create wizard

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *AutomationCommand) Type() string {
	return "automation"
}

// Execute runs the automation command.
func (c *AutomationCommand) Execute() error {
	out := c.out()

	switch c.Subcommand {
	case "create":
		return c.executeCreate(out)
	case "list", "":
		return c.executeList(out)
	case "test":
		return c.executeTest(out)
	case "enable":
		return c.executeEnable(out)
	case "disable":
		return c.executeDisable(out)
	case "history":
		return c.executeHistory(out)
	default:
		return fmt.Errorf("unknown automation subcommand: %q\nRun 'brain help automation' for usage", c.Subcommand)
	}
}

// out returns the output writer, defaulting to os.Stdout.
func (c *AutomationCommand) out() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stdout
}

// in returns the input reader, defaulting to os.Stdin.
func (c *AutomationCommand) in() io.Reader {
	if c.In != nil {
		return c.In
	}
	return os.Stdin
}

// getAPIClient returns the injected client or creates one from config.
func (c *AutomationCommand) getAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	c.apiClient = runner.NewAPIClient(c.Config.Runner)
	return c.apiClient
}

// =============================================================================
// Subcommand: create (interactive wizard)
// =============================================================================

func (c *AutomationCommand) executeCreate(out io.Writer) error {
	scanner := bufio.NewScanner(c.in())

	fmt.Fprintln(out, "Create Automation")
	fmt.Fprintln(out, strings.Repeat("─", 50))
	fmt.Fprintln(out)

	// Name
	fmt.Fprint(out, "Name: ")
	if !scanner.Scan() {
		return fmt.Errorf("cancelled")
	}
	name := strings.TrimSpace(scanner.Text())
	if name == "" {
		return fmt.Errorf("name is required")
	}

	// Trigger type
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Trigger type:")
	fmt.Fprintln(out, "  1) event    - fires on a brain event")
	fmt.Fprintln(out, "  2) cron     - fires on a schedule")
	fmt.Fprintln(out, "  3) webhook  - fires from external webhook")
	fmt.Fprint(out, "Choose [1-3]: ")
	if !scanner.Scan() {
		return fmt.Errorf("cancelled")
	}
	triggerChoice := strings.TrimSpace(scanner.Text())
	trigger := &types.AutomationTrigger{}
	switch triggerChoice {
	case "1", "event":
		trigger.Type = "event"
		fmt.Fprint(out, "Event name (e.g. task.completed, entry.created): ")
		if !scanner.Scan() {
			return fmt.Errorf("cancelled")
		}
		trigger.Event = strings.TrimSpace(scanner.Text())
		if trigger.Event == "" {
			return fmt.Errorf("event name is required for event triggers")
		}
	case "2", "cron":
		trigger.Type = "cron"
		fmt.Fprint(out, "Cron schedule (e.g. 0 9 * * *): ")
		if !scanner.Scan() {
			return fmt.Errorf("cancelled")
		}
		trigger.Schedule = strings.TrimSpace(scanner.Text())
		if trigger.Schedule == "" {
			return fmt.Errorf("schedule is required for cron triggers")
		}
	case "3", "webhook":
		trigger.Type = "webhook"
		fmt.Fprint(out, "Webhook path (e.g. /hooks/deploy): ")
		if !scanner.Scan() {
			return fmt.Errorf("cancelled")
		}
		trigger.Webhook = strings.TrimSpace(scanner.Text())
		if trigger.Webhook == "" {
			return fmt.Errorf("webhook path is required for webhook triggers")
		}
	default:
		return fmt.Errorf("invalid trigger choice: %q", triggerChoice)
	}

	// Action type
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Action type:")
	fmt.Fprintln(out, "  1) prompt   - create a task with an AI prompt")
	fmt.Fprintln(out, "  2) script   - run a shell command")
	fmt.Fprint(out, "Choose [1-2]: ")
	if !scanner.Scan() {
		return fmt.Errorf("cancelled")
	}
	actionChoice := strings.TrimSpace(scanner.Text())
	action := &types.AutomationAction{}
	switch actionChoice {
	case "1", "prompt":
		action.Type = "prompt"
		fmt.Fprint(out, "Prompt text: ")
		if !scanner.Scan() {
			return fmt.Errorf("cancelled")
		}
		action.DirectPrompt = strings.TrimSpace(scanner.Text())
		if action.DirectPrompt == "" {
			return fmt.Errorf("prompt text is required")
		}
		fmt.Fprint(out, "Agent (optional, press Enter to skip): ")
		if scanner.Scan() {
			if agent := strings.TrimSpace(scanner.Text()); agent != "" {
				action.Agent = agent
			}
		}
	case "2", "script":
		action.Type = "script"
		fmt.Fprint(out, "Shell command: ")
		if !scanner.Scan() {
			return fmt.Errorf("cancelled")
		}
		action.Command = strings.TrimSpace(scanner.Text())
		if action.Command == "" {
			return fmt.Errorf("shell command is required")
		}
	default:
		return fmt.Errorf("invalid action choice: %q", actionChoice)
	}

	// Project
	project := c.Flags.Project
	if project == "" {
		fmt.Fprintln(out)
		fmt.Fprint(out, "Project (optional, press Enter to skip): ")
		if scanner.Scan() {
			if p := strings.TrimSpace(scanner.Text()); p != "" {
				project = p
			}
		}
	}

	// Create via API
	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := types.CreateEntryRequest{
		Type:    "automation",
		Title:   name,
		Status:  "active",
		Tags:    []string{"automation"},
		Trigger: trigger,
		Action:  action,
		Project: project,
	}

	resp, err := client.CreateEntry(ctx, req)
	if err != nil {
		return fmt.Errorf("create automation: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Automation created: %s (%s)\n", resp.Path, resp.ID)
	return nil
}

// =============================================================================
// Subcommand: list
// =============================================================================

func (c *AutomationCommand) executeList(out io.Writer) error {
	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	params := map[string]string{
		"type": "automation",
	}
	if c.Flags.Project != "" {
		params["project"] = c.Flags.Project
	}
	if c.Flags.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", c.Flags.Limit)
	}

	resp, err := client.ListEntries(ctx, params)
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}

	if len(resp.Entries) == 0 {
		fmt.Fprintln(out, "No automations found")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Create one with:")
		fmt.Fprintln(out, "  brain automation create")
		return nil
	}

	// JSON format
	if c.Flags.Format == "json" {
		data, _ := json.MarshalIndent(resp.Entries, "", "  ")
		fmt.Fprintln(out, string(data))
		return nil
	}

	// Table format
	fmt.Fprintln(out, "Automations")
	fmt.Fprintln(out, strings.Repeat("─", 100))
	fmt.Fprintf(out, "%-10s %-25s %-10s %-10s %-15s %s\n",
		"ID", "Name", "Trigger", "Status", "Project", "Details")
	fmt.Fprintln(out, strings.Repeat("─", 100))

	for _, entry := range resp.Entries {
		triggerType := ""
		triggerDetail := ""
		if entry.Trigger != nil {
			triggerType = entry.Trigger.Type
			switch entry.Trigger.Type {
			case "event":
				triggerDetail = entry.Trigger.Event
			case "cron":
				triggerDetail = entry.Trigger.Schedule
			case "webhook":
				triggerDetail = entry.Trigger.Webhook
			}
		}

		project := entry.ProjectID
		if project == "" {
			project = "(global)"
		}

		// Truncate detail if too long
		if len(triggerDetail) > 25 {
			triggerDetail = triggerDetail[:22] + "..."
		}

		fmt.Fprintf(out, "%-10s %-25s %-10s %-10s %-15s %s\n",
			entry.ID,
			truncate(entry.Title, 25),
			triggerType,
			entry.Status,
			truncate(project, 15),
			triggerDetail,
		)
	}

	fmt.Fprintln(out, strings.Repeat("─", 100))
	if !c.Flags.Quiet {
		fmt.Fprintf(out, "Total: %d automation(s)\n", len(resp.Entries))
	}
	return nil
}

// =============================================================================
// Subcommand: test (dry-run event simulation)
// =============================================================================

func (c *AutomationCommand) executeTest(out io.Writer) error {
	if c.IDOrName == "" {
		return fmt.Errorf("usage: brain automation test <event-name>\n  Simulates an event and shows which automations would match")
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// List all active automations
	params := map[string]string{
		"type":   "automation",
		"status": "active",
	}
	if c.Flags.Project != "" {
		params["project"] = c.Flags.Project
	}

	resp, err := client.ListEntries(ctx, params)
	if err != nil {
		return fmt.Errorf("list automations: %w", err)
	}

	eventName := c.IDOrName
	fmt.Fprintf(out, "Simulating event: %q\n", eventName)
	fmt.Fprintln(out, strings.Repeat("─", 60))

	matched := 0
	for _, entry := range resp.Entries {
		if entry.Trigger == nil {
			continue
		}
		if entry.Trigger.Type == "event" && matchesEvent(entry.Trigger.Event, eventName) {
			matched++
			fmt.Fprintf(out, "\n  MATCH: %s (%s)\n", entry.Title, entry.ID)
			fmt.Fprintf(out, "    Trigger:  event=%s\n", entry.Trigger.Event)
			if entry.Action != nil {
				fmt.Fprintf(out, "    Action:   %s\n", entry.Action.Type)
				if entry.Action.DirectPrompt != "" {
					fmt.Fprintf(out, "    Prompt:   %s\n", truncate(entry.Action.DirectPrompt, 60))
				}
				if entry.Action.Command != "" {
					fmt.Fprintf(out, "    Command:  %s\n", truncate(entry.Action.Command, 60))
				}
			}
		}
	}

	fmt.Fprintln(out)
	if matched == 0 {
		fmt.Fprintf(out, "No automations matched event %q (dry-run, no tasks created)\n", eventName)
	} else {
		fmt.Fprintf(out, "%d automation(s) would match (dry-run, no tasks created)\n", matched)
	}
	return nil
}

// matchesEvent checks if an automation event pattern matches a given event name.
// Supports exact match and wildcard prefix (e.g., "task.*" matches "task.completed").
func matchesEvent(pattern, eventName string) bool {
	if pattern == eventName {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(eventName, prefix+".")
	}
	if pattern == "*" {
		return true
	}
	return false
}

// =============================================================================
// Subcommand: enable
// =============================================================================

func (c *AutomationCommand) executeEnable(out io.Writer) error {
	if c.IDOrName == "" {
		return fmt.Errorf("usage: brain automation enable <id>")
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status := "active"
	updates := map[string]interface{}{
		"status": status,
	}

	_, err := client.UpdateEntry(ctx, c.IDOrName, updates)
	if err != nil {
		return fmt.Errorf("enable automation: %w", err)
	}

	fmt.Fprintf(out, "Automation %s enabled (status: active)\n", c.IDOrName)
	return nil
}

// =============================================================================
// Subcommand: disable
// =============================================================================

func (c *AutomationCommand) executeDisable(out io.Writer) error {
	if c.IDOrName == "" {
		return fmt.Errorf("usage: brain automation disable <id>")
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	status := "archived"
	updates := map[string]interface{}{
		"status": status,
	}

	_, err := client.UpdateEntry(ctx, c.IDOrName, updates)
	if err != nil {
		return fmt.Errorf("disable automation: %w", err)
	}

	fmt.Fprintf(out, "Automation %s disabled (status: archived)\n", c.IDOrName)
	return nil
}

// =============================================================================
// Subcommand: history
// =============================================================================

func (c *AutomationCommand) executeHistory(out io.Writer) error {
	if c.IDOrName == "" {
		return fmt.Errorf("usage: brain automation history <id>")
	}

	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First get the automation entry to confirm it exists
	entry, err := client.GetEntry(ctx, c.IDOrName)
	if err != nil {
		return fmt.Errorf("get automation: %w", err)
	}

	if entry.Type != "automation" {
		return fmt.Errorf("entry %s is not an automation (type: %s)", c.IDOrName, entry.Type)
	}

	fmt.Fprintf(out, "History for automation: %s (%s)\n", entry.Title, entry.ID)
	fmt.Fprintln(out, strings.Repeat("─", 80))

	// Search for tasks generated by this automation using list with generated_by filter
	// We list tasks and filter client-side by generated_by field
	taskParams := map[string]string{
		"type": "task",
	}
	if c.Flags.Limit > 0 {
		taskParams["limit"] = fmt.Sprintf("%d", c.Flags.Limit)
	}
	if c.Flags.Project != "" {
		taskParams["project"] = c.Flags.Project
	} else if entry.ProjectID != "" {
		taskParams["project"] = entry.ProjectID
	}

	taskResp, err := client.ListEntries(ctx, taskParams)
	if err != nil {
		return fmt.Errorf("list tasks: %w", err)
	}

	// Filter to tasks actually generated by this automation
	var tasks []types.BrainEntry
	for _, t := range taskResp.Entries {
		if t.GeneratedBy == entry.ID || t.GeneratedBy == entry.Path {
			tasks = append(tasks, t)
		}
	}

	if len(tasks) == 0 {
		fmt.Fprintln(out, "No tasks generated by this automation yet")
		return nil
	}

	// JSON format
	if c.Flags.Format == "json" {
		data, _ := json.MarshalIndent(tasks, "", "  ")
		fmt.Fprintln(out, string(data))
		return nil
	}

	fmt.Fprintf(out, "%-10s %-30s %-12s %s\n", "ID", "Title", "Status", "Created")
	fmt.Fprintln(out, strings.Repeat("─", 80))

	for _, t := range tasks {
		created := t.Created
		if len(created) > 19 {
			created = created[:19]
		}
		fmt.Fprintf(out, "%-10s %-30s %-12s %s\n",
			t.ID,
			truncate(t.Title, 30),
			t.Status,
			created,
		)
	}

	fmt.Fprintln(out, strings.Repeat("─", 80))
	if !c.Flags.Quiet {
		fmt.Fprintf(out, "Total: %d task(s)\n", len(tasks))
	}
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// truncate shortens a string to max length, adding "..." if truncated.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
