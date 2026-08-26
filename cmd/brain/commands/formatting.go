package commands

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"text/template"

	"github.com/huynle/brain-api/internal/types"
)

// OutputConfig controls how brain entries are rendered to stdout.
type OutputConfig struct {
	Format    string // Named format or Go template string
	Delimiter string // Default: "\n", can be "\x00" for -0
	Quiet     bool   // Suppress counts/metadata
	NoColor   bool   // Disable color output

	// internal: set by -0 flag
	nulDelim bool
}

// RegisterFlags registers output formatting flags on the given FlagSet.
func (o *OutputConfig) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.Format, "format", "", "Output format: path, id, short, full, json, jsonl, or Go template")
	fs.BoolVar(&o.Quiet, "q", false, "Quiet mode: suppress count line")
	fs.BoolVar(&o.Quiet, "quiet", false, "Quiet mode: suppress count line")
	fs.BoolVar(&o.NoColor, "no-color", false, "Disable color output")

	// -0 sets NUL delimiter directly
	fs.BoolFunc("0", "Use NUL character as delimiter (for xargs -0)", func(_ string) error {
		o.nulDelim = true
		o.Delimiter = "\x00"
		return nil
	})

	// Set default delimiter
	o.Delimiter = "\n"
}

// resolveDelimiter updates Delimiter based on the -0 flag.
// Call after flag parsing.
func (o *OutputConfig) resolveDelimiter() {
	if o.nulDelim {
		o.Delimiter = "\x00"
	}
}

// IsCustomTemplate returns true if Format contains a Go template (has "{{").
func (o *OutputConfig) IsCustomTemplate() bool {
	return strings.Contains(o.Format, "{{")
}

// DetectDefaultFormat returns the appropriate default format based on TTY status.
// If Format is already set, returns it unchanged.
func (o *OutputConfig) DetectDefaultFormat(isTTY bool) string {
	o.resolveDelimiter()

	if o.Format != "" {
		return o.Format
	}
	if isTTY {
		return "short"
	}
	return "path"
}

// FormatEntry formats a single BrainEntry according to the configured format.
func (o *OutputConfig) FormatEntry(entry types.BrainEntry) string {
	switch o.Format {
	case "path":
		return entry.Path
	case "id":
		return entry.ID
	case "short":
		return formatShort(entry)
	case "full":
		return formatFull(entry)
	case "json", "jsonl":
		return formatJSON(entry)
	default:
		if o.IsCustomTemplate() {
			return formatTemplate(o.Format, entry)
		}
		// Unknown named format: fall back to path
		return entry.Path
	}
}

// FormatEntries formats multiple BrainEntry values according to the configured format.
func (o *OutputConfig) FormatEntries(entries []types.BrainEntry) string {
	if entries == nil {
		entries = []types.BrainEntry{}
	}

	switch o.Format {
	case "json":
		return formatJSONArray(entries)
	case "jsonl":
		return formatJSONLines(entries, o.Delimiter)
	default:
		if len(entries) == 0 {
			return ""
		}
		parts := make([]string, len(entries))
		for i, e := range entries {
			parts[i] = o.FormatEntry(e)
		}
		return strings.Join(parts, o.Delimiter)
	}
}

// formatShort renders a compact one-line summary.
func formatShort(entry types.BrainEntry) string {
	var parts []string
	parts = append(parts, entry.Title)
	parts = append(parts, entry.Path)
	if entry.Status != "" {
		parts = append(parts, "["+entry.Status+"]")
	}
	if entry.Priority != "" {
		parts = append(parts, "("+entry.Priority+")")
	}
	return strings.Join(parts, "  ")
}

// formatFull renders frontmatter-style metadata and body content.
func formatFull(entry types.BrainEntry) string {
	var buf strings.Builder
	buf.WriteString("---\n")
	buf.WriteString(fmt.Sprintf("title: %s\n", entry.Title))
	buf.WriteString(fmt.Sprintf("type: %s\n", entry.Type))
	if entry.Status != "" {
		buf.WriteString(fmt.Sprintf("status: %s\n", entry.Status))
	}
	if entry.Priority != "" {
		buf.WriteString(fmt.Sprintf("priority: %s\n", entry.Priority))
	}
	if len(entry.Tags) > 0 {
		buf.WriteString(fmt.Sprintf("tags: [%s]\n", strings.Join(entry.Tags, ", ")))
	}
	buf.WriteString(fmt.Sprintf("path: %s\n", entry.Path))
	buf.WriteString(fmt.Sprintf("id: %s\n", entry.ID))
	buf.WriteString("---\n")
	if entry.Content != "" {
		buf.WriteString(entry.Content)
	}
	return buf.String()
}

// formatJSON marshals a single entry to compact JSON.
func formatJSON(entry types.BrainEntry) string {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Sprintf("{\"error\": %q}", err.Error())
	}
	return string(data)
}

// formatJSONArray marshals a slice of entries to a JSON array.
func formatJSONArray(entries []types.BrainEntry) string {
	if len(entries) == 0 {
		return "[]"
	}
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Sprintf("{\"error\": %q}", err.Error())
	}
	return string(data)
}

// formatJSONLines renders one JSON object per line.
func formatJSONLines(entries []types.BrainEntry, delimiter string) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = formatJSON(e)
	}
	return strings.Join(lines, delimiter)
}

// formatTemplate executes a Go text/template against a BrainEntry.
func formatTemplate(tmplStr string, entry types.BrainEntry) string {
	tmpl, err := template.New("entry").Parse(tmplStr)
	if err != nil {
		return fmt.Sprintf("template error: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, entry); err != nil {
		return fmt.Sprintf("template error: %v", err)
	}
	return buf.String()
}
