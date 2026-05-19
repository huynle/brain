package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type projectSelectedMsg struct {
	projectID string
}

type ProjectPickerModal struct {
	projects      []string
	selectedIndex int
}

func NewProjectPickerModal(projects []string, currentProject string) *ProjectPickerModal {
	items := append([]string{"all"}, projects...)
	selectedIndex := 0
	for i, project := range items {
		if project == currentProject {
			selectedIndex = i
			break
		}
	}
	return &ProjectPickerModal{projects: items, selectedIndex: selectedIndex}
}

func (m *ProjectPickerModal) Init() tea.Cmd { return nil }

func (m *ProjectPickerModal) Update(msg tea.Msg) (Modal, tea.Cmd) { return m, nil }

func (m *ProjectPickerModal) View() string {
	var b strings.Builder
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	b.WriteString(dimStyle.Render("Choose a project from the full list."))
	b.WriteString("\n\n")
	for i, project := range m.projects {
		label := project
		if project == "all" {
			label = "All projects"
		}
		if i == m.selectedIndex {
			b.WriteString(selectedStyle.Render("→ " + label))
		} else {
			b.WriteString(dimStyle.Render("  " + label))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("j/k: navigate  Enter/click: select  Esc: cancel"))
	return b.String()
}

func (m *ProjectPickerModal) HandleKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		if len(m.projects) > 0 {
			m.selectedIndex = (m.selectedIndex + 1) % len(m.projects)
		}
		return true, nil
	case "k", "up":
		if len(m.projects) > 0 {
			m.selectedIndex--
			if m.selectedIndex < 0 {
				m.selectedIndex = len(m.projects) - 1
			}
		}
		return true, nil
	case "enter", "return":
		return true, m.selectCmd()
	case "esc", "q":
		return false, nil
	default:
		return false, nil
	}
}

func (m *ProjectPickerModal) HandleMouse(msg tea.MouseMsg, x, y int) (bool, tea.Cmd) {
	_ = msg
	_ = x
	row := y - 2
	if row < 0 || row >= len(m.projects) {
		return false, nil
	}
	m.selectedIndex = row
	return true, m.selectCmd()
}

func (m *ProjectPickerModal) selectCmd() tea.Cmd {
	if len(m.projects) == 0 {
		return nil
	}
	projectID := m.projects[m.selectedIndex]
	return func() tea.Msg {
		return projectSelectedMsg{projectID: projectID}
	}
}

func (m *ProjectPickerModal) Title() string { return "Select Project" }

func (m *ProjectPickerModal) Height() int { return len(m.projects) + 4 }

func (m *ProjectPickerModal) Width() int { return 72 }
