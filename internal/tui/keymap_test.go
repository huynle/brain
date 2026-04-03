package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// makeKeyMsg creates a tea.KeyMsg for a given key string.
// For simple rune keys like "H", "L", "T", "D" it creates a KeyRunes message.
// For special keys like "ctrl+l", "ctrl+o" it creates the appropriate KeyMsg.
func makeKeyMsg(k string) tea.KeyMsg {
	switch k {
	case "ctrl+l":
		return tea.KeyMsg{Type: tea.KeyCtrlL}
	case "ctrl+o":
		return tea.KeyMsg{Type: tea.KeyCtrlO}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
}

func TestKeyMapFromConfig_NoOverrides(t *testing.T) {
	defaults := DefaultKeyMap()
	result := KeyMapFromConfig(defaults, nil)

	// Should return defaults unchanged
	if !key.Matches(makeKeyMsg("H"), result.PrevContentTab) {
		t.Error("PrevContentTab should default to H")
	}
	if !key.Matches(makeKeyMsg("L"), result.NextContentTab) {
		t.Error("NextContentTab should default to L")
	}
	if !key.Matches(makeKeyMsg("l"), result.ToggleLogs) {
		t.Error("ToggleLogs should default to l")
	}
	if !key.Matches(makeKeyMsg("T"), result.ToggleDetail) {
		t.Error("ToggleDetail should default to T")
	}
}

func TestKeyMapFromConfig_WithOverrides(t *testing.T) {
	defaults := DefaultKeyMap()
	overrides := map[string]string{
		"prev_tab":      "Z",
		"next_tab":      "X",
		"toggle_logs":   "ctrl+o",
		"toggle_detail": "D",
	}
	result := KeyMapFromConfig(defaults, overrides)

	// Overridden bindings should match new keys
	if !key.Matches(makeKeyMsg("Z"), result.PrevContentTab) {
		t.Error("PrevContentTab should be overridden to Z")
	}
	if !key.Matches(makeKeyMsg("X"), result.NextContentTab) {
		t.Error("NextContentTab should be overridden to X")
	}
	if !key.Matches(makeKeyMsg("ctrl+o"), result.ToggleLogs) {
		t.Error("ToggleLogs should be overridden to ctrl+o")
	}
	if !key.Matches(makeKeyMsg("D"), result.ToggleDetail) {
		t.Error("ToggleDetail should be overridden to D")
	}
}

func TestKeyMapFromConfig_PartialOverrides(t *testing.T) {
	defaults := DefaultKeyMap()
	overrides := map[string]string{
		"toggle_logs": "ctrl+o",
	}
	result := KeyMapFromConfig(defaults, overrides)

	// Only toggle_logs should be overridden
	if !key.Matches(makeKeyMsg("ctrl+o"), result.ToggleLogs) {
		t.Error("ToggleLogs should be overridden to ctrl+o")
	}
	// Others should remain at defaults
	if !key.Matches(makeKeyMsg("H"), result.PrevContentTab) {
		t.Error("PrevContentTab should remain at default H")
	}
	if !key.Matches(makeKeyMsg("L"), result.NextContentTab) {
		t.Error("NextContentTab should remain at default L")
	}
	if !key.Matches(makeKeyMsg("T"), result.ToggleDetail) {
		t.Error("ToggleDetail should remain at default T")
	}
}

func TestKeyMapFromConfig_UnknownKeysIgnored(t *testing.T) {
	defaults := DefaultKeyMap()
	overrides := map[string]string{
		"unknown_key": "Z",
		"toggle_logs": "ctrl+o",
	}
	result := KeyMapFromConfig(defaults, overrides)

	// Known override should work
	if !key.Matches(makeKeyMsg("ctrl+o"), result.ToggleLogs) {
		t.Error("ToggleLogs should be overridden to ctrl+o")
	}
	// Unknown keys should not cause errors — defaults preserved
	if !key.Matches(makeKeyMsg("H"), result.PrevContentTab) {
		t.Error("PrevContentTab should remain at default H")
	}
}

func TestDefaultKeyMap_HasContentTabBindings(t *testing.T) {
	km := DefaultKeyMap()

	// Verify the 4 new bindings exist with correct defaults
	if !key.Matches(makeKeyMsg("H"), km.PrevContentTab) {
		t.Error("DefaultKeyMap should have PrevContentTab bound to H")
	}
	if !key.Matches(makeKeyMsg("L"), km.NextContentTab) {
		t.Error("DefaultKeyMap should have NextContentTab bound to L")
	}
	if !key.Matches(makeKeyMsg("l"), km.ToggleLogs) {
		t.Error("DefaultKeyMap should have ToggleLogs bound to l")
	}
	if !key.Matches(makeKeyMsg("T"), km.ToggleDetail) {
		t.Error("DefaultKeyMap should have ToggleDetail bound to T")
	}
}
