package tui

import (
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

// ModalManager manages modal lifecycle and rendering.
type ModalManager struct {
	activeModal Modal
	stack       []Modal
}

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
	
	// Set new modal as active
	m.activeModal = modal
	
	// Initialize the modal
	return modal.Init()
}

// Close closes the active modal.
// If there are modals in the stack, pops the top one.
func (m *ModalManager) Close() tea.Cmd {
	if m.activeModal == nil {
		return nil
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

	return nil
}

// IsOpen returns true if a modal is currently open.
func (m *ModalManager) IsOpen() bool {
	return m.activeModal != nil
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

// HandleKey routes key presses to the active modal.
// Returns true if the key was handled, false otherwise.
// Esc key closes the modal by default.
func (m *ModalManager) HandleKey(key string) (bool, tea.Cmd) {
	if m.activeModal == nil {
		return false, nil
	}

	// Handle Esc key to close modal
	if key == "esc" {
		return true, m.Close()
	}

	// Route key to modal
	handled, cmd := m.activeModal.HandleKey(key)
	return handled, cmd
}

// View renders the modal overlay.
// Returns empty string if no modal is open.
func (m *ModalManager) View(width, height int) string {
	if m.activeModal == nil {
		return ""
	}

	// Get modal content
	content := m.activeModal.View()

	// Apply modal styling with border and title
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorCyan).
		Padding(1, 2)

	// Add title if present
	title := m.activeModal.Title()
	if title != "" {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorCyan)
		content = titleStyle.Render(title) + "\n\n" + content
	}

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
