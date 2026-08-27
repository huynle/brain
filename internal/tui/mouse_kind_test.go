package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestClassifyMouse pins the mapping against bubbletea's own deprecated
// MouseMsg.Type shim, which classifyMouse replaces.
//
// The left-drag row is the one that matters: Action=Motion with Button=Left
// classifies as a left event, not a motion event. A naive
// "Press && Button==Left" migration would silently break splitter dragging,
// and no test would have caught it.
func TestClassifyMouse(t *testing.T) {
	tests := []struct {
		name   string
		action tea.MouseAction
		button tea.MouseButton
		want   mouseKind
	}{
		{"left press", tea.MouseActionPress, tea.MouseButtonLeft, mouseLeft},
		{"left drag", tea.MouseActionMotion, tea.MouseButtonLeft, mouseLeft},
		{"right press", tea.MouseActionPress, tea.MouseButtonRight, mouseRight},
		{"right drag", tea.MouseActionMotion, tea.MouseButtonRight, mouseRight},
		{"wheel up", tea.MouseActionPress, tea.MouseButtonWheelUp, mouseWheelUp},
		{"wheel down", tea.MouseActionPress, tea.MouseButtonWheelDown, mouseWheelDown},
		{"hover", tea.MouseActionMotion, tea.MouseButtonNone, mouseMotion},
		{"release", tea.MouseActionRelease, tea.MouseButtonNone, mouseRelease},
		{"press with no button", tea.MouseActionPress, tea.MouseButtonNone, mouseOther},
		{"middle press", tea.MouseActionPress, tea.MouseButtonMiddle, mouseOther},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tea.MouseMsg{Action: tt.action, Button: tt.button}
			if got := classifyMouse(msg); got != tt.want {
				t.Errorf("classifyMouse(%v, %v) = %v, want %v", tt.action, tt.button, got, tt.want)
			}
		})
	}
}
