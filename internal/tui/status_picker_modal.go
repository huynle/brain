package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/runner"
	"github.com/huynle/brain-api/internal/types"
)

// statusPickerResultMsg is sent when a status change completes (single or batch).
type statusPickerResultMsg struct {
	newStatus    string
	successCount int
	failedCount  int
	err          error
}

// StatusPickerModal displays a list of valid statuses for quick selection.
// Supports single-task and batch (multi-task) modes.
type StatusPickerModal struct {
	statuses      []string
	selectedIndex int
	currentStatus string
	taskPaths     []string // one or more task paths to update
	apiClient     *runner.APIClient
}

// NewStatusPickerModal creates a status picker for a single task.
func NewStatusPickerModal(taskPath string, currentStatus string, apiClient *runner.APIClient) *StatusPickerModal {
	return newStatusPickerModal([]string{taskPath}, currentStatus, apiClient)
}

// NewStatusPickerModalBatch creates a status picker for multiple tasks.
// currentStatus can be empty if tasks have mixed statuses.
func NewStatusPickerModalBatch(taskPaths []string, currentStatus string, apiClient *runner.APIClient) *StatusPickerModal {
	return newStatusPickerModal(taskPaths, currentStatus, apiClient)
}

func newStatusPickerModal(taskPaths []string, currentStatus string, apiClient *runner.APIClient) *StatusPickerModal {
	statuses := make([]string, len(types.EntryStatuses))
	copy(statuses, types.EntryStatuses)

	// Find the index of the current status to pre-select it
	selectedIndex := 0
	for i, s := range statuses {
		if s == currentStatus {
			selectedIndex = i
			break
		}
	}

	return &StatusPickerModal{
		statuses:      statuses,
		selectedIndex: selectedIndex,
		currentStatus: currentStatus,
		taskPaths:     taskPaths,
		apiClient:     apiClient,
	}
}

// Init implements Modal.
func (m *StatusPickerModal) Init() tea.Cmd {
	return nil
}

// Update implements Modal.
func (m *StatusPickerModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

// View implements Modal.
func (m *StatusPickerModal) View() string {
	var b strings.Builder

	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorDim)

	currentStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true)

	// Batch indicator
	if len(m.taskPaths) > 1 {
		countStyle := lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)
		b.WriteString(countStyle.Render(fmt.Sprintf("%d tasks selected", len(m.taskPaths))))
		b.WriteString("\n\n")
	}

	// Status list
	for i, status := range m.statuses {
		// Current status marker
		suffix := ""
		if status == m.currentStatus {
			suffix = " " + currentStyle.Render("(current)")
		}

		if i == m.selectedIndex {
			b.WriteString(selectedStyle.Render("→ "+status) + suffix)
		} else {
			b.WriteString(dimStyle.Render("  "+status) + suffix)
		}
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true)
	b.WriteString(footerStyle.Render("j/k: navigate  Enter: apply  Esc: cancel"))

	return b.String()
}

// HandleKey implements Modal.
func (m *StatusPickerModal) HandleKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		m.selectedIndex++
		if m.selectedIndex >= len(m.statuses) {
			m.selectedIndex = 0 // wrap
		}
		return true, nil

	case "k", "up":
		m.selectedIndex--
		if m.selectedIndex < 0 {
			m.selectedIndex = len(m.statuses) - 1 // wrap
		}
		return true, nil

	case "enter", "return":
		newStatus := m.statuses[m.selectedIndex]

		// If selecting the same status for a single task, just close (no-op)
		if newStatus == m.currentStatus && len(m.taskPaths) == 1 {
			return true, nil
		}

		// Apply status change via UpdateMetadata
		apiClient := m.apiClient
		taskPaths := m.taskPaths

		return true, func() tea.Msg {
			ctx := context.Background()
			successCount := 0
			failedCount := 0
			var lastErr error

			for _, path := range taskPaths {
				err := apiClient.UpdateMetadata(ctx, path, map[string]interface{}{
					"status": newStatus,
				})
				if err != nil {
					failedCount++
					lastErr = err
				} else {
					successCount++
				}
			}

			return statusPickerResultMsg{
				newStatus:    newStatus,
				successCount: successCount,
				failedCount:  failedCount,
				err:          lastErr,
			}
		}

	case "esc":
		// ModalManager handles close on esc
		return true, nil

	default:
		// Consume all keys while modal is open
		return true, nil
	}
}

// Title implements Modal.
func (m *StatusPickerModal) Title() string {
	return "Change Status"
}

// Width implements Modal.
func (m *StatusPickerModal) Width() int {
	return 36
}

// Height implements Modal.
func (m *StatusPickerModal) Height() int {
	lines := len(m.statuses) // status list
	lines += 2               // blank + footer
	if len(m.taskPaths) > 1 {
		lines += 2 // batch header + blank
	}
	return lines
}
