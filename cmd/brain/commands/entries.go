package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Entry Get Command
// =============================================================================

// EntryGetFlags holds flags for the brain get command.
type EntryGetFlags struct {
	Format  string // --format (path, id, short, full, json, jsonl, or Go template)
	Quiet   bool   // -q, --quiet
	NoColor bool   // --no-color
}

// GetCommand implements the Command interface for reading brain entries.
type GetCommand struct {
	Config   *UnifiedConfig
	Flags    *EntryGetFlags
	IDOrPath string // Positional argument: short ID or full path
	Out      io.Writer
	IsTTY    bool // Whether stdout is a TTY (set by router)

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *GetCommand) Type() string {
	return "get"
}

// Execute runs the get command.
func (c *GetCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	if c.IDOrPath == "" {
		return fmt.Errorf("usage: brain get <id-or-path> [--format <format>]")
	}

	client := c.getGetAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Determine effective format
	output := &OutputConfig{
		Format:  c.Flags.Format,
		Quiet:   c.Flags.Quiet,
		NoColor: c.Flags.NoColor,
	}
	format := output.DetectDefaultFormat(c.IsTTY)
	output.Format = format

	// Handle raw markdown for pipe mode (non-TTY with no explicit format)
	if !c.IsTTY && c.Flags.Format == "" {
		raw, _, err := client.GetEntryRaw(ctx, c.IDOrPath)
		if err != nil {
			return fmt.Errorf("get entry: %w", err)
		}
		fmt.Fprint(out, raw)
		return nil
	}

	// Handle "full" format: get frontmatter + body from API
	if format == "full" {
		full, err := client.GetEntryFull(ctx, c.IDOrPath)
		if err != nil {
			return fmt.Errorf("get entry: %w", err)
		}
		fmt.Fprint(out, full)
		return nil
	}

	// All other formats: get structured JSON entry from API
	entry, err := client.GetEntry(ctx, c.IDOrPath)
	if err != nil {
		return fmt.Errorf("get entry: %w", err)
	}

	// TTY default: rich metadata header + content
	if c.IsTTY && (format == "short" || format == "") {
		c.renderTTY(out, entry)
		return nil
	}

	// Named formats and custom templates
	result := output.FormatEntry(*entry)
	fmt.Fprintln(out, result)
	return nil
}

// renderTTY outputs a rich, human-readable display for interactive terminals.
func (c *GetCommand) renderTTY(out io.Writer, entry *types.BrainEntry) {
	// Metadata header
	fmt.Fprintf(out, "Path:     %s\n", entry.Path)
	fmt.Fprintf(out, "Title:    %s\n", entry.Title)
	fmt.Fprintf(out, "Type:     %s\n", entry.Type)
	if entry.Status != "" {
		fmt.Fprintf(out, "Status:   %s\n", entry.Status)
	}
	if entry.Priority != "" {
		fmt.Fprintf(out, "Priority: %s\n", entry.Priority)
	}
	if len(entry.Tags) > 0 {
		fmt.Fprintf(out, "Tags:     %s\n", strings.Join(entry.Tags, ", "))
	}
	if entry.ID != "" {
		fmt.Fprintf(out, "ID:       %s\n", entry.ID)
	}

	// Separator
	fmt.Fprintln(out, strings.Repeat("─", 40))

	// Content body
	if entry.Content != "" {
		fmt.Fprintln(out, entry.Content)
	}
}

// getGetAPIClient returns the API client, creating one from config if not injected.
func (c *GetCommand) getGetAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	return runner.NewAPIClient(c.Config.Runner)
}

// =============================================================================
// Entry Save Command
// =============================================================================

// EntrySaveFlags holds flags for the brain save command.
type EntrySaveFlags struct {
	Type      string
	Title     string
	Content   string
	NoEdit    bool
	Tags      string
	Status    string
	Priority  string
	DependsOn string
	FeatureID string
	Global    bool
	Project   string
}

// SaveCommand implements the Command interface for creating brain entries.
type SaveCommand struct {
	Config *UnifiedConfig
	Flags  *EntrySaveFlags
	Out    io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *SaveCommand) Type() string {
	return "save"
}

// Execute runs the save command.
func (c *SaveCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	// Validate required flags
	if c.Flags.Type == "" {
		return fmt.Errorf("--type is required")
	}
	if c.Flags.Title == "" {
		return fmt.Errorf("--title is required")
	}

	// Resolve content
	content, err := c.resolveContent()
	if err != nil {
		return err
	}

	// Build the API request
	req := c.buildRequest(content)

	// Send to API
	client := c.getAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.CreateEntry(ctx, req)
	if err != nil {
		return fmt.Errorf("create entry: %w", err)
	}

	fmt.Fprintf(out, "Saved: %s (ID: %s)\n", resp.Path, resp.ID)
	return nil
}

// resolveContent determines the content for the entry based on flags.
// Priority: --content flag → $EDITOR (default)
func (c *SaveCommand) resolveContent() (string, error) {
	if c.Flags.Content != "" {
		return c.resolveContentSource(c.Flags.Content)
	}

	// If --no-edit is set without --content, use empty content
	if c.Flags.NoEdit {
		return "", nil
	}

	// Default: open $EDITOR
	return c.openEditor()
}

// resolveContentSource resolves the --content flag value.
// Supports: literal text, "-" for stdin, "@path" for file.
func (c *SaveCommand) resolveContentSource(source string) (string, error) {
	if source == "-" {
		// Read from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}

	if strings.HasPrefix(source, "@") {
		// Read from file
		path := source[1:]
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", path, err)
		}
		return string(data), nil
	}

	// Literal text
	return source, nil
}

// openEditor opens $VISUAL/$EDITOR/vi with a template and returns the edited content.
func (c *SaveCommand) openEditor() (string, error) {
	// Create temp file with template
	tmpFile, err := os.CreateTemp("", "brain-save-*.md")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write template
	template := c.buildEditorTemplate()
	if _, err := tmpFile.WriteString(template); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write template: %w", err)
	}
	tmpFile.Close()

	// Determine editor
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	// Open editor
	cmd := exec.Command(editor, tmpFile.Name())
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}

	// Read back the file
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return "", fmt.Errorf("read edited file: %w", err)
	}

	// Parse: strip frontmatter and comment lines
	content := c.parseEditorContent(string(data))

	// Empty content means cancel
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("cancelled (empty content)")
	}

	return content, nil
}

// buildEditorTemplate creates the template shown in $EDITOR.
func (c *SaveCommand) buildEditorTemplate() string {
	var buf bytes.Buffer

	buf.WriteString("---\n")
	buf.WriteString(fmt.Sprintf("title: %s\n", c.Flags.Title))
	buf.WriteString(fmt.Sprintf("type: %s\n", c.Flags.Type))

	if c.Flags.Tags != "" {
		buf.WriteString("tags:\n")
		for _, tag := range strings.Split(c.Flags.Tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				buf.WriteString(fmt.Sprintf("  - %s\n", tag))
			}
		}
	} else {
		buf.WriteString("tags:\n")
		buf.WriteString(fmt.Sprintf("  - %s\n", c.Flags.Type))
	}

	status := c.Flags.Status
	if status == "" {
		if c.Flags.Type == "task" {
			status = "draft"
		} else {
			status = "active"
		}
	}
	buf.WriteString(fmt.Sprintf("status: %s\n", status))

	priority := c.Flags.Priority
	if priority == "" {
		priority = "medium"
	}
	buf.WriteString(fmt.Sprintf("priority: %s\n", priority))

	buf.WriteString("---\n\n")
	buf.WriteString("<!-- Enter your content below. Save and quit to create the entry. -->\n")
	buf.WriteString("<!-- Delete all content and save to cancel. -->\n\n")

	return buf.String()
}

// parseEditorContent strips frontmatter and HTML comments from editor output.
func (c *SaveCommand) parseEditorContent(raw string) string {
	content := raw

	// Strip YAML frontmatter (--- ... ---)
	if strings.HasPrefix(content, "---") {
		// Find the closing ---
		rest := content[3:]
		idx := strings.Index(rest, "\n---")
		if idx >= 0 {
			content = rest[idx+4:] // skip past closing "---\n"
		}
	}

	// Remove HTML comment lines
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<!--") && strings.HasSuffix(trimmed, "-->") {
			continue
		}
		lines = append(lines, line)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// buildRequest constructs the CreateEntryRequest from flags and content.
func (c *SaveCommand) buildRequest(content string) types.CreateEntryRequest {
	req := types.CreateEntryRequest{
		Type:    c.Flags.Type,
		Title:   c.Flags.Title,
		Content: content,
	}

	if c.Flags.Tags != "" {
		for _, tag := range strings.Split(c.Flags.Tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				req.Tags = append(req.Tags, tag)
			}
		}
	}

	if c.Flags.Status != "" {
		req.Status = c.Flags.Status
	}

	if c.Flags.Priority != "" {
		req.Priority = c.Flags.Priority
	}

	if c.Flags.DependsOn != "" {
		for _, dep := range strings.Split(c.Flags.DependsOn, ",") {
			dep = strings.TrimSpace(dep)
			if dep != "" {
				req.DependsOn = append(req.DependsOn, dep)
			}
		}
	}

	if c.Flags.FeatureID != "" {
		req.FeatureID = c.Flags.FeatureID
	}

	if c.Flags.Global {
		globalTrue := true
		req.Global = &globalTrue
	}

	if c.Flags.Project != "" {
		req.Project = c.Flags.Project
	}

	return req
}

// getAPIClient returns the API client, creating one from config if not injected.
func (c *SaveCommand) getAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	return runner.NewAPIClient(c.Config.Runner)
}

// =============================================================================
// Entry Update Command
// =============================================================================

// EntryUpdateFlags holds flags for the brain update command.
type EntryUpdateFlags struct {
	Status    string
	Title     string
	Content   string
	Append    string
	Note      string
	Tags      string
	Priority  string
	DependsOn string
	FeatureID string
}

// UpdateCommand implements the Command interface for updating brain entries.
type UpdateCommand struct {
	IDOrPath string
	Config   *UnifiedConfig
	Flags    *EntryUpdateFlags
	Out      io.Writer

	// stdin allows injecting a reader for testing; nil means os.Stdin.
	Stdin io.Reader

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *UpdateCommand) Type() string {
	return "update"
}

// Execute runs the update command.
func (c *UpdateCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	if c.IDOrPath == "" {
		return fmt.Errorf("entry ID or path is required\nUsage: brain update <id-or-path> --status <status> [flags]")
	}

	// Validate that at least one flag is provided
	if !c.hasAnyFlag() {
		return fmt.Errorf("at least one update flag is required\nUsage: brain update <id-or-path> --status <status> [flags]")
	}

	// Validate mutually exclusive flags
	if c.Flags.Content != "" && c.Flags.Append != "" {
		return fmt.Errorf("--content and --append are mutually exclusive")
	}

	// Build the update request
	updates := make(map[string]interface{})

	if c.Flags.Status != "" {
		updates["status"] = c.Flags.Status
	}

	if c.Flags.Title != "" {
		updates["title"] = c.Flags.Title
	}

	if c.Flags.Note != "" {
		updates["note"] = c.Flags.Note
	}

	if c.Flags.Priority != "" {
		updates["priority"] = c.Flags.Priority
	}

	if c.Flags.FeatureID != "" {
		updates["feature_id"] = c.Flags.FeatureID
	}

	if c.Flags.Tags != "" {
		tags := splitCSV(c.Flags.Tags)
		updates["tags"] = tags
	}

	if c.Flags.DependsOn != "" {
		deps := splitCSV(c.Flags.DependsOn)
		updates["depends_on"] = deps
	}

	// Handle --content (supports inline, stdin "-", and @file)
	if c.Flags.Content != "" {
		content, err := c.resolveContentSource(c.Flags.Content)
		if err != nil {
			return err
		}
		updates["content"] = content
	}

	// Handle --append
	if c.Flags.Append != "" {
		updates["append"] = c.Flags.Append
	}

	// Send to API
	client := c.getUpdateAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	entry, err := client.UpdateEntry(ctx, c.IDOrPath, updates)
	if err != nil {
		return fmt.Errorf("update entry: %w", err)
	}

	// Print confirmation
	fmt.Fprintf(out, "Updated: %s\n", entry.ID)
	c.printChanges(out, updates)

	return nil
}

// hasAnyFlag returns true if at least one update flag is set.
func (c *UpdateCommand) hasAnyFlag() bool {
	f := c.Flags
	return f.Status != "" || f.Title != "" || f.Content != "" ||
		f.Append != "" || f.Note != "" || f.Tags != "" ||
		f.Priority != "" || f.DependsOn != "" || f.FeatureID != ""
}

// resolveContentSource resolves the --content flag value.
// Supports: literal text, "-" for stdin, "@path" for file.
func (c *UpdateCommand) resolveContentSource(source string) (string, error) {
	if source == "-" {
		reader := c.Stdin
		if reader == nil {
			reader = os.Stdin
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}

	if strings.HasPrefix(source, "@") {
		path := source[1:]
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", path, err)
		}
		return string(data), nil
	}

	// Literal text
	return source, nil
}

// printChanges prints a summary of what was updated.
func (c *UpdateCommand) printChanges(out io.Writer, updates map[string]interface{}) {
	for key, val := range updates {
		switch v := val.(type) {
		case []string:
			fmt.Fprintf(out, "  %s: %s\n", key, strings.Join(v, ", "))
		case string:
			// Truncate long content for display
			display := v
			if len(display) > 80 {
				display = display[:77] + "..."
			}
			fmt.Fprintf(out, "  %s: %s\n", key, display)
		default:
			fmt.Fprintf(out, "  %s: %v\n", key, val)
		}
	}
}

// getUpdateAPIClient returns the API client, creating one from config if not injected.
func (c *UpdateCommand) getUpdateAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	return runner.NewAPIClient(c.Config.Runner)
}

// splitCSV splits a comma-separated string into trimmed, non-empty parts.
func splitCSV(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// =============================================================================
// Entry Search Command
// =============================================================================

// EntrySearchFlags holds flags for the brain search command.
type EntrySearchFlags struct {
	Filter      BrainFilter
	Output      OutputConfig
	Interactive bool // -i / --interactive: fzf post-filter
}

// SearchCommand implements the Command interface for searching brain entries.
type SearchCommand struct {
	Query  string
	Config *UnifiedConfig
	Flags  *EntrySearchFlags
	Out    io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *SearchCommand) Type() string {
	return "search"
}

// Execute runs the search command.
func (c *SearchCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	if c.Query == "" {
		return fmt.Errorf("search query is required\nUsage: brain search <query> [flags]")
	}

	// Validate filter flags
	if err := c.Flags.Filter.Validate(); err != nil {
		return err
	}

	// Detect TTY for default format
	isTTY := isTerminal(out)
	c.Flags.Output.Format = c.Flags.Output.DetectDefaultFormat(isTTY)

	// Build search request
	req := c.buildSearchRequest()

	// Call API
	client := c.getSearchAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.SearchEntries(ctx, req)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}

	// Convert SearchResult to BrainEntry for formatting
	entries := searchResultsToEntries(resp.Results)

	// Interactive fzf post-filter
	if c.Flags.Interactive {
		selected, err := fzfSelect(entries)
		if err != nil {
			return err
		}
		entries = selected
	}

	// Format and output
	if len(entries) == 0 {
		if !c.Flags.Output.Quiet {
			fmt.Fprintln(out, "No results found")
		}
		return nil
	}

	output := c.Flags.Output.FormatEntries(entries)
	if output != "" {
		fmt.Fprint(out, output)
		// Ensure trailing newline for non-NUL delimiters
		if c.Flags.Output.Delimiter != "\x00" && !strings.HasSuffix(output, "\n") {
			fmt.Fprintln(out)
		}
	}

	if !c.Flags.Output.Quiet {
		fmt.Fprintf(out, "\nFound %d entries\n", resp.Total)
	}

	return nil
}

// buildSearchRequest constructs the SearchRequest from query + filter flags.
func (c *SearchCommand) buildSearchRequest() types.SearchRequest {
	req := types.SearchRequest{
		Query: c.Query,
	}
	if c.Flags.Filter.Type != "" {
		req.Type = c.Flags.Filter.Type
	}
	if c.Flags.Filter.Status != "" {
		req.Status = c.Flags.Filter.Status
	}
	if c.Flags.Filter.FeatureID != "" {
		req.FeatureID = c.Flags.Filter.FeatureID
	}
	if c.Flags.Filter.Tags != "" {
		for _, tag := range strings.Split(c.Flags.Filter.Tags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				req.Tags = append(req.Tags, tag)
			}
		}
	}
	if c.Flags.Filter.Limit > 0 {
		limit := c.Flags.Filter.Limit
		req.Limit = &limit
	}
	return req
}

// getSearchAPIClient returns the API client, creating one from config if not injected.
func (c *SearchCommand) getSearchAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	return runner.NewAPIClient(c.Config.Runner)
}

// =============================================================================
// Entry List Command
// =============================================================================

// EntryListFlags holds flags for the brain list command.
type EntryListFlags struct {
	Filter      BrainFilter
	Output      OutputConfig
	Interactive bool // -i / --interactive: fzf post-filter
}

// ListCommand implements the Command interface for listing brain entries.
type ListCommand struct {
	Config *UnifiedConfig
	Flags  *EntryListFlags
	Out    io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *ListCommand) Type() string {
	return "list"
}

// Execute runs the list command.
func (c *ListCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	// Validate filter flags
	if err := c.Flags.Filter.Validate(); err != nil {
		return err
	}

	// Detect TTY for default format
	isTTY := isTerminal(out)
	c.Flags.Output.Format = c.Flags.Output.DetectDefaultFormat(isTTY)

	// Build query params from filter
	params := c.Flags.Filter.ToQueryParams()

	// Call API
	client := c.getListAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := client.ListEntries(ctx, params)
	if err != nil {
		return fmt.Errorf("list: %w", err)
	}

	entries := resp.Entries

	// Interactive fzf post-filter
	if c.Flags.Interactive {
		selected, err := fzfSelect(entries)
		if err != nil {
			return err
		}
		entries = selected
	}

	// Format and output
	if len(entries) == 0 {
		if !c.Flags.Output.Quiet {
			fmt.Fprintln(out, "No entries found")
		}
		return nil
	}

	output := c.Flags.Output.FormatEntries(entries)
	if output != "" {
		fmt.Fprint(out, output)
		// Ensure trailing newline for non-NUL delimiters
		if c.Flags.Output.Delimiter != "\x00" && !strings.HasSuffix(output, "\n") {
			fmt.Fprintln(out)
		}
	}

	if !c.Flags.Output.Quiet {
		fmt.Fprintf(out, "\nFound %d entries\n", resp.Total)
	}

	return nil
}

// getListAPIClient returns the API client, creating one from config if not injected.
func (c *ListCommand) getListAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	return runner.NewAPIClient(c.Config.Runner)
}

// =============================================================================
// Entry Edit Command
// =============================================================================

// EntryEditFlags holds flags for the brain edit command.
type EntryEditFlags struct {
	Filter      BrainFilter
	Interactive bool   // -i / --interactive: fzf selection
	Force       bool   // --force: skip safety confirmation
	NoColor     bool   // --no-color
	Quiet       bool   // -q, --quiet
	Format      string // --format for output
}

// EditCommand implements the Command interface for editing brain entries in $EDITOR.
type EditCommand struct {
	IDOrPath string
	Config   *UnifiedConfig
	Flags    *EntryEditFlags
	Out      io.Writer

	// apiClient is injectable for testing; nil means create from config.
	apiClient *runner.APIClient
}

// Type returns the command type identifier.
func (c *EditCommand) Type() string {
	return "edit"
}

// Execute runs the edit command.
func (c *EditCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	client := c.getEditAPIClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Determine target entry path(s)
	entryPath, err := c.resolveTarget(ctx, client, out)
	if err != nil {
		return err
	}
	if entryPath == "" {
		return nil // User cancelled
	}

	// Fetch full entry content (YAML frontmatter + body)
	original, err := client.GetEntryFull(ctx, entryPath)
	if err != nil {
		return fmt.Errorf("get entry: %w", err)
	}

	// Write to temp file
	shortID := extractShortID(entryPath)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("brain-edit-%s-*.md", shortID))
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(original); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmpFile.Close()

	// Open in $EDITOR
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	cmd := exec.Command(editor, tmpPath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor failed: %w", err)
	}

	// Read modified file
	modified, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read modified file: %w", err)
	}

	// Compare with original (byte-level diff)
	if string(modified) == original {
		fmt.Fprintln(out, "No changes made")
		return nil
	}

	// Check for dangerous field changes
	if !c.Flags.Force {
		if warn := c.checkDangerousChanges(original, string(modified)); warn != "" {
			fmt.Fprintf(out, "WARNING: %s\n", warn)
			fmt.Fprint(out, "Continue? [y/N] ")

			var answer string
			// A read failure leaves answer empty, which is not "y" — the
			// prompt then cancels, which is the safe default.
			_, _ = fmt.Fscan(os.Stdin, &answer)
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Fprintln(out, "Cancelled")
				return nil
			}
		}
	}

	// Send update via API
	if err := client.UpdateEntryFull(ctx, entryPath, string(modified)); err != nil {
		return fmt.Errorf("update entry: %w", err)
	}

	// Print summary of changes
	c.printEditSummary(out, original, string(modified))
	return nil
}

// resolveTarget determines which entry to edit based on flags and positional args.
func (c *EditCommand) resolveTarget(ctx context.Context, client *runner.APIClient, out io.Writer) (string, error) {
	// Direct ID/path provided
	if c.IDOrPath != "" && !c.Flags.Interactive {
		return c.IDOrPath, nil
	}

	// Interactive mode or filter-based selection
	if c.Flags.Interactive || c.hasFilterFlags() {
		// Validate filter flags
		if err := c.Flags.Filter.Validate(); err != nil {
			return "", err
		}

		params := c.Flags.Filter.ToQueryParams()
		resp, err := client.ListEntries(ctx, params)
		if err != nil {
			return "", fmt.Errorf("list entries: %w", err)
		}

		if len(resp.Entries) == 0 {
			fmt.Fprintln(out, "No entries found matching filters")
			return "", nil
		}

		// If interactive, always use fzf
		if c.Flags.Interactive {
			selected, err := fzfSelect(resp.Entries)
			if err != nil {
				return "", err
			}
			if len(selected) == 0 {
				return "", nil
			}
			return selected[0].Path, nil
		}

		// Non-interactive with filters: safety check for >5 entries
		if len(resp.Entries) > 5 && !c.Flags.Force {
			fmt.Fprintf(out, "Are you sure you want to open %d entries? [y/N] ", len(resp.Entries))

			var answer string
			// A read failure leaves answer empty, which is not "y" — the
			// prompt then cancels, which is the safe default.
			_, _ = fmt.Fscan(os.Stdin, &answer)
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Fprintln(out, "Cancelled (use --force to skip confirmation)")
				return "", nil
			}
		}

		// For non-interactive multi-entry: edit first matching entry
		// (batch editing is complex; single entry is the primary use case)
		if len(resp.Entries) == 1 {
			return resp.Entries[0].Path, nil
		}

		// Multiple entries without interactive: suggest using -i
		fmt.Fprintf(out, "Found %d entries. Use -i to select interactively.\n", len(resp.Entries))
		return "", nil
	}

	// No ID and no flags
	if c.IDOrPath != "" {
		return c.IDOrPath, nil
	}
	return "", fmt.Errorf("usage: brain edit <id-or-path> or brain edit -i [filters]")
}

// hasFilterFlags returns true if any filter flags are set.
func (c *EditCommand) hasFilterFlags() bool {
	f := c.Flags.Filter
	return f.Type != "" || f.Status != "" || f.Tags != "" ||
		f.Priority != "" || f.FeatureID != ""
}

// checkDangerousChanges compares frontmatter fields that are dangerous to change.
// Returns a warning string if dangerous changes detected, empty string otherwise.
func (c *EditCommand) checkDangerousChanges(original, modified string) string {
	origFields := extractFrontmatterField(original)
	modFields := extractFrontmatterField(modified)

	var warnings []string

	dangerousFields := []string{"type", "project"}
	for _, field := range dangerousFields {
		origVal := origFields[field]
		modVal := modFields[field]
		if origVal != modVal && origVal != "" && modVal != "" {
			warnings = append(warnings, fmt.Sprintf("%s changed: %q -> %q", field, origVal, modVal))
		}
	}

	if len(warnings) == 0 {
		return ""
	}
	return "Dangerous field changes detected: " + strings.Join(warnings, "; ")
}

// extractFrontmatterField extracts simple key: value pairs from YAML frontmatter.
func extractFrontmatterField(content string) map[string]string {
	fields := make(map[string]string)
	if !strings.HasPrefix(content, "---") {
		return fields
	}

	// Find closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return fields
	}
	frontmatter := rest[:idx]

	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			fields[key] = val
		}
	}

	return fields
}

// printEditSummary prints a human-readable summary of what changed.
func (c *EditCommand) printEditSummary(out io.Writer, original, modified string) {
	origFields := extractFrontmatterField(original)
	modFields := extractFrontmatterField(modified)

	var changes []string

	// Check frontmatter field changes
	allKeys := make(map[string]bool)
	for k := range origFields {
		allKeys[k] = true
	}
	for k := range modFields {
		allKeys[k] = true
	}

	for key := range allKeys {
		origVal := origFields[key]
		modVal := modFields[key]
		if origVal != modVal {
			if origVal == "" {
				changes = append(changes, fmt.Sprintf("%s: (added) %s", key, modVal))
			} else if modVal == "" {
				changes = append(changes, fmt.Sprintf("%s: (removed) %s", key, origVal))
			} else {
				changes = append(changes, fmt.Sprintf("%s: %s -> %s", key, origVal, modVal))
			}
		}
	}

	// Check body changes
	origBody := extractBody(original)
	modBody := extractBody(modified)
	if origBody != modBody {
		origLines := strings.Count(origBody, "\n")
		modLines := strings.Count(modBody, "\n")
		diff := modLines - origLines
		if diff > 0 {
			changes = append(changes, fmt.Sprintf("body: +%d lines", diff))
		} else if diff < 0 {
			changes = append(changes, fmt.Sprintf("body: %d lines", diff))
		} else {
			changes = append(changes, "body: modified")
		}
	}

	if len(changes) > 0 {
		fmt.Fprintf(out, "Updated: %s\n", strings.Join(changes, ", "))
	} else {
		fmt.Fprintln(out, "Updated (whitespace changes)")
	}
}

// extractBody extracts the markdown body after the YAML frontmatter.
func extractBody(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content
	}
	// Skip past "---\n"
	body := strings.TrimPrefix(rest[idx+4:], "\n")
	return body
}

// extractShortID extracts a short identifier from a path for temp file naming.
func extractShortID(pathOrID string) string {
	// If it's a full path like projects/brain-api/task/abc12def.md, extract abc12def
	parts := strings.Split(pathOrID, "/")
	last := parts[len(parts)-1]
	last = strings.TrimSuffix(last, ".md")
	if len(last) > 12 {
		return last[:12]
	}
	return last
}

// getEditAPIClient returns the API client, creating one from config if not injected.
func (c *EditCommand) getEditAPIClient() *runner.APIClient {
	if c.apiClient != nil {
		return c.apiClient
	}
	return runner.NewAPIClient(c.Config.Runner)
}

// =============================================================================
// Shared Helpers
// =============================================================================

// searchResultsToEntries converts SearchResult slice to BrainEntry slice
// for unified formatting output.
func searchResultsToEntries(results []types.SearchResult) []types.BrainEntry {
	entries := make([]types.BrainEntry, len(results))
	for i, r := range results {
		entries[i] = types.BrainEntry{
			ID:      r.ID,
			Path:    r.Path,
			Title:   r.Title,
			Type:    r.Type,
			Status:  r.Status,
			Content: r.Snippet,
		}
	}
	return entries
}

// isTerminal reports whether the writer is a terminal (TTY).
func isTerminal(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		stat, err := f.Stat()
		if err != nil {
			return false
		}
		return (stat.Mode() & os.ModeCharDevice) != 0
	}
	return false
}

// fzfSelect pipes entries through fzf for interactive selection.
func fzfSelect(entries []types.BrainEntry) ([]types.BrainEntry, error) {
	if len(entries) == 0 {
		return entries, nil
	}

	// Check fzf is available
	fzfPath, err := exec.LookPath("fzf")
	if err != nil {
		return nil, fmt.Errorf("fzf not found in PATH (install: https://github.com/junegunn/fzf)")
	}

	// Build lines: "path\tTitle [status]"
	var inputLines []string
	pathIndex := make(map[string]types.BrainEntry)
	for _, e := range entries {
		line := fmt.Sprintf("%s\t%s", e.Path, e.Title)
		if e.Status != "" {
			line += " [" + e.Status + "]"
		}
		inputLines = append(inputLines, line)
		pathIndex[e.Path] = e
	}

	input := strings.Join(inputLines, "\n")

	cmd := exec.Command(fzfPath, "--multi", "--delimiter=\t", "--with-nth=2..")
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		// fzf returns exit code 130 on Ctrl+C / Escape
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return nil, fmt.Errorf("cancelled")
		}
		return nil, fmt.Errorf("fzf: %w", err)
	}

	// Parse selected lines
	var selected []types.BrainEntry
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		// Extract path (first field before tab)
		parts := strings.SplitN(line, "\t", 2)
		path := parts[0]
		if e, ok := pathIndex[path]; ok {
			selected = append(selected, e)
		}
	}

	return selected, nil
}
