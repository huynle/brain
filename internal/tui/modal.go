package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Modal is the interface that modal implementations must satisfy.
type Modal interface {
	// Init initializes the modal and returns an optional command.
	Init() tea.Cmd

	// Update handles messages and returns the updated modal and optional command.
	Update(msg tea.Msg) (Modal, tea.Cmd)

	// View renders the modal content (without border/overlay).
	View() string

	// HandleKey handles a key press and returns whether it was handled and an optional command.
	HandleKey(key string) (handled bool, cmd tea.Cmd)

	// Title returns the modal title for display in the border.
	Title() string

	// Width returns the desired width of the modal content area.
	Width() int

	// Height returns the desired height of the modal content area.
	Height() int
}

// DestructiveModal is an optional interface that modals can implement
// to indicate they represent a destructive action (renders with red border).
type DestructiveModal interface {
	IsDestructive() bool
}

// MouseModal is an optional interface that modals can implement
// to receive mouse events (clicks on tabs, field rows, etc.).
type MouseModal interface {
	// HandleMouse handles a mouse event within the modal content area.
	// x, y are relative to the modal content origin (0,0 = top-left of content).
	// Returns true if the event was handled, and an optional command.
	HandleMouse(msg tea.MouseMsg, x, y int) (handled bool, cmd tea.Cmd)
}

// ModalManager manages modal lifecycle and rendering.
type ModalManager struct {
	activeModal  Modal
	stack        []Modal
	scrollOffset int // vertical scroll offset when content overflows
	contentLines int // total content lines from last render (for scroll bounds)
	viewportH    int // available viewport height from last render
}

// SettingsChangedMsg is sent when the Settings modal closes
// to trigger UI refresh with updated settings.
type SettingsChangedMsg struct{}

// NewModalManager creates a new ModalManager.
func NewModalManager() ModalManager {
	return ModalManager{
		stack: []Modal{},
	}
}

// Open opens a modal and calls its Init method.
// If a modal is already open, the current modal is pushed to the stack.
func (m *ModalManager) Open(modal Modal) tea.Cmd {
	// Push current modal to stack if one exists
	if m.activeModal != nil {
		m.stack = append(m.stack, m.activeModal)
	}

	// Set new modal as active and reset scroll
	m.activeModal = modal
	m.scrollOffset = 0
	m.contentLines = 0
	m.viewportH = 0

	// Initialize the modal
	return modal.Init()
}

// ScrollDown scrolls the modal content down by one line.
func (m *ModalManager) ScrollDown() {
	maxOffset := m.contentLines - m.viewportH
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset < maxOffset {
		m.scrollOffset++
	}
}

// ScrollUp scrolls the modal content up by one line.
func (m *ModalManager) ScrollUp() {
	if m.scrollOffset > 0 {
		m.scrollOffset--
	}
}

// NeedsScroll returns true if the modal content exceeds the viewport.
func (m *ModalManager) NeedsScroll() bool {
	return m.contentLines > m.viewportH && m.viewportH > 0
}

// Close closes the active modal.
// If there are modals in the stack, pops the top one.
func (m *ModalManager) Close() tea.Cmd {
	if m.activeModal == nil {
		return nil
	}

	// Check if Settings modal is closing to trigger UI refresh
	wasSettingsModal := false
	if _, ok := m.activeModal.(*SettingsModal); ok {
		wasSettingsModal = true
	}

	// Check if there's a modal in the stack to restore
	if len(m.stack) > 0 {
		// Pop the last modal from stack
		m.activeModal = m.stack[len(m.stack)-1]
		m.stack = m.stack[:len(m.stack)-1]
	} else {
		// No more modals, clear active
		m.activeModal = nil
	}

	// Send SettingsChangedMsg if Settings modal was closed
	if wasSettingsModal {
		return func() tea.Msg {
			return SettingsChangedMsg{}
		}
	}

	return nil
}

// IsOpen returns true if a modal is currently open.
func (m *ModalManager) IsOpen() bool {
	return m.activeModal != nil
}

// ActiveModal returns the currently active modal, or nil when none is open.
func (m *ModalManager) ActiveModal() Modal {
	return m.activeModal
}

// Update routes messages to the active modal.
func (m ModalManager) Update(msg tea.Msg) (ModalManager, tea.Cmd) {
	if m.activeModal == nil {
		return m, nil
	}

	// Route message to active modal
	newModal, cmd := m.activeModal.Update(msg)
	m.activeModal = newModal

	return m, cmd
}

// HandleMouse routes mouse events to the active modal if it implements MouseModal.
// screenW, screenH are the terminal dimensions needed to compute content-relative coordinates.
// Returns true if the event was handled.
func (m *ModalManager) HandleMouse(msg tea.MouseMsg, screenW, screenH int) (bool, tea.Cmd) {
	if m.activeModal == nil {
		return false, nil
	}
	if msg.Type == tea.MouseWheelDown {
		return m.activeModal.HandleKey("j")
	}
	if msg.Type == tea.MouseWheelUp {
		return m.activeModal.HandleKey("k")
	}

	// Check if the modal supports mouse events
	mm, ok := m.activeModal.(MouseModal)
	if !ok {
		return false, nil
	}

	// Only handle left clicks
	if msg.Type != tea.MouseLeft {
		return false, nil
	}

	// Compute modal content area origin on screen.
	// The modal is centered in the terminal with:
	//   border: 1 cell each side
	//   padding: 2 cells each side (horizontal), 1 cell each side (vertical)
	//   title: rendered above content (title line + blank line = 2 lines)
	modalW := m.activeModal.Width()
	maxW := screenW - 6 - 2
	if modalW > maxW {
		modalW = maxW
	}
	if modalW < 20 {
		modalW = 20
	}

	// Total box width = content width + border (2) + horizontal padding (4)
	boxW := modalW + 6

	// Compute visible content height (same as in View). Do not rely on
	// m.contentLines being populated by View: Model.View has a value receiver, so
	// render-time ModalManager mutations may be lost before mouse hit testing.
	contentLines := countRenderedLines(m.activeModal.View())
	m.contentLines = contentLines

	title := m.activeModal.Title()
	titleLines := 0
	if title != "" {
		titleLines = 2 // title + blank line
	}
	overhead := 4 + titleLines // border (2) + vertical padding (2) + title
	maxContentH := screenH - overhead
	if maxContentH < 3 {
		maxContentH = 3
	}
	m.viewportH = maxContentH
	visibleContentH := contentLines
	if visibleContentH > maxContentH {
		visibleContentH = maxContentH
	}

	// Total box height = visible content + title + border (2) + vertical padding (2)
	boxH := visibleContentH + titleLines + 4

	// Modal box is centered on screen
	boxX := (screenW - boxW) / 2
	boxY := (screenH - boxH) / 2

	// Content origin inside the box: border (1) + padding (2) horizontally, border (1) + padding (1) + title vertically
	contentX := boxX + 3              // border(1) + padding(2)
	contentY := boxY + 2 + titleLines // border(1) + padding(1) + title lines

	// Convert screen coords to content-relative coords
	relX := msg.X - contentX
	relY := msg.Y - contentY + m.scrollOffset // account for scroll

	// Check if click is within the modal box at all
	if msg.X < boxX || msg.X >= boxX+boxW || msg.Y < boxY || msg.Y >= boxY+boxH {
		return false, nil
	}

	return mm.HandleMouse(msg, relX, relY)
}

func countRenderedLines(content string) int {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return len(lines)
}

// HandleKey routes key presses to the active modal.
// Returns true if the key was handled, false otherwise.
// Esc key is routed to the modal first; if unhandled, closes the modal.
func (m *ModalManager) HandleKey(key string) (bool, tea.Cmd) {
	if m.activeModal == nil {
		return false, nil
	}

	// Handle scroll keys when content overflows (before routing to modal)
	if m.NeedsScroll() {
		switch key {
		case "ctrl+d":
			// Half-page down
			for i := 0; i < m.viewportH/2; i++ {
				m.ScrollDown()
			}
			return true, nil
		case "ctrl+u":
			// Half-page up
			for i := 0; i < m.viewportH/2; i++ {
				m.ScrollUp()
			}
			return true, nil
		}
	}

	// Route key to modal first (including Esc)
	handled, cmd := m.activeModal.HandleKey(key)
	if handled {
		return true, cmd
	}

	// If modal didn't handle Esc, close the modal
	if key == "esc" {
		return true, m.Close()
	}

	return false, nil
}

// View renders the modal overlay.
// Returns empty string if no modal is open.
func (m *ModalManager) View(width, height int) string {
	if m.activeModal == nil {
		return ""
	}

	// Get modal content (without title — title is rendered separately and pinned)
	content := m.activeModal.View()

	// Determine border color based on destructive flag
	borderColor := ColorCyan
	if dm, ok := m.activeModal.(DestructiveModal); ok && dm.IsDestructive() {
		borderColor = ColorBlocked // red
	}

	// Render title separately (pinned, never scrolled)
	title := m.activeModal.Title()
	titleRendered := ""
	titleLines := 0
	if title != "" {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(borderColor)
		titleRendered = titleStyle.Render(title) + "\n\n"
		titleLines = 2 // title + blank line
	}

	// Overhead: border (2) + padding (2) + title lines
	overhead := 4 + titleLines

	// Calculate available viewport for scrollable content
	maxContentH := height - overhead
	if maxContentH < 3 {
		maxContentH = 3 // minimum usable height
	}

	// Split content into lines and track dimensions
	contentLineSlice := strings.Split(content, "\n")
	// Remove trailing empty line from split
	if len(contentLineSlice) > 0 && contentLineSlice[len(contentLineSlice)-1] == "" {
		contentLineSlice = contentLineSlice[:len(contentLineSlice)-1]
	}
	m.contentLines = len(contentLineSlice)
	m.viewportH = maxContentH

	// Auto-scroll to keep focused item visible.
	// Scan for the → indicator to find the focused line.
	if m.contentLines > maxContentH {
		focusedLine := -1
		for i, line := range contentLineSlice {
			if strings.Contains(line, "→") {
				focusedLine = i
				break
			}
		}

		if focusedLine >= 0 {
			// Reserve 1 line for scroll indicator
			viewH := maxContentH - 1
			if viewH < 1 {
				viewH = 1
			}

			// If focused line is below the viewport, scroll down
			if focusedLine >= m.scrollOffset+viewH {
				m.scrollOffset = focusedLine - viewH + 1
			}
			// If focused line is above the viewport, scroll up
			if focusedLine < m.scrollOffset {
				m.scrollOffset = focusedLine
			}
		}
	}

	// Clamp scroll offset
	maxOffset := m.contentLines - maxContentH
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}

	// Apply viewport: slice content lines if they overflow
	if m.contentLines > maxContentH {
		// Reserve 1 line for scroll indicator
		viewH := maxContentH - 1
		if viewH < 1 {
			viewH = 1
		}

		end := m.scrollOffset + viewH
		if end > m.contentLines {
			end = m.contentLines
		}

		visibleLines := contentLineSlice[m.scrollOffset:end]

		// Build scroll indicator
		scrollInfo := ""
		if m.scrollOffset > 0 && end < m.contentLines {
			scrollInfo = lipgloss.NewStyle().Foreground(ColorDim).Render("▲ ▼ scroll")
		} else if m.scrollOffset > 0 {
			scrollInfo = lipgloss.NewStyle().Foreground(ColorDim).Render("▲ scroll up")
		} else {
			remaining := m.contentLines - end
			scrollInfo = lipgloss.NewStyle().Foreground(ColorDim).Render(
				"▼ " + strings.Repeat("·", min(remaining, 10)) + " more")
		}

		content = strings.Join(visibleLines, "\n") + "\n" + scrollInfo
	}

	// Combine pinned title with scrollable content
	content = titleRendered + content

	// Determine fixed modal width from the modal's declared width,
	// clamped to terminal bounds. This prevents the box from resizing
	// as content scrolls in and out of view.
	modalW := m.activeModal.Width()
	maxW := width - 6 - 2 // subtract horizontal overhead (border+padding) and margin
	if modalW > maxW {
		modalW = maxW
	}
	if modalW < 20 {
		modalW = 20
	}

	// Apply modal styling with border and fixed width
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 2).
		Width(modalW).      // fixed content width — box won't resize on scroll
		MaxWidth(width - 2) // hard clamp for terminal edges

	// Render modal with border
	modal := modalStyle.Render(content)

	// Center modal in terminal
	centered := lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)

	// Create dimmed background overlay
	dimStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("0")).
		Foreground(lipgloss.Color("8"))

	// Overlay the centered modal on the dimmed background
	return lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		dimStyle.Width(width).Height(height).Render(centered),
	)
}
