package main

import (
	"flag"
	"fmt"
	"github.com/huynle/brain-api/cmd/brain/commands"
)

// GlobalFlags contains flags applicable to all commands
type GlobalFlags struct {
	Verbose bool
	Help    bool
	Version bool
}

// ServerFlags for server command
type ServerFlags struct {
	Port    int
	Host    string
	Daemon  bool
	LogFile string
	TLS     bool
	TLSCert string
	TLSKey  string
}

// RunnerFlags for runner commands
type RunnerFlags struct {
	TUI          bool
	Foreground   bool
	Background   bool
	Dashboard    bool
	MaxParallel  int
	PollInterval int
	Workdir      string
	Agent        string
	Model        string
	Include      []string
	Exclude      []string
	Follow       bool
}

// MCPFlags for MCP command
type MCPFlags struct {
	APIURL string
}

// TokenFlags for token command
type TokenFlags struct {
	Name string
}

// ParseGlobalFlags parses global flags from args
func ParseGlobalFlags(args []string) (*GlobalFlags, []string) {
	flags := &GlobalFlags{}
	fs := flag.NewFlagSet("global", flag.ContinueOnError)
	fs.BoolVar(&flags.Verbose, "verbose", false, "Verbose output")
	fs.BoolVar(&flags.Verbose, "v", false, "Verbose output (short)")
	fs.BoolVar(&flags.Help, "help", false, "Show help")
	fs.BoolVar(&flags.Help, "h", false, "Show help (short)")
	fs.BoolVar(&flags.Version, "version", false, "Show version")

	// Parse and return remaining args
	fs.Parse(args)
	return flags, fs.Args()
}

// ParseServerFlags parses server-specific flags
func ParseServerFlags(args []string) (*ServerFlags, error) {
	flags := &ServerFlags{}
	fs := flag.NewFlagSet("server", flag.ExitOnError)

	fs.IntVar(&flags.Port, "port", 0, "Server port")
	fs.IntVar(&flags.Port, "p", 0, "Server port (short)")
	fs.StringVar(&flags.Host, "host", "", "Server host")
	fs.BoolVar(&flags.Daemon, "daemon", false, "Run as daemon")
	fs.BoolVar(&flags.Daemon, "d", false, "Run as daemon (short)")
	fs.StringVar(&flags.LogFile, "log-file", "", "Log file path")
	fs.BoolVar(&flags.TLS, "tls", false, "Enable TLS")
	fs.StringVar(&flags.TLSCert, "tls-cert", "", "TLS certificate path")
	fs.StringVar(&flags.TLSKey, "tls-key", "", "TLS key path")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return flags, nil
}

// ParseRunnerFlags parses runner-specific flags
func ParseRunnerFlags(args []string) (*RunnerFlags, error) {
	flags := &RunnerFlags{}
	fs := flag.NewFlagSet("runner", flag.ExitOnError)

	fs.BoolVar(&flags.TUI, "tui", false, "Interactive TUI")
	fs.BoolVar(&flags.Foreground, "foreground", false, "Foreground without TUI")
	fs.BoolVar(&flags.Foreground, "f", false, "Foreground (short)")
	fs.BoolVar(&flags.Background, "background", false, "Background daemon")
	fs.BoolVar(&flags.Background, "b", false, "Background (short)")
	fs.BoolVar(&flags.Dashboard, "dashboard", false, "Tmux dashboard")
	fs.IntVar(&flags.MaxParallel, "max-parallel", 0, "Max parallel tasks")
	fs.IntVar(&flags.MaxParallel, "p", 0, "Max parallel (short)")
	fs.IntVar(&flags.PollInterval, "poll-interval", 0, "Poll interval seconds")
	fs.StringVar(&flags.Workdir, "workdir", "", "Working directory")
	fs.StringVar(&flags.Workdir, "w", "", "Working directory (short)")
	fs.StringVar(&flags.Agent, "agent", "", "OpenCode agent")
	fs.StringVar(&flags.Model, "model", "", "Model to use")
	fs.StringVar(&flags.Model, "m", "", "Model (short)")
	fs.BoolVar(&flags.Follow, "follow", false, "Follow logs")

	// Multi-value flags
	fs.Func("include", "Include project pattern", func(s string) error {
		flags.Include = append(flags.Include, s)
		return nil
	})
	fs.Func("i", "Include pattern (short)", func(s string) error {
		flags.Include = append(flags.Include, s)
		return nil
	})
	fs.Func("exclude", "Exclude project pattern", func(s string) error {
		flags.Exclude = append(flags.Exclude, s)
		return nil
	})
	fs.Func("e", "Exclude pattern (short)", func(s string) error {
		flags.Exclude = append(flags.Exclude, s)
		return nil
	})

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return flags, nil
}

// ParseMCPFlags parses MCP-specific flags
func ParseMCPFlags(args []string) (*MCPFlags, error) {
	flags := &MCPFlags{}
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)

	fs.StringVar(&flags.APIURL, "api-url", "", "Brain API URL")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return flags, nil
}

// ParseTokenFlags parses token-specific flags
func ParseTokenFlags(args []string) (*TokenFlags, error) {
	flags := &TokenFlags{}
	fs := flag.NewFlagSet("token", flag.ExitOnError)

	fs.StringVar(&flags.Name, "name", "", "Token name")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return flags, nil
}

// UnifiedConfig is a placeholder for the future unified config system
// This will be implemented in Phase 1.3
type UnifiedConfig struct {
	Server struct {
		Port       int
		Host       string
		BrainDir   string
		EnableAuth bool
		APIKey     string
		LogLevel   string
		TLS        struct {
			Enabled  bool
			CertPath string
			KeyPath  string
		}
		PIDFile string
		LogFile string
	}
	Runner struct {
		MaxParallel     int
		PollInterval    int
		WorkDir         string
		StateDir        string
		LogDir          string
		ExcludeProjects []string
		OpenCode        struct {
			Agent string
			Model string
		}
	}
	MCP struct {
		APIURL string
	}
}

// ApplyFlagsToConfig applies CLI flags to config with proper precedence
func ApplyFlagsToConfig(cfg *UnifiedConfig, globalFlags *GlobalFlags, cmdFlags interface{}) {
	// Apply command-specific flags based on type
	switch flags := cmdFlags.(type) {
	case *ServerFlags:
		if flags.Port != 0 {
			cfg.Server.Port = flags.Port
		}
		if flags.Host != "" {
			cfg.Server.Host = flags.Host
		}
		if flags.TLS {
			cfg.Server.TLS.Enabled = true
		}
		if flags.TLSCert != "" {
			cfg.Server.TLS.CertPath = flags.TLSCert
		}
		if flags.TLSKey != "" {
			cfg.Server.TLS.KeyPath = flags.TLSKey
		}

	case *RunnerFlags:
		if flags.MaxParallel != 0 {
			cfg.Runner.MaxParallel = flags.MaxParallel
		}
		if flags.PollInterval != 0 {
			cfg.Runner.PollInterval = flags.PollInterval
		}
		if flags.Workdir != "" {
			cfg.Runner.WorkDir = flags.Workdir
		}
		if flags.Agent != "" {
			cfg.Runner.OpenCode.Agent = flags.Agent
		}
		if flags.Model != "" {
			cfg.Runner.OpenCode.Model = flags.Model
		}
		if len(flags.Exclude) > 0 {
			cfg.Runner.ExcludeProjects = append(cfg.Runner.ExcludeProjects, flags.Exclude...)
		}
	}
}

// LifecycleFlags holds flags for lifecycle commands (start, stop, restart).
type LifecycleFlags struct {
	PIDFile string
	LogFile string
	Timeout int
	Force   bool
	DryRun  bool
}

// ParseLifecycleFlags parses lifecycle command flags from args.
func ParseLifecycleFlags(args []string) (*LifecycleFlags, error) {
	flags := &LifecycleFlags{
		Timeout: 10, // Default 10 second timeout
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--pid-file":
			if i+1 < len(args) {
				flags.PIDFile = args[i+1]
				i++
			}
		case "--log-file":
			if i+1 < len(args) {
				flags.LogFile = args[i+1]
				i++
			}
		case "--timeout":
			if i+1 < len(args) {
				timeout := 0
				if _, err := fmt.Sscanf(args[i+1], "%d", &timeout); err == nil {
					flags.Timeout = timeout
				}
				i++
			}
		case "--force", "-f":
			flags.Force = true
		case "--dry-run":
			flags.DryRun = true
		}
	}

	return flags, nil
}

// convertToCommandsLifecycleFlags converts main.LifecycleFlags to commands.LifecycleFlags.
func convertToCommandsLifecycleFlags(flags *LifecycleFlags) *commands.LifecycleFlags {
	return &commands.LifecycleFlags{
		PIDFile: flags.PIDFile,
		LogFile: flags.LogFile,
		Timeout: flags.Timeout,
		Force:   flags.Force,
		DryRun:  flags.DryRun,
	}
}
