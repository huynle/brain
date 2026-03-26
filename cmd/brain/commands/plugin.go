package commands

import (
	"fmt"

	"github.com/huynle/brain-api/internal/plugins"
)

// PluginCommand implements the Command interface for plugin management.
type PluginCommand struct {
	Subcommand string // "install", "uninstall", "status"
	Target     string // "opencode", "claude-code", etc.
	Config     *UnifiedConfig
	Flags      *PluginFlags
}

// PluginFlags holds plugin command flags.
type PluginFlags struct {
	Force  bool
	DryRun bool
	APIURL string
}

// Type returns the command type identifier.
func (c *PluginCommand) Type() string {
	return fmt.Sprintf("plugin-%s", c.Subcommand)
}

// Execute runs the plugin command.
func (c *PluginCommand) Execute() error {
	switch c.Subcommand {
	case "install":
		return c.install()
	case "uninstall":
		return c.uninstall()
	case "status":
		return c.status()
	default:
		return fmt.Errorf("unknown subcommand: %s", c.Subcommand)
	}
}

func (c *PluginCommand) install() error {
	// Resolve API URL from config or flag
	apiURL := c.Config.MCP.APIURL
	if c.Flags.APIURL != "" {
		apiURL = c.Flags.APIURL
	}

	opts := plugins.InstallOptions{
		Force:  c.Flags.Force,
		DryRun: c.Flags.DryRun,
		APIURL: apiURL,
	}

	fmt.Printf("Installing brain to %s...\n\n", c.Target)

	if err := plugins.InstallPlugin(c.Target, opts); err != nil {
		return err
	}

	fmt.Printf("\n✅ Installation complete\n")
	return nil
}

func (c *PluginCommand) uninstall() error {
	fmt.Printf("Uninstalling brain from %s...\n\n", c.Target)

	if err := plugins.UninstallPlugin(c.Target); err != nil {
		return err
	}

	fmt.Printf("\n✅ Uninstallation complete\n")
	return nil
}

func (c *PluginCommand) status() error {
	fmt.Println("Plugin Installation Status")
	fmt.Println()

	targets := plugins.GetAvailableTargets()
	for _, target := range targets {
		status := "not installed"
		icon := "⏭"

		if target.Exists() {
			if err := target.Validate(); err == nil {
				status = "installed"
				icon = "✅"
			} else {
				status = "incomplete"
				icon = "⚠️"
			}
		} else {
			status = "target not found"
			icon = "❌"
		}

		fmt.Printf("%s %s - %s [%s]\n", icon, target.Name(), target.Description(), status)
	}

	return nil
}
