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

// featureAssignmentResultMsg is sent when a manual assignment action completes.
type featureAssignmentResultMsg struct {
	runnerID  string
	projectID string
	featureID string
	action    string
	response  *types.FeatureAssignmentResponse
	err       error
}

// FeaturePickerModal displays project features for manual runner assignment.
type FeaturePickerModal struct {
	runnerID      string
	projectID     string
	features      []string
	selectedIndex int
	assignments   map[string]types.FeatureAssignmentResponse
	apiClient     *runner.APIClient
}

// NewFeaturePickerModal creates a feature picker for a runner.
func NewFeaturePickerModal(runnerID, projectID string, allFeatures []string, assignments []types.FeatureAssignmentResponse, apiClient *runner.APIClient) *FeaturePickerModal {
	assignmentMap := make(map[string]types.FeatureAssignmentResponse, len(assignments))
	for _, assignment := range assignments {
		if assignment.FeatureID != "" {
			assignmentMap[assignment.FeatureID] = assignment
		}
	}
	return &FeaturePickerModal{
		runnerID:      runnerID,
		projectID:     projectID,
		features:      allFeatures,
		selectedIndex: 0,
		assignments:   assignmentMap,
		apiClient:     apiClient,
	}
}

// Init implements Modal.
func (m *FeaturePickerModal) Init() tea.Cmd {
	return nil
}

// Update implements Modal.
func (m *FeaturePickerModal) Update(msg tea.Msg) (Modal, tea.Cmd) {
	return m, nil
}

// View implements Modal.
func (m *FeaturePickerModal) View() string {
	var b strings.Builder

	selectedStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true)

	dimStyle := lipgloss.NewStyle().
		Foreground(ColorDim)

	checkedStyle := lipgloss.NewStyle().
		Foreground(ColorReady).
		Bold(true)

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(ColorCyan).
		Bold(true)
	b.WriteString(titleStyle.Render(fmt.Sprintf("Assign Feature: %s", m.runnerID)))
	if m.projectID != "" {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("Project: %s", m.projectID)))
	}
	b.WriteString("\n\n")

	// Feature list
	if len(m.features) == 0 {
		b.WriteString(dimStyle.Render("  No features available"))
		b.WriteString("\n")
	} else {
		for i, feature := range m.features {
			marker := "[ ]"
			suffix := ""
			if assignment, ok := m.assignments[feature]; ok {
				if assignment.RunnerID == m.runnerID {
					marker = checkedStyle.Render("[*]")
				} else {
					marker = lipgloss.NewStyle().Foreground(ColorWaiting).Bold(true).Render("[!]")
				}
				source := assignment.Source
				if source == "" {
					source = "unknown"
				}
				target := assignment.RunnerID
				if target == "" {
					target = "none"
				}
				suffix = fmt.Sprintf(" -> %s (%s)", target, source)
				if assignment.Status != "" && assignment.Status != "active" {
					suffix += fmt.Sprintf(" %s", assignment.Status)
				}
			}

			line := fmt.Sprintf("%s %s%s", marker, feature, suffix)

			if i == m.selectedIndex {
				b.WriteString(selectedStyle.Render("→ " + line))
			} else {
				b.WriteString(dimStyle.Render("  " + line))
			}
			b.WriteString("\n")
		}
	}

	// Footer
	b.WriteString("\n")
	footerStyle := lipgloss.NewStyle().
		Foreground(ColorDim).
		Italic(true)
	b.WriteString(footerStyle.Render("j/k: navigate  Enter: assign/reassign  c: clear  Esc: cancel"))

	return b.String()
}

// HandleKey implements Modal.
func (m *FeaturePickerModal) HandleKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		if len(m.features) > 0 {
			m.selectedIndex++
			if m.selectedIndex >= len(m.features) {
				m.selectedIndex = 0 // wrap
			}
		}
		return true, nil

	case "k", "up":
		if len(m.features) > 0 {
			m.selectedIndex--
			if m.selectedIndex < 0 {
				m.selectedIndex = len(m.features) - 1 // wrap
			}
		}
		return true, nil

	case "enter", "return":
		if len(m.features) == 0 {
			return true, nil
		}
		featureID := m.features[m.selectedIndex]
		intent := "assign"
		if assignment, ok := m.assignments[featureID]; ok && assignment.RunnerID != "" && assignment.RunnerID != m.runnerID {
			intent = "reassign"
		}
		return true, m.assignFeatureCmd(featureID, intent)

	case "c":
		if len(m.features) == 0 {
			return true, nil
		}
		featureID := m.features[m.selectedIndex]
		if _, ok := m.assignments[featureID]; !ok {
			return true, nil
		}
		return true, m.clearAssignmentCmd(featureID)

	case "esc", "q":
		return false, nil // close modal without saving

	default:
		return false, nil
	}
}

// assignFeatureCmd calls the API to assign or reassign a feature to the runner.
func (m *FeaturePickerModal) assignFeatureCmd(featureID, intent string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.apiClient.AssignFeatureToRunner(context.Background(), m.projectID, featureID, types.FeatureAssignmentRequest{
			RunnerID: m.runnerID,
			Intent:   intent,
		})
		if err != nil {
			return featureAssignmentResultMsg{
				runnerID:  m.runnerID,
				projectID: m.projectID,
				featureID: featureID,
				action:    intent,
				err:       err,
			}
		}
		return featureAssignmentResultMsg{
			runnerID:  m.runnerID,
			projectID: m.projectID,
			featureID: featureID,
			action:    intent,
			response:  resp,
		}
	}
}

// clearAssignmentCmd calls the API to clear the selected feature assignment.
func (m *FeaturePickerModal) clearAssignmentCmd(featureID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.apiClient.ClearFeatureAssignment(context.Background(), m.projectID, featureID)
		if err != nil {
			return featureAssignmentResultMsg{
				runnerID:  m.runnerID,
				projectID: m.projectID,
				featureID: featureID,
				action:    "clear",
				err:       err,
			}
		}
		return featureAssignmentResultMsg{
			runnerID:  m.runnerID,
			projectID: m.projectID,
			featureID: featureID,
			action:    "clear",
			response:  resp,
		}
	}
}

// Title implements Modal (optional, for modal manager).
func (m *FeaturePickerModal) Title() string {
	return "Feature Assignment"
}

// Height returns the approximate height of the modal content.
func (m *FeaturePickerModal) Height() int {
	// Title (2 lines) + features + footer (2 lines) + padding
	return 4 + len(m.features) + 2
}

// Width returns the desired width of the modal content.
func (m *FeaturePickerModal) Width() int {
	return 60 // reasonable width for feature names
}
