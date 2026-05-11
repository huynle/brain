package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

// EntryTree displays brain entries grouped by directory for the active project.
type EntryTree struct {
	entries []types.BrainEntry
	visible []entryTreeRow
	cursor  int
	offset  int
	width   int
	height  int
}

type entryTreeRow struct {
	entry *types.BrainEntry
	label string
	depth int
	isDir bool
}

// NewEntryTree creates an entry tree component.
func NewEntryTree() EntryTree {
	return EntryTree{}
}

// SetEntries replaces the displayed entries and rebuilds the flattened tree.
func (t *EntryTree) SetEntries(entries []types.BrainEntry) {
	t.entries = append([]types.BrainEntry(nil), entries...)
	sort.Slice(t.entries, func(i, j int) bool { return t.entries[i].Path < t.entries[j].Path })
	t.rebuildRows()
	t.clampCursor()
}

// SetSize updates viewport dimensions.
func (t *EntryTree) SetSize(width, height int) {
	t.width = width
	t.height = height
	t.ensureCursorVisible()
}

// MoveDown moves selection to the next entry.
func (t *EntryTree) MoveDown() {
	for i := t.cursor + 1; i < len(t.visible); i++ {
		if t.visible[i].entry != nil {
			t.cursor = i
			t.ensureCursorVisible()
			return
		}
	}
}

// MoveUp moves selection to the previous entry.
func (t *EntryTree) MoveUp() {
	for i := t.cursor - 1; i >= 0; i-- {
		if t.visible[i].entry != nil {
			t.cursor = i
			t.ensureCursorVisible()
			return
		}
	}
}

// GotoTop selects the first entry.
func (t *EntryTree) GotoTop() {
	for i := range t.visible {
		if t.visible[i].entry != nil {
			t.cursor = i
			t.ensureCursorVisible()
			return
		}
	}
}

// GotoBottom selects the last entry.
func (t *EntryTree) GotoBottom() {
	for i := len(t.visible) - 1; i >= 0; i-- {
		if t.visible[i].entry != nil {
			t.cursor = i
			t.ensureCursorVisible()
			return
		}
	}
}

// SelectVisibleLine selects an entry by rendered content line, excluding the title line.
func (t *EntryTree) SelectVisibleLine(line int) bool {
	row := t.offset + line - 1 // title line is above the flattened rows
	if row < 0 || row >= len(t.visible) || t.visible[row].entry == nil {
		return false
	}
	t.cursor = row
	t.ensureCursorVisible()
	return true
}

// SelectedEntry returns the selected entry, if any.
func (t EntryTree) SelectedEntry() *types.BrainEntry {
	if t.cursor < 0 || t.cursor >= len(t.visible) {
		return nil
	}
	return t.visible[t.cursor].entry
}

// View renders the entry tree.
func (t *EntryTree) View(width, height int) string {
	t.SetSize(width, height)
	if width < 10 {
		width = 10
	}
	if height < 1 {
		height = 1
	}

	var lines []string
	lines = append(lines, TitleStyle.Render(fmt.Sprintf("Brain Entries (%d)", len(t.entries))))
	if len(t.entries) == 0 {
		lines = append(lines, DimStyle.Render("No entries found"))
		return strings.Join(lines, "\n")
	}

	t.ensureCursorVisible()
	rowHeight := height - 1
	if rowHeight < 0 {
		rowHeight = 0
	}
	end := t.offset + rowHeight
	if end > len(t.visible) {
		end = len(t.visible)
	}
	for i := t.offset; i < end; i++ {
		row := t.visible[i]
		line := t.renderRow(row)
		if i == t.cursor && row.entry != nil {
			line = SelectedRowStyle.Width(width).Render(line)
		}
		lines = append(lines, line)
	}

	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func (t *EntryTree) rebuildRows() {
	t.visible = nil
	seenDirs := map[string]bool{}
	for i := range t.entries {
		entry := &t.entries[i]
		parts := strings.Split(filepath.ToSlash(entry.Path), "/")
		if len(parts) >= 4 && parts[0] == "projects" {
			parts = parts[2:]
		}
		if len(parts) > 1 {
			dir := strings.Join(parts[:len(parts)-1], "/")
			if !seenDirs[dir] {
				seenDirs[dir] = true
				t.visible = append(t.visible, entryTreeRow{label: dir + "/", depth: 0, isDir: true})
			}
		}
		t.visible = append(t.visible, entryTreeRow{entry: entry, label: t.entryLabel(*entry), depth: 1})
	}
}

func (t *EntryTree) entryLabel(entry types.BrainEntry) string {
	label := entry.Title
	if label == "" {
		label = filepath.Base(entry.Path)
	}
	if entry.Type != "" {
		label = fmt.Sprintf("%s [%s]", label, entry.Type)
	}
	if status := embeddingWarning(entry.EmbeddingStatus); status != "" {
		label = fmt.Sprintf("%s  %s", label, status)
	}
	return label
}

func (t EntryTree) renderRow(row entryTreeRow) string {
	indent := strings.Repeat("  ", row.depth)
	if row.isDir {
		return indent + lipgloss.NewStyle().Foreground(ColorCyan).Render("▾ "+row.label)
	}
	return indent + "├─ " + row.label
}

func (t *EntryTree) clampCursor() {
	if len(t.visible) == 0 {
		t.cursor = 0
		t.offset = 0
		return
	}
	if t.cursor >= len(t.visible) {
		t.cursor = len(t.visible) - 1
	}
	if t.visible[t.cursor].entry == nil {
		t.GotoTop()
	}
	t.ensureCursorVisible()
}

func (t *EntryTree) ensureCursorVisible() {
	rowHeight := t.height - 1
	if rowHeight <= 0 {
		t.offset = 0
		return
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+rowHeight {
		t.offset = t.cursor - rowHeight + 1
	}
	maxOffset := len(t.visible) - rowHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

func embeddingWarning(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "missing", "not_embedded", "not-embedded", "none":
		return DimStyle.Render("embed:missing")
	case "stale", "needs_embedding", "needs-embedding", "outdated":
		return DimStyle.Render("embed:stale")
	case "unknown":
		return DimStyle.Render("embed:unknown")
	default:
		return ""
	}
}
