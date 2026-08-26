package tui

import tea "github.com/charmbracelet/bubbletea"

// mouseKind is a mouse event classified by its Action and Button.
type mouseKind int

const (
	mouseOther mouseKind = iota
	mouseLeft
	mouseRight
	mouseRelease
	mouseWheelUp
	mouseWheelDown
	mouseMotion
)

// classifyMouse folds a MouseMsg's Action/Button pair into the kinds this UI
// acts on.
//
// It replaces tea.MouseMsg.Type, which bubbletea deprecated in favour of
// Action + Button. The mapping is deliberately not a naive equality check: a
// left-button *drag* arrives as Action=Motion with Button=Left, and the panel
// splitter drag handling depends on that classifying as a left event rather
// than a bare motion. This mirrors bubbletea's own backward-compatibility
// shim, so behaviour is unchanged while reading only non-deprecated fields.
func classifyMouse(msg tea.MouseMsg) mouseKind {
	switch msg.Action {
	case tea.MouseActionRelease:
		// bubbletea only reports a plain release (X10 mode), with no button.
		if msg.Button == tea.MouseButtonNone {
			return mouseRelease
		}
	case tea.MouseActionPress, tea.MouseActionMotion:
		switch msg.Button {
		case tea.MouseButtonLeft:
			return mouseLeft
		case tea.MouseButtonRight:
			return mouseRight
		case tea.MouseButtonWheelUp:
			return mouseWheelUp
		case tea.MouseButtonWheelDown:
			return mouseWheelDown
		}
		// Buttonless movement (and buttons this UI ignores) is hover.
		if msg.Action == tea.MouseActionMotion {
			return mouseMotion
		}
	}
	return mouseOther
}
