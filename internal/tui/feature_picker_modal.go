package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/runner"
)

// featureAffinityResultMsg is sent when affinity update completes.
type featureAffinityResultMsg struct {
	runnerID string
	features []string
	err      error
}

// FeaturePickerModal displays a list of features for multi-selection.
// User can select/deselect features to assign to a runner.
type FeaturePickerModal struct {
	runnerID      string
	features      []string        // all available features
	selectedIndex int             // cursor position
	selectedSet   map[string]bool // selected features
	apiClient     *runner.APIClient
}

// NewFeaturePickerModal creates a feature picker for a runner.
func NewFeaturePickerModal(runnerID string, currentFeatures []string, allFeatures []string, apiClient *runner.APIClient) *FeaturePickerModal {
	// Build selected set from current features
	selectedSet := make(map[string]bool)
	for _, f := range currentFeatures {
		selectedSet[f] = true
	}

	return &FeaturePickerModal{
		runnerID:      runnerID,
		features:      allFeatures,
		selectedIndex: 0,
		selectedSet:   selectedSet,
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
	b.WriteString(titleStyle.Render(fmt.Sprintf("Feature Affinity: %s", m.runnerID)))
	b.WriteString("\n\n")

	// Feature list
	if len(m.features) == 0 {
		b.WriteString(dimStyle.Render("  No features available"))
		b.WriteString("\n")
	} else {
		for i, feature := range m.features {
			checkbox := "[ ]"
			if m.selectedSet[feature] {
				checkbox = checkedStyle.Render("[✓]")
			}

			line := fmt.Sprintf("%s %s", checkbox, feature)

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
	b.WriteString(footerStyle.Render("j/k: navigate  Space: toggle  Enter: save  Esc: cancel"))

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

	case " ": // space to toggle
		if len(m.features) > 0 {
			feature := m.features[m.selectedIndex]
			if m.selectedSet[feature] {
				delete(m.selectedSet, feature)
			} else {
				m.selectedSet[feature] = true
			}
		}
		return true, nil

	case "enter", "return":
		// Collect selected features
		selectedFeatures := []string{}
		for _, f := range m.features {
			if m.selectedSet[f] {
				selectedFeatures = append(selectedFeatures, f)
			}
		}

		// Call API to update affinity
		return true, m.updateAffinityCmd(selectedFeatures)

	case "esc", "q":
		return false, nil // close modal without saving

	default:
		return false, nil
	}
}

// updateAffinityCmd calls the API to update runner affinity.
func (m *FeaturePickerModal) updateAffinityCmd(features []string) tea.Cmd {
	return func() tea.Msg {
		err := m.apiClient.UpdateRunnerAffinity(context.Background(), m.runnerID, features)
		if err != nil {
			return featureAffinityResultMsg{
				runnerID: m.runnerID,
				features: features,
				err:      err,
			}
		}
		return featureAffinityResultMsg{
			runnerID: m.runnerID,
			features: features,
			err:      nil,
		}
	}
}

// Title implements Modal (optional, for modal manager).
func (m *FeaturePickerModal) Title() string {
	return "Feature Affinity"
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
