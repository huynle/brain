package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/cmd/brain/assets"
)

// InitFlags holds flags for the init command.
type InitFlags struct {
	Force  bool
	DryRun bool
}

// InitCommand implements the init command.
type InitCommand struct {
	Config *UnifiedConfig
	Flags  *InitFlags
	Out    io.Writer
}

// Type returns the command type.
func (c *InitCommand) Type() string {
	return "init"
}

// Execute runs the init command.
func (c *InitCommand) Execute() error {
	// Get writer
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	// Expand tilde in brain directory path
	brainDir := expandPath(c.Config.Server.BrainDir)

	// Track counts for summary
	createdCount := 0
	skippedCount := 0

	// Define directories to create
	dirs := []string{
		brainDir,
		filepath.Join(brainDir, ".zk"),
		filepath.Join(brainDir, ".zk", "templates"),
		filepath.Join(brainDir, "global"),
		filepath.Join(brainDir, "projects"),
	}

	// Create directories
	if c.Flags.DryRun {
		fmt.Fprintf(out, "DRY RUN: Would create directories in %s\n", brainDir)
		for _, dir := range dirs {
			fmt.Fprintf(out, "  - %s\n", dir)
		}
	} else {
		for _, dir := range dirs {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}
		}
	}

	// Copy templates from embedded assets
	templates := assets.ListTemplates()
	templatesDir := filepath.Join(brainDir, ".zk", "templates")

	for _, templateName := range templates {
		destPath := filepath.Join(templatesDir, templateName)

		// Check if file exists
		exists := fileExists(destPath)

		if exists && !c.Flags.Force {
			skippedCount++
			if !c.Flags.DryRun {
				fmt.Fprintf(out, "⏭  Skipped %s (already exists)\n", templateName)
			}
			continue
		}

		if c.Flags.DryRun {
			if exists {
				fmt.Fprintf(out, "DRY RUN: Would overwrite %s\n", templateName)
			} else {
				fmt.Fprintf(out, "DRY RUN: Would create %s\n", templateName)
			}
			createdCount++
			continue
		}

		// Get template content
		content, err := assets.GetTemplate(templateName)
		if err != nil {
			fmt.Fprintf(out, "⚠️  Failed to load template %s: %v\n", templateName, err)
			continue
		}

		// Write template
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			fmt.Fprintf(out, "⚠️  Failed to write template %s: %v\n", templateName, err)
			continue
		}

		createdCount++
		fmt.Fprintf(out, "✅ Created %s\n", templateName)
	}

	// Copy config.toml
	configPath := filepath.Join(brainDir, ".zk", "config.toml")
	configExists := fileExists(configPath)

	if configExists && !c.Flags.Force {
		skippedCount++
		if !c.Flags.DryRun {
			fmt.Fprintf(out, "⏭  Skipped config.toml (already exists)\n")
		}
	} else {
		if c.Flags.DryRun {
			if configExists {
				fmt.Fprintf(out, "DRY RUN: Would overwrite config.toml\n")
			} else {
				fmt.Fprintf(out, "DRY RUN: Would create config.toml\n")
			}
		} else {
			configContent, err := assets.GetReferenceConfig()
			if err != nil {
				return fmt.Errorf("failed to load reference config: %w", err)
			}

			if err := os.WriteFile(configPath, configContent, 0644); err != nil {
				return fmt.Errorf("failed to write config.toml: %w", err)
			}

			fmt.Fprintf(out, "✅ Created config.toml\n")
		}
		createdCount++
	}

	// Print summary
	if c.Flags.DryRun {
		fmt.Fprintf(out, "\nDRY RUN Summary:\n")
		fmt.Fprintf(out, "  Would create/update: %d files\n", createdCount)
	} else {
		fmt.Fprintf(out, "\n✅ Initialization complete!\n")
		fmt.Fprintf(out, "  Created: %d files\n", createdCount)
		if skippedCount > 0 {
			fmt.Fprintf(out, "  Skipped: %d files (use --force to overwrite)\n", skippedCount)
		}
	}

	return nil
}

// fileExists checks if a file or directory exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ConfigCommand implements the config command.
type ConfigCommand struct {
	Config *UnifiedConfig
	Out    io.Writer
}

// Type returns the command type.
func (c *ConfigCommand) Type() string {
	return "config"
}

// Execute displays the current configuration.
func (c *ConfigCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}

	cfg := c.Config

	// Display Server configuration
	fmt.Fprintf(out, "=== Server Configuration ===\n")
	fmt.Fprintf(out, "  Port:        %d\n", cfg.Server.Port)
	fmt.Fprintf(out, "  Host:        %s\n", cfg.Server.Host)
	fmt.Fprintf(out, "  BrainDir:    %s\n", cfg.Server.BrainDir)
	fmt.Fprintf(out, "  EnableAuth:  %v\n", cfg.Server.EnableAuth)
	fmt.Fprintf(out, "  LogLevel:    %s\n", cfg.Server.LogLevel)

	if cfg.Server.PIDFile != "" {
		fmt.Fprintf(out, "  PIDFile:     %s\n", cfg.Server.PIDFile)
	}
	if cfg.Server.LogFile != "" {
		fmt.Fprintf(out, "  LogFile:     %s\n", cfg.Server.LogFile)
	}

	// Display TLS if enabled
	if cfg.Server.TLS.CertPath != "" || cfg.Server.TLS.KeyPath != "" {
		fmt.Fprintf(out, "  TLS:\n")
		if cfg.Server.TLS.CertPath != "" {
			fmt.Fprintf(out, "    CertPath:  %s\n", cfg.Server.TLS.CertPath)
		}
		if cfg.Server.TLS.KeyPath != "" {
			fmt.Fprintf(out, "    KeyPath:   %s\n", cfg.Server.TLS.KeyPath)
		}
	}
	fmt.Fprintln(out)

	// Display Runner configuration
	fmt.Fprintf(out, "=== Runner Configuration ===\n")
	fmt.Fprintf(out, "  MaxParallel:   %d\n", cfg.Runner.MaxParallel)
	fmt.Fprintf(out, "  PollInterval:  %d\n", cfg.Runner.PollInterval)

	if cfg.Runner.WorkDir != "" {
		fmt.Fprintf(out, "  WorkDir:       %s\n", cfg.Runner.WorkDir)
	}
	if cfg.Runner.StateDir != "" {
		fmt.Fprintf(out, "  StateDir:      %s\n", cfg.Runner.StateDir)
	}
	if cfg.Runner.LogDir != "" {
		fmt.Fprintf(out, "  LogDir:        %s\n", cfg.Runner.LogDir)
	}

	if len(cfg.Runner.ExcludeProjects) > 0 {
		fmt.Fprintf(out, "  ExcludeProjects:\n")
		for _, proj := range cfg.Runner.ExcludeProjects {
			fmt.Fprintf(out, "    - %s\n", proj)
		}
	}

	if cfg.Runner.OpenCode.Agent != "" || cfg.Runner.OpenCode.Model != "" {
		fmt.Fprintf(out, "  OpenCode:\n")
		if cfg.Runner.OpenCode.Agent != "" {
			fmt.Fprintf(out, "    Agent:     %s\n", cfg.Runner.OpenCode.Agent)
		}
		if cfg.Runner.OpenCode.Model != "" {
			fmt.Fprintf(out, "    Model:     %s\n", cfg.Runner.OpenCode.Model)
		}
	}
	fmt.Fprintln(out)

	// Display MCP configuration
	fmt.Fprintf(out, "=== MCP Configuration ===\n")
	fmt.Fprintf(out, "  APIURL:      %s\n", cfg.MCP.APIURL)
	fmt.Fprintln(out)

	// Display config file location
	configPath := getConfigPath()
	if fileExists(configPath) {
		fmt.Fprintf(out, "Config file: %s\n", configPath)
	} else {
		fmt.Fprintf(out, "Config file: (using defaults)\n")
	}

	return nil
}

// getConfigPath returns the unified config path.
// Duplicates logic from internal/config to avoid circular dependency.
func getConfigPath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		configHome = filepath.Join(homeDir, ".config")
	}
	return filepath.Join(configHome, "brain", "config.yaml")
}
