package commands

import (
	"context"
	"fmt"
	"strconv"
	"text/tabwriter"
)

func (c *RunCommand) runFeatures() error {
	if err := c.rejectExtraArgs(1, "brain run features <project>"); err != nil {
		return err
	}
	project := c.projectArg()
	if project == "" {
		return &UsageError{Message: "brain run features: project required: brain run features <project>"}
	}
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	features, err := client.GetFeatures(context.Background(), project)
	if err != nil {
		return fmt.Errorf("failed to list features for %s: %w", project, err)
	}
	if len(features) == 0 {
		fmt.Fprintf(c.out(), "No features found for project %s.\n", project)
		return nil
	}

	w := tabwriter.NewWriter(c.out(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "FEATURE\tTASKS\tCOMPLETED\tREADY\tWAITING\tBLOCKED\tSTARTABLE")
	var unresolved []string
	for _, f := range features {
		// Classification mirrors the server: ready (runnable now), waiting
		// (dependencies still in progress) and blocked (unmet/blocked deps).
		completed, ready, waiting, blocked := 0, 0, 0, 0
		for _, t := range f.Tasks {
			if t.Status == "completed" {
				completed++
			}
			switch t.Classification {
			case "ready":
				ready++
			case "waiting":
				waiting++
			case "blocked":
				blocked++
			}
		}
		startable := "no"
		if f.Ready {
			startable = "yes"
		}
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%s\n", f.FeatureID, len(f.Tasks), completed, ready, waiting, blocked, startable)
		for _, dep := range f.UnresolvedFeatureDeps {
			unresolved = append(unresolved, fmt.Sprintf("%s depends on unknown feature %q", f.FeatureID, dep))
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	for _, u := range unresolved {
		fmt.Fprintf(c.out(), "warning: %s\n", u)
	}
	return nil
}

// defaultLogLimit matches the server's default page size for task logs.
const defaultLogLimit = 100

func (c *RunCommand) runLogs() error {
	if err := c.rejectExtraArgs(2, "brain run logs <project> <taskId> [--limit N]"); err != nil {
		return err
	}
	if len(c.Args) < 2 {
		return &UsageError{Message: "brain run logs: usage: brain run logs <project> <taskId> [--limit N]"}
	}
	project, taskID := c.Args[0], c.Args[1]
	limit := defaultLogLimit
	if c.Flags != nil && c.Flags.Limit > 0 {
		limit = c.Flags.Limit
	}
	if c.Flags != nil && (c.Flags.Follow || c.Flags.Foreground) {
		fmt.Fprintln(c.out(), "note: the task logs endpoint does not stream, so -f/--follow is not supported; showing the latest lines once.")
	}
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	resp, err := client.GetTaskLogs(context.Background(), project, taskID, limit)
	if err != nil {
		return fmt.Errorf("failed to get logs for %s/%s: %w", project, taskID, err)
	}
	if len(resp.Lines) == 0 {
		fmt.Fprintf(c.out(), "No log lines for %s/%s.\n", project, taskID)
		return nil
	}
	w := tabwriter.NewWriter(c.out(), 0, 4, 2, ' ', 0)
	for _, l := range resp.Lines {
		fmt.Fprintf(w, "%s\t%s\t%s\n", l.Timestamp, l.Level, l.Content)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if resp.Total > len(resp.Lines) {
		fmt.Fprintf(c.out(), "(showing the latest %d of %d lines; use --limit for more, max 1000)\n", len(resp.Lines), resp.Total)
	}
	return nil
}

func (c *RunCommand) runConfig() error {
	client, err := c.makeAPIClient()
	if err != nil {
		return err
	}
	d, err := client.GetTaskDefaults(context.Background())
	if err != nil {
		return fmt.Errorf("could not fetch task defaults from %s: %w", c.apiURL(), err)
	}
	orDash := func(v string) string {
		if v == "" {
			return "-"
		}
		return v
	}
	boolOrDash := func(v *bool) string {
		if v == nil {
			return "-"
		}
		return strconv.FormatBool(*v)
	}
	w := tabwriter.NewWriter(c.out(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KEY\tVALUE")
	for _, kv := range [][2]string{
		{"agent", orDash(d.Agent)},
		{"model", orDash(d.Model)},
		{"execution_mode", orDash(d.ExecutionMode)},
		{"complete_on_idle", boolOrDash(d.CompleteOnIdle)},
		{"merge_policy", orDash(d.MergePolicy)},
		{"merge_strategy", orDash(d.MergeStrategy)},
		{"merge_target_branch", orDash(d.MergeTargetBranch)},
		{"remote_branch_policy", orDash(d.RemoteBranchPolicy)},
		{"open_pr_before_merge", boolOrDash(d.OpenPRBeforeMerge)},
		{"target_workdir", orDash(d.TargetWorkdir)},
	} {
		fmt.Fprintf(w, "%s\t%s\n", kv[0], kv[1])
	}
	return w.Flush()
}
