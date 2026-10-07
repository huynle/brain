package commands

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
)

// runStopRenamed is the message for the removed `brain run stop`. It used to
// call POST /tasks/runner/pause, pausing every project on the server while
// leaving the local runner process running.
const runStopRenamed = "`brain run stop` was renamed to `brain run pause-all` (it pauses all projects server-wide; it never stopped the local runner). To stop a local runner, stop its process.\nFor a runner started with `brain runner start`, use `brain runner stop` (`--all` stops every runner on this machine)."

func (c *RunCommand) out() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stdout
}

func (c *RunCommand) in() io.Reader {
	if c.In != nil {
		return c.In
	}
	return os.Stdin
}

func (c *RunCommand) stdinIsTerminal() bool {
	if c.StdinIsTerminal != nil {
		return c.StdinIsTerminal()
	}
	// A character-device check is not enough: /dev/null is one, and cron or
	// CI jobs commonly run with stdin redirected from it.
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

// confirm asks a y/N question. --yes answers it; without --yes it refuses
// unless stdin is a terminal, so a script can never trigger a server-wide
// change by accident.
func (c *RunCommand) confirm(action, question string) (bool, error) {
	if c.Flags != nil && c.Flags.Yes {
		return true, nil
	}
	if err := c.requireConfirmable(action); err != nil {
		return false, err
	}
	fmt.Fprintf(c.out(), "%s [y/N] ", question)
	line, err := bufio.NewReader(c.in()).ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(c.out())
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	}
	return false, nil
}

// requireConfirmable refuses up front, before any request is made, when a
// confirmation would be needed but cannot be asked.
func (c *RunCommand) requireConfirmable(action string) error {
	if (c.Flags != nil && c.Flags.Yes) || c.stdinIsTerminal() {
		return nil
	}
	return fmt.Errorf("refusing to %s without confirmation: stdin is not a terminal (pass --yes to confirm)", action)
}

// projectArg returns the explicit project positional, or "" when none was
// given ("all" is the router's placeholder for "no project").
func (c *RunCommand) projectArg() string {
	if c.Project == "all" {
		return ""
	}
	return c.Project
}

// rejectProjectArgs refuses positionals on a server-wide command: someone who
// typed `pause-all demo` meant `pause demo`, and --yes would otherwise skip
// the only prompt that says "every project".
func (c *RunCommand) rejectProjectArgs(single string) error {
	if len(c.Args) == 0 {
		return nil
	}
	return &UsageError{Message: fmt.Sprintf("brain run %s: takes no arguments (got %q); it acts on every project server-wide.\nFor one project, use `brain run %s <project>`.",
		c.Subcommand, strings.Join(c.Args, " "), single)}
}

func (c *RunCommand) runPauseAll() error {
	if err := c.rejectProjectArgs("pause"); err != nil {
		return err
	}
	if err := c.requireConfirmable("pause all projects"); err != nil {
		return err
	}
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	scope := "every project"
	if projects, err := client.ListProjects(ctx); err == nil {
		scope = fmt.Sprintf("all %d projects", len(projects))
	} else {
		fmt.Fprintf(c.out(), "Could not count projects: %v\n", err)
	}
	fmt.Fprintf(c.out(), "This pauses task execution for %s on %s, for every runner (server-wide).\n", scope, c.apiURL())
	fmt.Fprintln(c.out(), "It does not stop any runner process. Undo with `brain run resume-all`.")
	ok, err := c.confirm("pause all projects", "Pause "+scope+"?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(c.out(), "Aborted; nothing was paused.")
		return nil
	}
	if err := client.PauseAll(ctx); err != nil {
		return fmt.Errorf("failed to pause all projects: %w", err)
	}
	fmt.Fprintf(c.out(), "Paused %s.\n", strings.TrimPrefix(scope, "all "))
	return nil
}

func (c *RunCommand) runResumeAll() error {
	if err := c.rejectProjectArgs("resume"); err != nil {
		return err
	}
	if err := c.requireConfirmable("resume all projects"); err != nil {
		return err
	}
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	ctx := context.Background()
	status, err := client.GetRunnerStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get runner status: %w", err)
	}
	if !status.Paused && len(status.PausedProjects) == 0 {
		fmt.Fprintln(c.out(), "No projects are paused; nothing to resume.")
		return nil
	}
	// The server reports paused=true whenever ANY project is paused, so the
	// scope comes from pausedProjects, compared with the known projects.
	if len(status.PausedProjects) == 0 {
		fmt.Fprintln(c.out(), "Task execution is paused server-wide.")
	} else if known, err := client.ListProjects(ctx); err == nil && len(known) > 0 && allIn(known, status.PausedProjects) {
		fmt.Fprintf(c.out(), "All %d projects are paused.\n", len(known))
	}
	if len(status.PausedProjects) > 0 {
		fmt.Fprintf(c.out(), "Currently paused projects (%d):\n", len(status.PausedProjects))
		for _, p := range status.PausedProjects {
			fmt.Fprintf(c.out(), "  %s\n", p)
		}
	}
	fmt.Fprintln(c.out(), "Warning: this also resumes projects that were paused individually (`brain run pause <project>`).")
	fmt.Fprintln(c.out(), "To resume a single project instead, use `brain run resume <project>`.")
	ok, err := c.confirm("resume all projects", "Resume all projects on "+c.apiURL()+"?")
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(c.out(), "Aborted; nothing was resumed.")
		return nil
	}
	if err := client.ResumeAll(ctx); err != nil {
		return fmt.Errorf("failed to resume all projects: %w", err)
	}
	fmt.Fprintln(c.out(), "Resumed all projects.")
	return nil
}

func (c *RunCommand) runPauseProject() error {
	project := c.projectArg()
	if project == "" {
		return &UsageError{Message: "brain run pause: project required: brain run pause <project>\n(To pause every project server-wide, use `brain run pause-all`.)"}
	}
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	if err := client.PauseProject(context.Background(), project); err != nil {
		return fmt.Errorf("failed to pause %s: %w", project, err)
	}
	fmt.Fprintf(c.out(), "Paused project %s.\n", project)
	return nil
}

func (c *RunCommand) runResumeProject() error {
	project := c.projectArg()
	if project == "" {
		return &UsageError{Message: "brain run resume: project required: brain run resume <project>\n(To resume every project server-wide, use `brain run resume-all`.)"}
	}
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	if err := client.ResumeProject(context.Background(), project); err != nil {
		return fmt.Errorf("failed to resume %s: %w", project, err)
	}
	fmt.Fprintf(c.out(), "Resumed project %s.\n", project)
	return nil
}

func (c *RunCommand) apiURL() string {
	if u := c.Config.Runner.BrainAPIURL; u != "" {
		return u
	}
	return c.Config.MCP.APIURL
}

// allIn reports whether every element of want appears in have.
func allIn(want, have []string) bool {
	set := make(map[string]bool, len(have))
	for _, h := range have {
		set[h] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
