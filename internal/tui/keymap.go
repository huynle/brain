package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keyboard shortcuts for the TUI.
type KeyMap struct {
	// Navigation
	Up   key.Binding
	Down key.Binding
	Top  key.Binding
	Bot  key.Binding

	// Panel
	Tab key.Binding

	// Project tabs (multi-project mode)
	PrevTab key.Binding
	NextTab key.Binding

	// Content tabs (Tasks / Dream)
	PrevContentTab key.Binding
	NextContentTab key.Binding

	// Panel toggles (configurable)
	ToggleLogs   key.Binding
	ToggleDetail key.Binding

	// Actions
	Refresh  key.Binding
	Quit     key.Binding
	Pause    key.Binding
	PauseAll key.Binding
}

// DefaultKeyMap returns the default key bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("j/k", "Navigate"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/k", "Navigate"),
		),
		Top: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "Top"),
		),
		Bot: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "Bottom"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("Tab", "Panel"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("H"),
			key.WithHelp("H/L", "Project"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("H/L", "Project"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "Refresh"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "Quit"),
		),
		Pause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "Pause"),
		),
		PauseAll: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "Pause All"),
		),
		PrevContentTab: key.NewBinding(
			key.WithKeys("h", "["),
			key.WithHelp("h/l", "Tabs"),
		),
		NextContentTab: key.NewBinding(
			key.WithKeys("l", "]"),
			key.WithHelp("h/l", "Tabs"),
		),
		ToggleLogs: key.NewBinding(
			key.WithKeys("z"),
			key.WithHelp("z", "Logs"),
		),
		ToggleDetail: key.NewBinding(
			key.WithKeys("T"),
			key.WithHelp("T", "Detail"),
		),
	}
}

// KeyMapFromConfig returns a KeyMap with defaults overridden by user config.
// Recognized config keys: prev_tab, next_tab, toggle_logs, toggle_detail.
// Unknown keys are silently ignored.
func KeyMapFromConfig(defaults KeyMap, overrides map[string]string) KeyMap {
	km := defaults
	for name, keyStr := range overrides {
		switch name {
		case "prev_tab":
			km.PrevContentTab = key.NewBinding(
				key.WithKeys(keyStr),
				key.WithHelp(keyStr, "Content Tab"),
			)
		case "next_tab":
			km.NextContentTab = key.NewBinding(
				key.WithKeys(keyStr),
				key.WithHelp(keyStr, "Content Tab"),
			)
		case "toggle_logs":
			km.ToggleLogs = key.NewBinding(
				key.WithKeys(keyStr),
				key.WithHelp(keyStr, "Logs"),
			)
		case "toggle_detail":
			km.ToggleDetail = key.NewBinding(
				key.WithKeys(keyStr),
				key.WithHelp(keyStr, "Detail"),
			)
		}
	}
	return km
}
