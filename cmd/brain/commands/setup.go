package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/huynle/brain-api/cmd/brain/assets"
	"github.com/huynle/brain-api/internal/config"
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
		filepath.Join(brainDir, config.DataDir),
		filepath.Join(brainDir, config.DataDir, "templates"),
		filepath.Join(brainDir, "global"),
		filepath.Join(brainDir, "global", "automation"),
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
	templatesDir := filepath.Join(brainDir, config.DataDir, "templates")

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

	// Copy default automation entries to global/automation/
	automations := assets.ListAutomations()
	automationsDir := filepath.Join(brainDir, "global", "automation")

	for _, automationName := range automations {
		destPath := filepath.Join(automationsDir, automationName)

		// Check if file exists
		exists := fileExists(destPath)

		if exists && !c.Flags.Force {
			skippedCount++
			if !c.Flags.DryRun {
				fmt.Fprintf(out, "⏭  Skipped automation/%s (already exists)\n", automationName)
			}
			continue
		}

		if c.Flags.DryRun {
			if exists {
				fmt.Fprintf(out, "DRY RUN: Would overwrite automation/%s\n", automationName)
			} else {
				fmt.Fprintf(out, "DRY RUN: Would create automation/%s\n", automationName)
			}
			createdCount++
			continue
		}

		// Get automation content
		content, err := assets.GetAutomation(automationName)
		if err != nil {
			fmt.Fprintf(out, "⚠️  Failed to load automation %s: %v\n", automationName, err)
			continue
		}

		// Write automation
		if err := os.WriteFile(destPath, content, 0644); err != nil {
			fmt.Fprintf(out, "⚠️  Failed to write automation %s: %v\n", automationName, err)
			continue
		}

		createdCount++
		fmt.Fprintf(out, "✅ Created automation/%s\n", automationName)
	}

	// Copy config.toml
	configPath := filepath.Join(brainDir, config.DataDir, "config.toml")
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

			if configExists {
				backupPath, err := config.BackupConfigFile(configPath)
				if err != nil {
					return fmt.Errorf("failed to back up config.toml: %w", err)
				}
				_, _ = fmt.Fprintf(out, "Backup saved: %s\n", backupPath)
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

// ConfigFlags holds flags for the config command.
type ConfigFlags struct {
	Print bool
	Force bool
}

// ConfigCommand implements the config command.
type ConfigCommand struct {
	Config     *UnifiedConfig
	Subcommand string
	Flags      *ConfigFlags
	Out        io.Writer
}

// Type returns the command type.
func (c *ConfigCommand) Type() string {
	return "config"
}

// Execute runs the config command.
func (c *ConfigCommand) Execute() error {
	out := c.Out
	if out == nil {
		out = os.Stdout
	}
	flags := c.Flags
	if flags == nil {
		flags = &ConfigFlags{}
	}

	switch c.Subcommand {
	case "", "show":
		return c.executeShow(out)
	case "defaults":
		return printDefaultConfigYAML(out)
	case "init":
		if flags.Print {
			return printDefaultConfigYAML(out)
		}
		// Never touch an existing config without an explicit --force, and
		// with --force show the user exactly what they are about to lose.
		configPath := getConfigPath()
		existing, readErr := os.ReadFile(configPath)
		if readErr == nil {
			if !flags.Force {
				return fmt.Errorf("config file already exists: %s (use --force to overwrite; a timestamped .bak will be saved alongside it)", configPath)
			}
			_, _ = fmt.Fprintf(out, "⚠️  Overwriting existing config: %s\n", configPath)
			if defaults, err := config.DefaultConfigYAML(); err == nil {
				if diff := diffLines(string(existing), string(defaults)); len(diff) > 0 {
					_, _ = fmt.Fprintf(out, "Changes (- current, + default):\n")
					for _, line := range diff {
						_, _ = fmt.Fprintf(out, "  %s\n", line)
					}
				}
			}
		}
		path, backupPath, err := config.WriteDefaultConfig(flags.Force)
		if err != nil {
			return err
		}
		if backupPath != "" {
			_, _ = fmt.Fprintf(out, "Backup saved: %s\n", backupPath)
		}
		_, _ = fmt.Fprintf(out, "Wrote config file: %s\n", path)
		return nil
	default:
		return fmt.Errorf("unknown config subcommand: %q", c.Subcommand)
	}
}

func printDefaultConfigYAML(out io.Writer) error {
	data, err := config.DefaultConfigYAML()
	if err != nil {
		return fmt.Errorf("generate default config: %w", err)
	}
	_, err = out.Write(data)
	return err
}

// executeShow displays the current configuration.
func (c *ConfigCommand) executeShow(out io.Writer) error {

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
	if cfg.Server.Embedding.Provider != "" || cfg.Server.Embedding.Model != "" {
		fmt.Fprintf(out, "  Embedding:\n")
		fmt.Fprintf(out, "    Enabled:   %v\n", cfg.Server.Embedding.Enabled)
		if cfg.Server.Embedding.Provider != "" {
			fmt.Fprintf(out, "    Provider:  %s\n", cfg.Server.Embedding.Provider)
		}
		if cfg.Server.Embedding.Model != "" {
			fmt.Fprintf(out, "    Model:     %s\n", cfg.Server.Embedding.Model)
		}
		if cfg.Server.Embedding.BaseURL != "" {
			fmt.Fprintf(out, "    BaseURL:   %s\n", cfg.Server.Embedding.BaseURL)
		}
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

	if cfg.Runner.Opencode.Agent != "" || cfg.Runner.Opencode.Model != "" {
		fmt.Fprintf(out, "  OpenCode:\n")
		if cfg.Runner.Opencode.Agent != "" {
			fmt.Fprintf(out, "    Agent:     %s\n", cfg.Runner.Opencode.Agent)
		}
		if cfg.Runner.Opencode.Model != "" {
			fmt.Fprintf(out, "    Model:     %s\n", cfg.Runner.Opencode.Model)
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
