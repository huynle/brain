package plugins

// Target represents a plugin installation target (e.g., OpenCode, Claude Code)
type Target interface {
	// ID returns the unique identifier for this target
	ID() string

	// Name returns the human-readable name
	Name() string

	// Description returns a description of what this target is
	Description() string

	// Exists checks if the target is already installed
	Exists() bool

	// Install performs the installation
	Install(opts InstallOptions) error

	// Uninstall removes the installed plugin
	Uninstall() error

	// Validate checks if the installation is valid and complete
	Validate() error
}

// GetAvailableTargets returns all available installation targets
func GetAvailableTargets() []Target {
	return []Target{
		NewOpenCodeTarget(),
	}
}
