package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings holds persisted TUI preferences.
type Settings struct {
	GroupCollapsed   map[string]bool `json:"groupCollapsed"`   // group name -> collapsed state
	FeatureCollapsed map[string]bool `json:"featureCollapsed"` // feature ID -> collapsed state
}

// getSettingsPath returns the path to the settings file.
// Uses ~/.brain/tui-settings.json
func getSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	brainDir := filepath.Join(home, ".brain")
	if err := os.MkdirAll(brainDir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(brainDir, "tui-settings.json"), nil
}

// LoadSettings loads settings from disk. Returns default settings if file doesn't exist.
func LoadSettings() (Settings, error) {
	path, err := getSettingsPath()
	if err != nil {
		return Settings{
			GroupCollapsed:   make(map[string]bool),
			FeatureCollapsed: make(map[string]bool),
		}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No settings file yet, return defaults
			return Settings{
				GroupCollapsed:   make(map[string]bool),
				FeatureCollapsed: make(map[string]bool),
			}, nil
		}
		return Settings{
			GroupCollapsed:   make(map[string]bool),
			FeatureCollapsed: make(map[string]bool),
		}, err
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{
			GroupCollapsed:   make(map[string]bool),
			FeatureCollapsed: make(map[string]bool),
		}, err
	}

	// Ensure maps are initialized
	if settings.GroupCollapsed == nil {
		settings.GroupCollapsed = make(map[string]bool)
	}
	if settings.FeatureCollapsed == nil {
		settings.FeatureCollapsed = make(map[string]bool)
	}

	return settings, nil
}

// SaveSettings saves settings to disk.
func SaveSettings(settings Settings) error {
	path, err := getSettingsPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
