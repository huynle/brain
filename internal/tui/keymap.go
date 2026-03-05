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

	// Actions
	Refresh key.Binding
	Quit    key.Binding
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
			key.WithKeys("h", "["),
			key.WithHelp("h/l", "Tabs"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("l", "]"),
			key.WithHelp("h/l", "Tabs"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "Refresh"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "Quit"),
		),
	}
}
