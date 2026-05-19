package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Embeddings Command
// =============================================================================

// EmbeddingsFlags holds flags for the embeddings command.
type EmbeddingsFlags struct {
	Project string // --project (filter by project ID)
	Path    string // --path (filter by path prefix)
	All     bool   // --all (all projects; default when --project is omitted)
	Force   bool   // --force (re-embed even if current)
	DryRun  bool   // --dry-run (show what would be done without doing it)
	Verbose bool   // --verbose (show detailed progress)
}

// EmbeddingsCommand implements the Command interface for embeddings operations.
type EmbeddingsCommand struct {
	Subcommand string // "backfill" (only supported subcommand for now)
	Config     *UnifiedConfig
	Flags      *EmbeddingsFlags
	Out        io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *EmbeddingsCommand) Type() string {
	return "embeddings"
}

// Execute runs the embeddings command.
func (c *EmbeddingsCommand) Execute() error {
	out := c.out()

	switch c.Subcommand {
	case "backfill":
		return c.executeBackfill(out)
	case "":
		return fmt.Errorf("missing subcommand\nUsage: brain embeddings backfill [flags]\n\nAvailable subcommands:\n  backfill  Generate embeddings for existing notes")
	default:
		return fmt.Errorf("unknown embeddings subcommand: %q\nUsage: brain embeddings backfill [flags]", c.Subcommand)
	}
}

// out returns the output writer, defaulting to os.Stdout.
func (c *EmbeddingsCommand) out() io.Writer {
	if c.Out != nil {
		return c.Out
	}
	return os.Stdout
}

// getAPIClient returns the injected client or creates one from config.
func (c *EmbeddingsCommand) getAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	c.apiClient = runner.NewAPIClient(c.Config.Runner)
	return c.apiClient
}

// executeBackfill generates embeddings for existing notes that are missing embeddings or have stale embeddings.
func (c *EmbeddingsCommand) executeBackfill(out io.Writer) error {
	fmt.Fprintln(out, "Embeddings Backfill")
	fmt.Fprintln(out, "═══════════════════")
	fmt.Fprintln(out)

	if c.Flags.DryRun {
		fmt.Fprintln(out, "DRY RUN MODE: No changes will be made")
		fmt.Fprintln(out)
	}

	if c.Flags.Verbose {
		fmt.Fprintln(out, "Configuration:")
		fmt.Fprintf(out, "  API URL:    %s\n", c.Config.Runner.BrainAPIURL)
		if c.Flags.Project != "" {
			fmt.Fprintf(out, "  Project:    %s\n", c.Flags.Project)
		}
		if c.Flags.Path != "" {
			fmt.Fprintf(out, "  Path:       %s\n", c.Flags.Path)
		}
		fmt.Fprintf(out, "  Mode:       %s\n", c.embeddingMode())
		fmt.Fprintln(out)
	}

	req := types.EmbeddingBackfillRequest{
		Project: c.Flags.Project,
		Path:    c.Flags.Path,
		Force:   c.Flags.Force,
		DryRun:  c.Flags.DryRun,
	}

	if c.Flags.DryRun {
		return c.executeDryRun(out)
	}

	// Run the backfill
	fmt.Fprintln(out, "Generating embeddings for stale/missing notes...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := c.getAPIClient().BackfillEmbeddings(ctx, req)
	if err != nil {
		return fmt.Errorf("backfill embeddings: %w", err)
	}

	// Print results
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Results:")
	fmt.Fprintf(out, "  ✅ Processed: %d notes\n", result.Processed)
	if result.Skipped > 0 {
		fmt.Fprintf(out, "  ⏭  Skipped:   %d notes (empty or no chunks)\n", result.Skipped)
	}
	if result.Failed > 0 {
		fmt.Fprintf(out, "  ❌ Failed:    %d notes\n", result.Failed)
	}
	fmt.Fprintf(out, "  ⏱  Duration:  %s\n", result.Duration)
	fmt.Fprintln(out)

	if result.Failed > 0 {
		fmt.Fprintln(out, "⚠️  Some notes failed to process. Check the logs for details.")
		fmt.Fprintln(out, "   You can safely re-run this command to retry failed notes.")
	} else {
		fmt.Fprintln(out, "✅ Backfill complete! All notes have up-to-date embeddings.")
	}

	return nil
}

func (c *EmbeddingsCommand) embeddingMode() string {
	if c.Flags.Force {
		return "force re-embed matching notes"
	}
	return "embed missing or stale matching notes"
}

// executeDryRun shows what would be done without actually generating embeddings.
func (c *EmbeddingsCommand) executeDryRun(out io.Writer) error {
	entries, err := c.listDryRunCandidates()
	if err != nil {
		return err
	}

	fmt.Fprintln(out, "Notes that would be processed:")
	fmt.Fprintln(out)

	for _, entry := range entries {
		if c.Flags.Verbose {
			fmt.Fprintf(out, "  [%s] %s\n", entry.ID, entry.Path)
			fmt.Fprintf(out, "      Title: %s\n", entry.Title)
			if entry.ProjectID != "" {
				fmt.Fprintf(out, "      Project: %s\n", entry.ProjectID)
			}
			if entry.Type != "" {
				fmt.Fprintf(out, "      Type: %s\n", entry.Type)
			}
			fmt.Fprintln(out)
		} else {
			fmt.Fprintf(out, "  • %s\n", entry.Path)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Total notes to process: %d\n", len(entries))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run without --dry-run to generate embeddings.")

	return nil
}

func (c *EmbeddingsCommand) listDryRunCandidates() ([]types.BrainEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	const pageSize = 500
	var candidates []types.BrainEntry
	for offset := 0; ; offset += pageSize {
		params := map[string]string{
			"limit":     strconv.Itoa(pageSize),
			"offset":    strconv.Itoa(offset),
			"sortBy":    "modified",
			"sortOrder": "desc",
		}
		if c.Flags.Project != "" {
			params["project"] = c.Flags.Project
		}

		resp, err := c.getAPIClient().ListEntries(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("list entries for embedding dry-run: %w", err)
		}
		for _, entry := range resp.Entries {
			if c.Flags.Path != "" && !strings.HasPrefix(entry.Path, c.Flags.Path) {
				continue
			}
			if c.Flags.Force || embeddingBackfillNeeded(entry.EmbeddingStatus) {
				candidates = append(candidates, entry)
			}
		}
		if len(resp.Entries) < pageSize {
			break
		}
	}
	return candidates, nil
}

func embeddingBackfillNeeded(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "missing", "not_embedded", "not-embedded", "none", "stale", "needs_embedding", "needs-embedding", "outdated":
		return true
	default:
		return false
	}
}
