package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
}

// NewDreamViewer creates a new DreamViewer.
func NewDreamViewer() DreamViewer {
	return DreamViewer{}
}

// SetContent sets the dream content and resets the viewport.
func (d *DreamViewer) SetContent(content string) {
	d.content = content
	d.loading = false
	d.errMsg = ""
	d.fetched = true
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
