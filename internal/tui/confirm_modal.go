package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// MaxVisibleTasks is the maximum number of task titles to display before truncating.
	MaxVisibleTasks = 5
	// MaxTitleLength is the maximum length for task title display.
	MaxTitleLength = 40
)

// confirmResultMsg is sent when user confirms or cancels.
type confirmResultMsg struct {
	confirmed bool
	cancelled bool
}

// ConfirmModal is a modal for confirming an action.
type ConfirmModal struct {
	title       string
	message     string
	taskTitles  []string
	featureID   string
	destructive bool
	confirmed   bool
	cancelled   bool
	onConfirm   func() tea.Msg
	onCancel    func() tea.Msg
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

// WithTaskTitles sets the list of task titles to display in the modal.
func (m *ConfirmModal) WithTaskTitles(titles []string) *ConfirmModal {
	m.taskTitles = titles
	return m
}

// WithFeatureID sets the feature ID context for the modal.
func (m *ConfirmModal) WithFeatureID(id string) *ConfirmModal {
	m.featureID = id
	return m
}

// WithDestructive marks the modal as a destructive action (red border).
func (m *ConfirmModal) WithDestructive(destructive bool) *ConfirmModal {
	m.destructive = destructive
	return m
}

// IsDestructive returns whether this modal represents a destructive action.
func (m *ConfirmModal) IsDestructive() bool {
	return m.destructive
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

	// Feature ID context
	if m.featureID != "" {
		dimStyle := lipgloss.NewStyle().Foreground(ColorDim)
		b.WriteString(dimStyle.Render(fmt.Sprintf(" (feature: %s)", m.featureID)))
	}

	// Task title list
	if len(m.taskTitles) > 0 {
		b.WriteString("\n")
		visibleTitles := m.taskTitles
		if len(visibleTitles) > MaxVisibleTasks {
			visibleTitles = visibleTitles[:MaxVisibleTasks]
		}

		dimBullet := lipgloss.NewStyle().Foreground(ColorDim)
		for _, title := range visibleTitles {
			truncated := truncateTitle(title, MaxTitleLength)
			b.WriteString("\n")
			b.WriteString(dimBullet.Render("  • "))
			b.WriteString(truncated)
		}

		hiddenCount := len(m.taskTitles) - len(visibleTitles)
		if hiddenCount > 0 {
			b.WriteString("\n")
			b.WriteString(dimBullet.Render(fmt.Sprintf("  ... and %d more", hiddenCount)))
		}
	}

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
	case "n":
		m.cancelled = true
		m.confirmed = false
		if m.onCancel != nil {
			return true, func() tea.Msg {
				return m.onCancel()
			}
		}
		// Let ModalManager handle close
		return false, nil
	case "esc":
		m.cancelled = true
		m.confirmed = false
		if m.onCancel != nil {
			return true, func() tea.Msg {
				return m.onCancel()
			}
		}
		// Let ModalManager handle close
		return false, nil
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
	// Calculate width based on message length and task titles
	maxLen := len(m.message)

	// Check task title widths (bullet prefix "  • " = 4 chars)
	for _, title := range m.taskTitles {
		titleLen := len(truncateTitle(title, MaxTitleLength)) + 4
		if titleLen > maxLen {
			maxLen = titleLen
		}
	}

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
	lines := 1 // message
	lines += 2 // blank line + prompt

	// Task title lines
	if len(m.taskTitles) > 0 {
		visible := len(m.taskTitles)
		if visible > MaxVisibleTasks {
			visible = MaxVisibleTasks
			lines++ // "... and N more" line
		}
		lines += visible + 1 // titles + blank line before list
	}

	return lines + 4 // padding
}
