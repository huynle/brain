package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DreamSearchMode tracks the search input state.
type DreamSearchMode int

const (
	DreamSearchOff    DreamSearchMode = iota // No search active
	DreamSearchTyping                        // User is typing a search query
	DreamSearchLocked                        // Search is locked, n/N to navigate
)

// DreamViewer displays dream content in a scrollable viewport.
type DreamViewer struct {
	content  string
	viewport viewport.Model
	loading  bool
	errMsg   string
	ready    bool
	width    int
	height   int
	fetched  bool // true if content has been fetched at least once

	// Search state
	searchMode    DreamSearchMode
	searchQuery   string
	matchLines    []int    // line numbers (0-indexed) that contain matches
	currentMatch  int      // index into matchLines
	originalLines []string // raw content lines (no highlighting)
}

// NewDreamViewer creates a new DreamViewer.
func NewDreamViewer() DreamViewer {
	return DreamViewer{}
}

// SetContent sets the dream content and resets the viewport.
func (d *DreamViewer) SetContent(content string) {
	d.content = content
	d.originalLines = strings.Split(content, "\n")
	d.loading = false
	d.errMsg = ""
	d.fetched = true
	// Reset search state
	d.searchMode = DreamSearchOff
	d.searchQuery = ""
	d.matchLines = nil
	d.currentMatch = 0
	if d.ready {
		d.viewport.SetContent(content)
		d.viewport.GotoTop()
	}
}

// SetSize updates the viewport dimensions.
func (d *DreamViewer) SetSize(w, h int) {
	d.width = w
	d.height = h
	if !d.ready {
		d.viewport = viewport.New(w, h)
		d.viewport.SetContent(d.content)
		d.ready = true
	} else {
		d.viewport.Width = w
		d.viewport.Height = h
	}
}

// GotoTop scrolls to the top of the content.
func (d *DreamViewer) GotoTop() {
	if d.ready {
		d.viewport.GotoTop()
	}
}

// GotoBottom scrolls to the bottom of the content.
func (d *DreamViewer) GotoBottom() {
	if d.ready {
		d.viewport.GotoBottom()
	}
}

// ScrollUp scrolls the viewport up by n lines.
func (d *DreamViewer) ScrollUp(n int) {
	if d.ready {
		d.viewport.LineUp(n)
	}
}

// ScrollDown scrolls the viewport down by n lines.
func (d *DreamViewer) ScrollDown(n int) {
	if d.ready {
		d.viewport.LineDown(n)
	}
}

// SearchMode returns the current search mode.
func (d *DreamViewer) SearchMode() DreamSearchMode {
	return d.searchMode
}

// SearchQuery returns the current search query.
func (d *DreamViewer) SearchQuery() string {
	return d.searchQuery
}

// MatchCount returns the number of search matches.
func (d *DreamViewer) MatchCount() int {
	return len(d.matchLines)
}

// CurrentMatchIndex returns the 1-based index of the current match.
func (d *DreamViewer) CurrentMatchIndex() int {
	if len(d.matchLines) == 0 {
		return 0
	}
	return d.currentMatch + 1
}

// StartSearch enters search typing mode.
func (d *DreamViewer) StartSearch() {
	d.searchMode = DreamSearchTyping
	d.searchQuery = ""
	d.matchLines = nil
	d.currentMatch = 0
	// Restore original content (remove previous highlights)
	if d.ready && len(d.originalLines) > 0 {
		d.viewport.SetContent(strings.Join(d.originalLines, "\n"))
	}
}

// CancelSearch exits search mode and restores original content.
func (d *DreamViewer) CancelSearch() {
	d.searchMode = DreamSearchOff
	d.searchQuery = ""
	d.matchLines = nil
	d.currentMatch = 0
	if d.ready && len(d.originalLines) > 0 {
		d.viewport.SetContent(strings.Join(d.originalLines, "\n"))
	}
}

// SetSearchQuery updates the search query and refreshes highlights.
func (d *DreamViewer) SetSearchQuery(query string) {
	d.searchQuery = query
	d.applySearchHighlight()
}

// LockSearch locks the current search for n/N navigation.
func (d *DreamViewer) LockSearch() {
	if d.searchQuery != "" && len(d.matchLines) > 0 {
		d.searchMode = DreamSearchLocked
		d.scrollToCurrentMatch()
	} else if d.searchQuery == "" {
		d.CancelSearch()
	} else {
		// Query set but no matches
		d.searchMode = DreamSearchLocked
	}
}

// NextMatch jumps to the next search match.
func (d *DreamViewer) NextMatch() {
	if len(d.matchLines) == 0 {
		return
	}
	d.currentMatch = (d.currentMatch + 1) % len(d.matchLines)
	d.scrollToCurrentMatch()
}

// PrevMatch jumps to the previous search match.
func (d *DreamViewer) PrevMatch() {
	if len(d.matchLines) == 0 {
		return
	}
	d.currentMatch--
	if d.currentMatch < 0 {
		d.currentMatch = len(d.matchLines) - 1
	}
	d.scrollToCurrentMatch()
}

// applySearchHighlight highlights matches in the viewport content.
func (d *DreamViewer) applySearchHighlight() {
	if !d.ready || len(d.originalLines) == 0 {
		return
	}

	query := d.searchQuery
	if query == "" {
		d.matchLines = nil
		d.currentMatch = 0
		d.viewport.SetContent(strings.Join(d.originalLines, "\n"))
		return
	}

	queryLower := strings.ToLower(query)
	d.matchLines = nil
	d.currentMatch = 0

	highlightStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("226")). // Yellow background
		Foreground(lipgloss.Color("0")).   // Black text
		Bold(true)

	highlighted := make([]string, len(d.originalLines))
	for i, line := range d.originalLines {
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, queryLower) {
			d.matchLines = append(d.matchLines, i)
			// Highlight all occurrences in this line (case-insensitive)
			highlighted[i] = highlightMatches(line, query, highlightStyle)
		} else {
			highlighted[i] = line
		}
	}

	d.viewport.SetContent(strings.Join(highlighted, "\n"))

	// Auto-scroll to first match during typing
	if len(d.matchLines) > 0 && d.searchMode == DreamSearchTyping {
		d.scrollToCurrentMatch()
	}
}

// highlightMatches highlights all case-insensitive occurrences of query in line.
func highlightMatches(line, query string, style lipgloss.Style) string {
	if query == "" {
		return line
	}

	lineLower := strings.ToLower(line)
	queryLower := strings.ToLower(query)

	var result strings.Builder
	pos := 0
	for {
		idx := strings.Index(lineLower[pos:], queryLower)
		if idx == -1 {
			result.WriteString(line[pos:])
			break
		}
		// Write text before match
		result.WriteString(line[pos : pos+idx])
		// Write highlighted match (preserve original case)
		matchText := line[pos+idx : pos+idx+len(query)]
		result.WriteString(style.Render(matchText))
		pos += idx + len(query)
	}
	return result.String()
}

// scrollToCurrentMatch scrolls the viewport so the current match is visible.
func (d *DreamViewer) scrollToCurrentMatch() {
	if !d.ready || len(d.matchLines) == 0 {
		return
	}
	targetLine := d.matchLines[d.currentMatch]
	// Center the match in the viewport
	halfHeight := d.viewport.Height / 2
	scrollTo := targetLine - halfHeight
	if scrollTo < 0 {
		scrollTo = 0
	}
	d.viewport.SetYOffset(scrollTo)
}

// SetLoading sets the loading state.
func (d *DreamViewer) SetLoading(loading bool) {
	d.loading = loading
	if loading {
		d.errMsg = ""
	}
}

// SetError sets the error message.
func (d *DreamViewer) SetError(msg string) {
	d.errMsg = msg
	d.loading = false
	d.fetched = true
}

// HasContent returns true if dream content has been fetched.
func (d *DreamViewer) HasContent() bool {
	return d.fetched
}

// Update delegates key messages to the viewport for scrolling.
func (d *DreamViewer) Update(msg tea.Msg) tea.Cmd {
	if !d.ready {
		return nil
	}
	var cmd tea.Cmd
	d.viewport, cmd = d.viewport.Update(msg)
	return cmd
}

// View renders the dream viewer panel.
// width and height are the available dimensions (excluding border).
func (d *DreamViewer) View(width, height int) string {
	if width != d.width || height != d.height {
		d.SetSize(width, height)
	}

	var content string

	switch {
	case d.loading:
		content = d.centeredMessage(width, height, "⏳ Fetching dream...")
	case d.errMsg != "":
		content = d.centeredMessage(width, height,
			fmt.Sprintf("❌ %s", d.errMsg))
	case !d.fetched:
		content = d.centeredMessage(width, height,
			"Switch to Dream tab to load content")
	case d.content == "":
		content = d.centeredMessage(width, height,
			"No dream found.\n\nRun: brain dream <project> --now")
	default:
		// Render scrollable viewport
		if d.ready {
			content = d.viewport.View()
		}
	}

	return content
}

// centeredMessage renders a centered message within the given dimensions.
func (d *DreamViewer) centeredMessage(width, height int, msg string) string {
	style := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Foreground(ColorDim)

	// Split multi-line messages
	lines := strings.Split(msg, "\n")
	rendered := strings.Join(lines, "\n")

	return style.Render(rendered)
}
