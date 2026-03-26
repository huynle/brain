package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// MaxVisibleSessions is the maximum number of sessions shown in the modal.
const MaxVisibleSessions = 8

// maxSessionIDLen is the maximum display length for session IDs.
const maxSessionIDLen = 30

// SessionSelectModal displays a list of sessions for selection.
type SessionSelectModal struct {
	sessionIDs    []string
	selectedIndex int
	tmuxMode      bool
	onSelect      func(sessionID string) tea.Msg
	width         int
	height        int
}

// NewSessionSelectModal creates a new session selection modal.
// sessionIDs should be pre-sorted by timestamp descending (latest first).
func NewSessionSelectModal(sessionIDs []string, tmuxMode bool, onSelect func(string) tea.Msg) *SessionSelectModal {
	return &SessionSelectModal{
		sessionIDs:    sessionIDs,
		selectedIndex: 0,
		tmuxMode:      tmuxMode,
		onSelect:      onSelect,
	}
}

// Init implements Modal.
func (m *SessionSelectModal) Init() tea.Cmd {
	return nil
}

// Update implements Modal.
func (m *SessionSelectModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

// View implements Modal.
func (m *SessionSelectModal) View() string {
	var b strings.Builder

	// Header with session count
	count := len(m.sessionIDs)
	noun := "sessions"
	if count == 1 {
		noun = "session"
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorCyan)
	b.WriteString(headerStyle.Render(fmt.Sprintf("%d %s", count, noun)))
	b.WriteString("\n\n")

	// Session list (max MaxVisibleSessions)
	visible := count
	if visible > MaxVisibleSessions {
		visible = MaxVisibleSessions
	}

	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorDim)

	latestStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true)

	for i := 0; i < visible; i++ {
		id := m.sessionIDs[i]

		// Truncate long IDs
		displayID := id
		if len(displayID) > maxSessionIDLen {
			displayID = displayID[:maxSessionIDLen] + "..."
		}

		// Add "(latest)" label to first session
		suffix := ""
		if i == 0 {
			suffix = " " + latestStyle.Render("(latest)")
		}

		// Render selected vs unselected
		if i == m.selectedIndex {
			b.WriteString(selectedStyle.Render("> "+displayID) + suffix)
		} else {
			b.WriteString(dimStyle.Render("  "+displayID) + suffix)
		}
		b.WriteString("\n")
	}

	// Overflow indicator
	if count > MaxVisibleSessions {
		overflow := count - MaxVisibleSessions
		b.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more", overflow)))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true)
	b.WriteString(footerStyle.Render("j/k: Navigate  Enter: Open  Esc: Cancel"))

	return b.String()
}

// HandleKey implements Modal.
func (m *SessionSelectModal) HandleKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		m.selectedIndex++
		if m.selectedIndex >= len(m.sessionIDs) {
			m.selectedIndex = 0 // wrap
		}
		return true, nil

	case "k", "up":
		m.selectedIndex--
		if m.selectedIndex < 0 {
			m.selectedIndex = len(m.sessionIDs) - 1 // wrap
		}
		return true, nil

	case "enter", "return":
		if m.selectedIndex >= 0 && m.selectedIndex < len(m.sessionIDs) {
			selectedID := m.sessionIDs[m.selectedIndex]
			return true, func() tea.Msg {
				return m.onSelect(selectedID)
			}
		}
		return true, nil

	case "esc":
		// ModalManager handles close on esc
		return true, nil

	default:
		// Consume all keys while modal is open
		return true, nil
	}
}

// Title implements Modal.
func (m *SessionSelectModal) Title() string {
	return "Select Session"
}

// Width implements Modal.
func (m *SessionSelectModal) Width() int {
	return 40
}

// Height implements Modal.
func (m *SessionSelectModal) Height() int {
	visible := len(m.sessionIDs)
	if visible > MaxVisibleSessions {
		visible = MaxVisibleSessions
	}
	// visible sessions + title line + blank + footer line + border padding
	return visible + 4
}
