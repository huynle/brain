package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings holds persisted TUI preferences.
type Settings struct {
	GroupCollapsed    map[string]bool `json:"groupCollapsed"`    // group name -> collapsed state
	GroupVisible      map[string]bool `json:"groupVisible"`      // group name -> visibility state
	FeatureCollapsed  map[string]bool `json:"featureCollapsed"`  // feature ID -> collapsed state
	ProjectLimits     map[string]int  `json:"projectLimits"`     // project -> max parallel tasks
	GlobalMaxParallel int             `json:"globalMaxParallel"` // global max parallel limit
	DefaultModel      string          `json:"defaultModel"`      // default model override for tasks
	TextWrap          bool            `json:"textWrap"`          // wrap long lines in panels
	LogLevel          string          `json:"logLevel"`          // log level: "error", "info", "debug"
}

// getDefaultGroupVisible returns the default visibility map for status groups.
// Visible by default: Ready, Waiting, Active, Blocked, Completed, Validated
// Hidden by default: Draft, Cancelled, Superseded, Archived
func getDefaultGroupVisible() map[string]bool {
	return map[string]bool{
		"Ready":      true,
		"Waiting":    true,
		"Active":     true,
		"Blocked":    true,
		"Completed":  true,
		"Validated":  true,
		"Draft":      false,
		"Cancelled":  false,
		"Superseded": false,
		"Archived":   false,
	}
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
			GroupCollapsed:    make(map[string]bool),
			GroupVisible:      getDefaultGroupVisible(),
			FeatureCollapsed:  make(map[string]bool),
			ProjectLimits:     make(map[string]int),
			GlobalMaxParallel: 4,
			DefaultModel:      "",
			TextWrap:          true,
			LogLevel:          "info",
		}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// No settings file yet, return defaults
			return Settings{
				GroupCollapsed:    make(map[string]bool),
				GroupVisible:      getDefaultGroupVisible(),
				FeatureCollapsed:  make(map[string]bool),
				ProjectLimits:     make(map[string]int),
				GlobalMaxParallel: 4,
				DefaultModel:      "",
				TextWrap:          true,
				LogLevel:          "info",
			}, nil
		}
		return Settings{
			GroupCollapsed:    make(map[string]bool),
			GroupVisible:      getDefaultGroupVisible(),
			FeatureCollapsed:  make(map[string]bool),
			ProjectLimits:     make(map[string]int),
			GlobalMaxParallel: 4,
			DefaultModel:      "",
			TextWrap:          true,
			LogLevel:          "info",
		}, err
	}

	var settings Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return Settings{
			GroupCollapsed:    make(map[string]bool),
			GroupVisible:      getDefaultGroupVisible(),
			FeatureCollapsed:  make(map[string]bool),
			ProjectLimits:     make(map[string]int),
			GlobalMaxParallel: 4,
			DefaultModel:      "",
			TextWrap:          true,
			LogLevel:          "info",
		}, err
	}

	// Ensure maps are initialized
	if settings.GroupCollapsed == nil {
		settings.GroupCollapsed = make(map[string]bool)
	}
	if settings.GroupVisible == nil || len(settings.GroupVisible) == 0 {
		settings.GroupVisible = getDefaultGroupVisible()
	} else {
		// Merge with defaults to ensure all groups have a visibility setting
		defaults := getDefaultGroupVisible()
		for group, visible := range defaults {
			if _, exists := settings.GroupVisible[group]; !exists {
				settings.GroupVisible[group] = visible
			}
		}
	}
	if settings.FeatureCollapsed == nil {
		settings.FeatureCollapsed = make(map[string]bool)
	}
	if settings.ProjectLimits == nil {
		settings.ProjectLimits = make(map[string]int)
	}
	if settings.GlobalMaxParallel == 0 {
		settings.GlobalMaxParallel = 4
	}
	if settings.DefaultModel == "" {
		settings.DefaultModel = "" // Empty string means no override
	}
	// TextWrap defaults to true (not set in JSON means false, so we need to check if loaded from file)
	// LogLevel defaults to "info"
	if settings.LogLevel == "" {
		settings.LogLevel = "info"
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
