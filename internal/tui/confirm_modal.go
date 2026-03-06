package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// confirmResultMsg is sent when user confirms or cancels.
type confirmResultMsg struct {
	confirmed bool
	cancelled bool
}

// ConfirmModal is a modal for confirming an action.
type ConfirmModal struct {
	title     string
	message   string
	confirmed bool
	cancelled bool
	onConfirm func() tea.Msg
	onCancel  func() tea.Msg
}

// NewConfirmModal creates a new confirmation modal.
func NewConfirmModal(title, message string) *ConfirmModal {
	return &ConfirmModal{
		title:   title,
		message: message,
	}
}

// WithOnConfirm sets the callback to execute when confirmed.
func (m *ConfirmModal) WithOnConfirm(fn func() tea.Msg) *ConfirmModal {
	m.onConfirm = fn
	return m
}

// WithOnCancel sets the callback to execute when cancelled.
func (m *ConfirmModal) WithOnCancel(fn func() tea.Msg) *ConfirmModal {
	m.onCancel = fn
	return m
}

// Init implements Modal.
func (m *ConfirmModal) Init() tea.Cmd {
	return nil
}

// Update implements Modal.
func (m *ConfirmModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

// View implements Modal.
func (m *ConfirmModal) View() string {
	var b strings.Builder

	// Message
	b.WriteString(m.message)
	b.WriteString("\n\n")

	// Prompt
	b.WriteString("[y/n]")

	return b.String()
}

// HandleKey implements Modal.
func (m *ConfirmModal) HandleKey(key string) (bool, tea.Cmd) {
	switch strings.ToLower(key) {
	case "y":
		m.confirmed = true
		m.cancelled = false
		if m.onConfirm != nil {
			return true, func() tea.Msg {
				return m.onConfirm()
			}
		}
		return true, func() tea.Msg {
			return confirmResultMsg{confirmed: true}
		}
	case "n", "esc":
		m.cancelled = true
		m.confirmed = false
		if m.onCancel != nil {
			return true, func() tea.Msg {
				return m.onCancel()
			}
		}
		return true, func() tea.Msg {
			return confirmResultMsg{cancelled: true}
		}
	default:
		// Consume other keys to prevent passthrough
		return true, nil
	}
}

// Title implements Modal.
func (m *ConfirmModal) Title() string {
	return m.title
}

// Width implements Modal.
func (m *ConfirmModal) Width() int {
	// Calculate width based on message length
	maxLen := len(m.message)
	if maxLen < 40 {
		maxLen = 40
	}
	if maxLen > 80 {
		maxLen = 80
	}
	return maxLen
}

// Height implements Modal.
func (m *ConfirmModal) Height() int {
	// Title + message + prompt + padding
	lines := 1       // message
	lines += 2       // blank line + prompt
	return lines + 4 // padding
}
