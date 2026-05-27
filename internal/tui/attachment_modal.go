package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/huynle/brain-api/internal/types"
)

type attachmentModalActionMsg struct {
	Action     string
	Attachment types.AttachmentReference
}

// AttachmentModal presents entry attachments with enough metadata to choose an
// explicit open/download action without relying on subtle inline highlighting.
type AttachmentModal struct {
	attachments   []types.AttachmentReference
	selectedIndex int
}

func NewAttachmentModal(attachments []types.AttachmentReference, selectedIndex int) *AttachmentModal {
	items := append([]types.AttachmentReference(nil), attachments...)
	m := &AttachmentModal{attachments: items, selectedIndex: selectedIndex}
	m.clampSelection()
	return m
}

func (m *AttachmentModal) Init() tea.Cmd { return nil }

func (m *AttachmentModal) Update(msg tea.Msg) (Modal, tea.Cmd) { return m, nil }

func (m *AttachmentModal) View() string {
	var b strings.Builder
	dimStyle := lipgloss.NewStyle().Foreground(ColorDim)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorCyan).Bold(true)

	if len(m.attachments) == 0 {
		b.WriteString(dimStyle.Render("No attachments on this entry."))
		return b.String()
	}

	b.WriteString(dimStyle.Render("Select an attachment, then open, download, or extract it."))
	b.WriteString("\n\n")
	for i, att := range m.attachments {
		marker := "  "
		style := dimStyle
		if i == m.selectedIndex {
			marker = "→ "
			style = selectedStyle
		}

		name := attachmentDisplayName(att)
		role := attachmentDisplayValue(att.Role, "attachment")
		mime := attachmentDisplayValue(att.ContentType, "unknown MIME")
		id := attachmentDisplayValue(att.ID, "unknown ID")

		b.WriteString(style.Render(marker + name))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("    ID: %s", id)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("    Role: %s", role)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("    MIME: %s", mime)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render(fmt.Sprintf("    Size: %s", formatAttachmentSize(att.Size))))
		b.WriteString("\n")
		for _, line := range attachmentExtractionLines(att) {
			b.WriteString(dimStyle.Render("    " + line))
			b.WriteString("\n")
		}
		if att.Caption != "" {
			b.WriteString(dimStyle.Render("    Caption: " + att.Caption))
			b.WriteString("\n")
		}
		if len(att.Derived) > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("    Derived: %d artifact(s)", len(att.Derived))))
			b.WriteString("\n")
		}
		if i < len(m.attachments)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("j/k: select  o: open  d: download  x: extract  Enter: open  Esc/q: close"))
	return b.String()
}

func (m *AttachmentModal) HandleKey(key string) (bool, tea.Cmd) {
	switch key {
	case "j", "down":
		m.move(1)
		return true, nil
	case "k", "up":
		m.move(-1)
		return true, nil
	case "o", "enter", "return":
		return true, m.actionCmd("open")
	case "d":
		return true, m.actionCmd("download")
	case "x":
		return true, m.actionCmd("extract")
	case "q":
		return false, nil
	default:
		return false, nil
	}
}

func (m *AttachmentModal) HandleMouse(_ tea.MouseMsg, _ int, y int) (bool, tea.Cmd) {
	idx := m.indexAtLine(y)
	if idx < 0 {
		return false, nil
	}
	m.selectedIndex = idx
	return true, nil
}

func (m *AttachmentModal) SelectedAttachment() types.AttachmentReference {
	m.clampSelection()
	if len(m.attachments) == 0 {
		return types.AttachmentReference{}
	}
	return m.attachments[m.selectedIndex]
}

func (m *AttachmentModal) Title() string { return "Attachments" }

func (m *AttachmentModal) Width() int { return 86 }

func (m *AttachmentModal) Height() int {
	if len(m.attachments) == 0 {
		return 3
	}
	height := 4 // intro, blank, help, final spacing
	for _, att := range m.attachments {
		height += 5
		height += len(attachmentExtractionLines(att))
		if att.Caption != "" {
			height++
		}
		if len(att.Derived) > 0 {
			height++
		}
	}
	if len(m.attachments) > 1 {
		height += len(m.attachments) - 1
	}
	return height
}

func (m *AttachmentModal) move(delta int) {
	if len(m.attachments) == 0 {
		return
	}
	m.selectedIndex = (m.selectedIndex + delta + len(m.attachments)) % len(m.attachments)
}

func (m *AttachmentModal) actionCmd(action string) tea.Cmd {
	att := m.SelectedAttachment()
	return func() tea.Msg {
		return attachmentModalActionMsg{Action: action, Attachment: att}
	}
}

func (m *AttachmentModal) clampSelection() {
	if len(m.attachments) == 0 {
		m.selectedIndex = 0
		return
	}
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	if m.selectedIndex >= len(m.attachments) {
		m.selectedIndex = len(m.attachments) - 1
	}
}

func (m *AttachmentModal) indexAtLine(y int) int {
	if len(m.attachments) == 0 || y < 2 {
		return -1
	}
	line := y - 2
	for i, att := range m.attachments {
		rowLines := 5
		rowLines += len(attachmentExtractionLines(att))
		if att.Caption != "" {
			rowLines++
		}
		if len(att.Derived) > 0 {
			rowLines++
		}
		if line < rowLines {
			return i
		}
		line -= rowLines + 1
	}
	return -1
}

func attachmentDisplayName(att types.AttachmentReference) string {
	if att.Filename != "" {
		return att.Filename
	}
	if att.ID != "" {
		return att.ID
	}
	return "(unnamed attachment)"
}

func attachmentDisplayValue(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func attachmentExtractionLines(att types.AttachmentReference) []string {
	textStatus := "none"
	status := att.Metadata["extraction_status"]
	provider := att.Metadata["extraction_provider"]
	model := att.Metadata["extraction_model"]
	errText := att.Metadata["extraction_error"]
	if att.DerivedText != nil {
		status = att.DerivedText.Status
		provider = att.DerivedText.Metadata["provider"]
		model = att.DerivedText.Metadata["model"]
		errText = att.DerivedText.Error
	}
	if len(att.Derived) > 0 || status == types.AttachmentExtractionStatusReady {
		textStatus = "ready"
	}

	if status == "" {
		status = "not requested"
	}

	lines := []string{"Text: " + textStatus, "Extraction: " + status}
	if provider != "" || model != "" {
		lines = append(lines, fmt.Sprintf("Model: %s / %s", attachmentDisplayValue(provider, "unknown provider"), attachmentDisplayValue(model, "unknown model")))
	}
	if errText != "" {
		lines = append(lines, "Error: "+errText)
	}
	return lines
}
