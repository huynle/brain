package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/huynle/brain-api/internal/indexer"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/service"
	"github.com/huynle/brain-api/internal/storage"
)

// =============================================================================
// Embeddings Command
// =============================================================================

// EmbeddingsFlags holds flags for the embeddings command.
type EmbeddingsFlags struct {
	Project string // --project (filter by project ID)
	Path    string // --path (filter by path prefix)
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
	brainDir := expandPath(c.Config.Server.BrainDir)

	fmt.Fprintln(out, "Embeddings Backfill")
	fmt.Fprintln(out, "═══════════════════")
	fmt.Fprintln(out)

	if c.Flags.DryRun {
		fmt.Fprintln(out, "DRY RUN MODE: No changes will be made")
		fmt.Fprintln(out)
	}

	// Initialize storage layer
	dbPath := filepath.Join(brainDir, "brain.db")
	store, err := storage.New(dbPath)
	if err != nil {
		return fmt.Errorf("initialize storage: %w", err)
	}
	defer store.Close()

	// Initialize embedding client
	embeddingClient, err := service.NewAiFactoryEmbeddingClient(c.Config.Server.Embedding)
	if err != nil {
		return fmt.Errorf("initialize embedding client: %w", err)
	}

	// Initialize indexer
	idx := indexer.NewIndexer(brainDir, store)

	if c.Flags.Verbose {
		fmt.Fprintln(out, "Configuration:")
		fmt.Fprintf(out, "  Brain Dir:  %s\n", brainDir)
		fmt.Fprintf(out, "  Model:      %s\n", c.Config.Server.Embedding.Model)
		if c.Flags.Project != "" {
			fmt.Fprintf(out, "  Project:    %s\n", c.Flags.Project)
		}
		if c.Flags.Path != "" {
			fmt.Fprintf(out, "  Path:       %s\n", c.Flags.Path)
		}
		fmt.Fprintln(out)
	}

	if c.Flags.DryRun {
		return c.executeDryRun(out, idx)
	}

	// Run the backfill
	fmt.Fprintln(out, "Generating embeddings for stale/missing notes...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := idx.IndexEmbeddings(ctx, embeddingClient)
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
	fmt.Fprintf(out, "  ⏱  Duration:  %s\n", result.Duration.Round(time.Millisecond))
	fmt.Fprintln(out)

	if result.Failed > 0 {
		fmt.Fprintln(out, "⚠️  Some notes failed to process. Check the logs for details.")
		fmt.Fprintln(out, "   You can safely re-run this command to retry failed notes.")
	} else {
		fmt.Fprintln(out, "✅ Backfill complete! All notes have up-to-date embeddings.")
	}

	return nil
}

// executeDryRun shows what would be done without actually generating embeddings.
func (c *EmbeddingsCommand) executeDryRun(out io.Writer, idx *indexer.Indexer) error {
	// Query notes that need embedding (re)generation
	// This duplicates the query from IndexEmbeddings, but allows us to show what would be done
	query := `
		SELECT DISTINCT n.id, n.path, n.title, n.project_id, n.type
		FROM notes n
		LEFT JOIN (
			SELECT note_id, MAX(embedding_indexed_at) as latest_indexed
			FROM note_embeddings_meta
			GROUP BY note_id
		) m ON n.id = m.note_id
		WHERE m.note_id IS NULL OR n.indexed_at > m.latest_indexed
	`

	rows, err := idx.DB().Query(query)
	if err != nil {
		return fmt.Errorf("query stale notes: %w", err)
	}
	defer rows.Close()

	count := 0
	fmt.Fprintln(out, "Notes that would be processed:")
	fmt.Fprintln(out)

	for rows.Next() {
		var id int64
		var path, title string
		var projectID, noteType *string

		if err := rows.Scan(&id, &path, &title, &projectID, &noteType); err != nil {
			fmt.Fprintf(out, "  ⚠️  Error scanning row: %v\n", err)
			continue
		}

		count++
		if c.Flags.Verbose {
			fmt.Fprintf(out, "  [%d] %s\n", id, path)
			fmt.Fprintf(out, "      Title: %s\n", title)
			if projectID != nil {
				fmt.Fprintf(out, "      Project: %s\n", *projectID)
			}
			if noteType != nil {
				fmt.Fprintf(out, "      Type: %s\n", *noteType)
			}
			fmt.Fprintln(out)
		} else {
			fmt.Fprintf(out, "  • %s\n", path)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Total notes to process: %d\n", count)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run without --dry-run to generate embeddings.")

	return nil
}
