package main

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/huynle/brain-api/cmd/brain/commands"
	uconfig "github.com/huynle/brain-api/internal/config"
	"github.com/huynle/brain-api/internal/runner"
)

// GlobalFlags contains flags applicable to all commands
type GlobalFlags struct {
	Verbose bool
	Help    bool
	Version bool
}

// APIFlags for api command
type APIFlags struct {
	Port          int
	Host          string
	Daemon        bool
	LogFile       string
	TLS           bool
	TLSCert       string
	TLSKey        string
	Runner        bool
	RunnerProject string
	MaxParallel   int
	Include       []string
	Exclude       []string
	Executor      string
}

// RunnerFlags for runner commands
type RunnerFlags struct {
	TUI          bool
	Foreground   bool
	Headless     bool
	Dashboard    bool
	Monitor      bool
	Runner       bool
	MaxParallel  int
	PollInterval int
	Workdir      string
	Agent        string
	Model        string
	Executor     string
	PiBin        string
	PiModel      string
	PiThinking   string
	Include      []string
	Exclude      []string
	FeatureIDs   []string
	Follow       bool
}

// MCPFlags for MCP command
type MCPFlags struct {
	APIURL string
}

// TokenFlags for token command
type TokenFlags struct {
	Name  string
	Scope string
}

// PluginFlags for plugin commands (install, uninstall, plugin-status)
type PluginFlags struct {
	Force  bool
	DryRun bool
	APIURL string
}

// InitFlags for init command
type InitFlags struct {
	Force  bool
	DryRun bool
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

	// Parse and return remaining args. ContinueOnError already reports a bad
	// flag to the flag set's output, and fs.Args() stays usable after one, so
	// there is nothing further to do with the error here.
	_ = fs.Parse(args)
	return flags, fs.Args()
}

// ParseAPIFlags parses API server-specific flags
func ParseAPIFlags(args []string) (*APIFlags, error) {
	flags := &APIFlags{}
	fs := flag.NewFlagSet("api", flag.ExitOnError)

	fs.IntVar(&flags.Port, "port", 0, "Server port")
	fs.IntVar(&flags.Port, "p", 0, "Server port (short)")
	fs.StringVar(&flags.Host, "host", "", "Server host")
	fs.BoolVar(&flags.Daemon, "daemon", false, "Run as daemon")
	fs.BoolVar(&flags.Daemon, "d", false, "Run as daemon (short)")
	fs.StringVar(&flags.LogFile, "log-file", "", "Log file path")
	fs.BoolVar(&flags.TLS, "tls", false, "Enable TLS")
	fs.StringVar(&flags.TLSCert, "tls-cert", "", "TLS certificate path")
	fs.StringVar(&flags.TLSKey, "tls-key", "", "TLS key path")
	fs.BoolVar(&flags.Runner, "runner", false, "Run embedded task runner")
	fs.StringVar(&flags.RunnerProject, "runner-project", "", "Embedded runner project")
	fs.IntVar(&flags.MaxParallel, "max-parallel", 0, "Embedded runner max parallel tasks")
	fs.StringVar(&flags.Executor, "executor", "", "Embedded runner executor")
	fs.Func("include", "Embedded runner include project pattern", func(s string) error {
		flags.Include = append(flags.Include, s)
		return nil
	})
	fs.Func("i", "Embedded runner include project pattern (short)", func(s string) error {
		flags.Include = append(flags.Include, s)
		return nil
	})
	fs.Func("exclude", "Embedded runner exclude project pattern", func(s string) error {
		flags.Exclude = append(flags.Exclude, s)
		return nil
	})
	fs.Func("e", "Embedded runner exclude project pattern (short)", func(s string) error {
		flags.Exclude = append(flags.Exclude, s)
		return nil
	})

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
	fs.BoolVar(&flags.Headless, "headless", false, "Headless mode (no TUI, no tmux)")
	fs.BoolVar(&flags.Headless, "b", false, "Headless (short)")
	fs.BoolVar(&flags.Dashboard, "dashboard", false, "Tmux dashboard")
	fs.BoolVar(&flags.Monitor, "monitor", false, "Monitor-only TUI (no local runner)")
	fs.BoolVar(&flags.Runner, "runner", false, "Run a local runner alongside the TUI (brain start)")
	fs.IntVar(&flags.MaxParallel, "max-parallel", 0, "Max parallel tasks")
	fs.IntVar(&flags.MaxParallel, "p", 0, "Max parallel (short)")
	fs.IntVar(&flags.PollInterval, "poll-interval", 0, "Poll interval seconds")
	fs.StringVar(&flags.Workdir, "workdir", "", "Working directory")
	fs.StringVar(&flags.Workdir, "w", "", "Working directory (short)")
	fs.StringVar(&flags.Agent, "agent", "", "OpenCode agent")
	fs.StringVar(&flags.Model, "model", "", "Model to use")
	fs.StringVar(&flags.Model, "m", "", "Model (short)")
	fs.StringVar(&flags.Executor, "executor", "", "Default executor (opencode or pi)")
	fs.StringVar(&flags.PiBin, "pi-bin", "", "Pi binary path")
	fs.StringVar(&flags.PiModel, "pi-model", "", "Pi model")
	fs.StringVar(&flags.PiThinking, "pi-thinking", "", "Pi thinking level (off, minimal, low, medium, high, xhigh)")
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
	fs.Func("feature-id", "Only run tasks from this feature (repeatable)", func(s string) error {
		flags.FeatureIDs = append(flags.FeatureIDs, s)
		return nil
	})
	fs.Func("F", "Feature ID filter (short)", func(s string) error {
		flags.FeatureIDs = append(flags.FeatureIDs, s)
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
	fs.StringVar(&flags.Scope, "scope", "", "Token scope: admin:*, runner:*, read:*, or control:* (default: admin:*)")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	return flags, nil
}

// ParseDreamFlags parses dream-specific flags
// ParsePluginFlags parses plugin-specific flags
func ParsePluginFlags(args []string) (*PluginFlags, error) {
	flags := &PluginFlags{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--force", "-f":
			flags.Force = true
		case "--dry-run":
			flags.DryRun = true
		case "--api-url":
			if i+1 < len(args) {
				flags.APIURL = args[i+1]
				i++
			}
		}
	}

	return flags, nil
}

// UnifiedConfig holds the unified configuration for the brain CLI.
// Runner uses runner.RunnerConfig directly so all fields pass through
// without lossy field-by-field copying.
type UnifiedConfig struct {
	Server struct {
		Port       int
		Host       string
		BrainDir   string
		EnableAuth bool
		LogLevel   string
		CORSOrigin string
		OAuthPIN   string
		JWTSecret  string
		TLS        struct {
			Enabled  bool
			CertPath string
			KeyPath  string
		}
		PIDFile         string
		LogFile         string
		LogMaxSize      int // MB
		LogMaxBackups   int
		TaskDefaults    uconfig.TaskDefaultsConfig
		FeatureCheckout uconfig.FeatureCheckoutConfig
		IndexWatch      uconfig.IndexWatchConfig
		Embedding       uconfig.EmbeddingConfig
		Attachments     uconfig.AttachmentConfig

		AttachmentExtraction uconfig.AttachmentExtractionConfig
		Assistant            uconfig.AssistantConfig
	}
	Runner runner.RunnerConfig
	MCP    struct {
		APIURL string
	}
	TUI struct {
		KeyBindings map[string]string
	}
}

// ApplyFlagsToConfig applies CLI flags to config with proper precedence
func ApplyFlagsToConfig(cfg *UnifiedConfig, globalFlags *GlobalFlags, cmdFlags interface{}) {
	// Apply command-specific flags based on type
	switch flags := cmdFlags.(type) {
	case *APIFlags:
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
			cfg.Runner.Opencode.Agent = flags.Agent
		}
		if flags.Model != "" {
			cfg.Runner.Opencode.Model = flags.Model
		}
		if len(flags.Include) > 0 {
			cfg.Runner.IncludeProjects = append(cfg.Runner.IncludeProjects, flags.Include...)
		}
		if flags.Executor != "" {
			cfg.Runner.DefaultExecutor = flags.Executor
		}
		if flags.PiBin != "" {
			cfg.Runner.Pi.Bin = flags.PiBin
		}
		if flags.PiModel != "" {
			cfg.Runner.Pi.Model = flags.PiModel
		}
		if flags.PiThinking != "" {
			cfg.Runner.Pi.Thinking = flags.PiThinking
		}
		if len(flags.Exclude) > 0 {
			cfg.Runner.ExcludeProjects = append(cfg.Runner.ExcludeProjects, flags.Exclude...)
		}
		if len(flags.FeatureIDs) > 0 {
			cfg.Runner.FeatureIDs = flags.FeatureIDs
		}
	}
}

// LifecycleFlags holds flags for lifecycle commands (start, stop, restart).
type LifecycleFlags struct {
	PIDFile       string
	LogFile       string
	Timeout       int
	Force         bool
	DryRun        bool
	Daemon        bool
	Port          int
	Host          string
	Runner        bool
	RunnerProject string
	MaxParallel   int
	Include       []string
	Exclude       []string
	Executor      string
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
		case "--daemon", "-d":
			flags.Daemon = true
		case "--port", "-p":
			if i+1 < len(args) {
				port := 0
				if _, err := fmt.Sscanf(args[i+1], "%d", &port); err == nil {
					flags.Port = port
				}
				i++
			}
		case "--host":
			if i+1 < len(args) {
				flags.Host = args[i+1]
				i++
			}
		case "--runner":
			flags.Runner = true
		case "--runner-project":
			if i+1 < len(args) {
				flags.RunnerProject = args[i+1]
				i++
			}
		case "--max-parallel":
			if i+1 < len(args) {
				maxParallel := 0
				if _, err := fmt.Sscanf(args[i+1], "%d", &maxParallel); err == nil {
					flags.MaxParallel = maxParallel
				}
				i++
			}
		case "--include", "-i":
			if i+1 < len(args) {
				flags.Include = append(flags.Include, args[i+1])
				i++
			}
		case "--exclude", "-e":
			if i+1 < len(args) {
				flags.Exclude = append(flags.Exclude, args[i+1])
				i++
			}
		case "--executor":
			if i+1 < len(args) {
				flags.Executor = args[i+1]
				i++
			}
		}
	}

	return flags, nil
}

// convertToCommandsLifecycleFlags converts main.LifecycleFlags to commands.LifecycleFlags.
func convertToCommandsLifecycleFlags(flags *LifecycleFlags) *commands.LifecycleFlags {
	return &commands.LifecycleFlags{
		PIDFile:       flags.PIDFile,
		LogFile:       flags.LogFile,
		Timeout:       flags.Timeout,
		Force:         flags.Force,
		DryRun:        flags.DryRun,
		Daemon:        flags.Daemon,
		Port:          flags.Port,
		Host:          flags.Host,
		Runner:        flags.Runner,
		RunnerProject: flags.RunnerProject,
		MaxParallel:   flags.MaxParallel,
		Include:       flags.Include,
		Exclude:       flags.Exclude,
		Executor:      flags.Executor,
	}
}

// DreamFlags for dream command
type DreamFlags struct {
	Enable   bool
	Disable  bool
	Now      bool
	Schedule string
}

// ParseDreamFlags parses dream command flags from args.
func ParseDreamFlags(args []string) (*DreamFlags, error) {
	flags := &DreamFlags{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--enable":
			flags.Enable = true
		case "--disable":
			flags.Disable = true
		case "--now":
			flags.Now = true
		case "--schedule":
			if i+1 < len(args) {
				flags.Schedule = args[i+1]
				i++
			}
		}
	}

	return flags, nil
}

// convertToCommandsDreamFlags converts main.DreamFlags to commands.DreamFlags.
func convertToCommandsDreamFlags(flags *DreamFlags) *commands.DreamFlags {
	return &commands.DreamFlags{
		Enable:   flags.Enable,
		Disable:  flags.Disable,
		Now:      flags.Now,
		Schedule: flags.Schedule,
	}
}

// ParseInitFlags parses init command flags from args.
func ParseInitFlags(args []string) (*InitFlags, error) {
	flags := &InitFlags{}

	for _, arg := range args {
		switch arg {
		case "--force", "-f":
			flags.Force = true
		case "--dry-run":
			flags.DryRun = true
		}
	}

	return flags, nil
}

// convertToCommandsInitFlags converts main.InitFlags to commands.InitFlags.
func convertToCommandsInitFlags(flags *InitFlags) *commands.InitFlags {
	return &commands.InitFlags{
		Force:  flags.Force,
		DryRun: flags.DryRun,
	}
}

// DoctorFlags for doctor command
type DoctorFlags struct {
	Fix              bool
	Force            bool
	DryRun           bool
	Verbose          bool
	SkipVersionCheck bool
}

// ParseDoctorFlags parses doctor command flags from args.
func ParseDoctorFlags(args []string) (*DoctorFlags, error) {
	flags := &DoctorFlags{}

	for _, arg := range args {
		switch arg {
		case "--fix":
			flags.Fix = true
		case "--force", "-f":
			flags.Force = true
		case "--dry-run":
			flags.DryRun = true
		case "--verbose", "-v":
			flags.Verbose = true
		case "--skip-version-check":
			flags.SkipVersionCheck = true
		}
	}

	return flags, nil
}

// convertToCommandsDoctorFlags converts main.DoctorFlags to commands.DoctorFlags.
func convertToCommandsDoctorFlags(flags *DoctorFlags) *commands.DoctorFlags {
	return &commands.DoctorFlags{
		Fix:              flags.Fix,
		Force:            flags.Force,
		DryRun:           flags.DryRun,
		Verbose:          flags.Verbose,
		SkipVersionCheck: flags.SkipVersionCheck,
	}
}

// EntrySaveFlags for brain save command
type EntrySaveFlags struct {
	Type      string
	Title     string
	Content   string
	NoEdit    bool
	Tags      string
	Status    string
	Priority  string
	DependsOn string
	FeatureID string
	Global    bool
	Project   string
}

// ParseEntrySaveFlags parses entry save command flags from args.
func ParseEntrySaveFlags(args []string) (*EntrySaveFlags, error) {
	flags := &EntrySaveFlags{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--type":
			if i+1 < len(args) {
				flags.Type = args[i+1]
				i++
			}
		case "--title":
			if i+1 < len(args) {
				flags.Title = args[i+1]
				i++
			}
		case "--content":
			if i+1 < len(args) {
				flags.Content = args[i+1]
				i++
			}
		case "--no-edit":
			flags.NoEdit = true
		case "--tags":
			if i+1 < len(args) {
				flags.Tags = args[i+1]
				i++
			}
		case "--status":
			if i+1 < len(args) {
				flags.Status = args[i+1]
				i++
			}
		case "--priority":
			if i+1 < len(args) {
				flags.Priority = args[i+1]
				i++
			}
		case "--depends-on":
			if i+1 < len(args) {
				flags.DependsOn = args[i+1]
				i++
			}
		case "--feature-id":
			if i+1 < len(args) {
				flags.FeatureID = args[i+1]
				i++
			}
		case "--global":
			flags.Global = true
		case "--project":
			if i+1 < len(args) {
				flags.Project = args[i+1]
				i++
			}
		}
	}

	return flags, nil
}

// convertToCommandsEntrySaveFlags converts main.EntrySaveFlags to commands.EntrySaveFlags.
func convertToCommandsEntrySaveFlags(flags *EntrySaveFlags) *commands.EntrySaveFlags {
	return &commands.EntrySaveFlags{
		Type:      flags.Type,
		Title:     flags.Title,
		Content:   flags.Content,
		NoEdit:    flags.NoEdit,
		Tags:      flags.Tags,
		Status:    flags.Status,
		Priority:  flags.Priority,
		DependsOn: flags.DependsOn,
		FeatureID: flags.FeatureID,
		Global:    flags.Global,
		Project:   flags.Project,
	}
}

// convertToCommandsPluginFlags converts main.PluginFlags to commands.PluginFlags.
func convertToCommandsPluginFlags(flags *PluginFlags) *commands.PluginFlags {
	return &commands.PluginFlags{
		Force:  flags.Force,
		DryRun: flags.DryRun,
		APIURL: flags.APIURL,
	}
}

// EntryGetFlags holds flags for the brain get command.
type EntryGetFlags struct {
	Format  string // --format (path, id, short, full, json, jsonl, or Go template)
	Quiet   bool   // -q, --quiet
	NoColor bool   // --no-color
}

// ParseEntryGetFlags parses entry get command flags and the positional id-or-path argument.
// Returns the flags, the positional argument (id-or-path), and any error.
func ParseEntryGetFlags(args []string) (*EntryGetFlags, string, error) {
	flags := &EntryGetFlags{}
	var idOrPath string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		case "-q", "--quiet":
			flags.Quiet = true
		case "--no-color":
			flags.NoColor = true
		default:
			// First non-flag argument is the id-or-path
			if !isFlag(arg) && idOrPath == "" {
				idOrPath = arg
			}
		}
	}

	return flags, idOrPath, nil
}

// convertToCommandsEntryGetFlags converts main.EntryGetFlags to commands.EntryGetFlags.
func convertToCommandsEntryGetFlags(flags *EntryGetFlags) *commands.EntryGetFlags {
	return &commands.EntryGetFlags{
		Format:  flags.Format,
		Quiet:   flags.Quiet,
		NoColor: flags.NoColor,
	}
}

// EntryUpdateFlags for the "brain update" command.
type EntryUpdateFlags struct {
	Status    string
	Title     string
	Content   string
	Append    string
	Note      string
	Tags      string
	Priority  string
	DependsOn string
	FeatureID string
}

// ParseEntryUpdateFlags parses entry update flags from args.
// Returns the flags and the positional ID/path argument.
func ParseEntryUpdateFlags(args []string) (*EntryUpdateFlags, string, error) {
	flags := &EntryUpdateFlags{}

	// Extract positional arg (ID or path) — first non-flag argument
	idOrPath := ""
	flagArgs := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !isFlag(arg) && idOrPath == "" {
			idOrPath = arg
			continue
		}
		flagArgs = append(flagArgs, arg)
	}

	// Parse flags from remaining args
	for i := 0; i < len(flagArgs); i++ {
		arg := flagArgs[i]
		switch arg {
		case "--status":
			if i+1 < len(flagArgs) {
				flags.Status = flagArgs[i+1]
				i++
			}
		case "--title":
			if i+1 < len(flagArgs) {
				flags.Title = flagArgs[i+1]
				i++
			}
		case "--content":
			if i+1 < len(flagArgs) {
				flags.Content = flagArgs[i+1]
				i++
			}
		case "--append":
			if i+1 < len(flagArgs) {
				flags.Append = flagArgs[i+1]
				i++
			}
		case "--note":
			if i+1 < len(flagArgs) {
				flags.Note = flagArgs[i+1]
				i++
			}
		case "--tags":
			if i+1 < len(flagArgs) {
				flags.Tags = flagArgs[i+1]
				i++
			}
		case "--priority":
			if i+1 < len(flagArgs) {
				flags.Priority = flagArgs[i+1]
				i++
			}
		case "--depends-on":
			if i+1 < len(flagArgs) {
				flags.DependsOn = flagArgs[i+1]
				i++
			}
		case "--feature-id":
			if i+1 < len(flagArgs) {
				flags.FeatureID = flagArgs[i+1]
				i++
			}
		}
	}

	return flags, idOrPath, nil
}

// convertToCommandsEntryUpdateFlags converts main.EntryUpdateFlags to commands.EntryUpdateFlags.
func convertToCommandsEntryUpdateFlags(flags *EntryUpdateFlags) *commands.EntryUpdateFlags {
	return &commands.EntryUpdateFlags{
		Status:    flags.Status,
		Title:     flags.Title,
		Content:   flags.Content,
		Append:    flags.Append,
		Note:      flags.Note,
		Tags:      flags.Tags,
		Priority:  flags.Priority,
		DependsOn: flags.DependsOn,
		FeatureID: flags.FeatureID,
	}
}

// AttachmentFlags holds flags for the brain attachments command.
type AttachmentFlags struct {
	Project          string
	Entry            string
	Role             string
	Description      string
	Output           string
	Format           string
	Quiet            bool
	DryRun           bool
	Force            bool
	SkipReady        bool
	BatchSize        int
	RateLimitDelayMs int
}

// ParseAttachmentFlags parses attachment subcommand flags and returns positional args.
func ParseAttachmentFlags(args []string) (*AttachmentFlags, []string, error) {
	flags := &AttachmentFlags{}
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--project", "-p":
			if i+1 < len(args) {
				flags.Project = args[i+1]
				i++
			}
		case "--entry", "-e":
			if i+1 < len(args) {
				flags.Entry = args[i+1]
				i++
			}
		case "--role", "-r":
			if i+1 < len(args) {
				flags.Role = args[i+1]
				i++
			}
		case "--description", "--caption", "-d":
			if i+1 < len(args) {
				flags.Description = args[i+1]
				i++
			}
		case "--output", "-o":
			if i+1 < len(args) {
				flags.Output = args[i+1]
				i++
			}
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		case "-q", "--quiet":
			flags.Quiet = true
		case "--dry-run":
			flags.DryRun = true
		case "--force":
			flags.Force = true
			flags.SkipReady = false
		case "--skip-ready":
			flags.SkipReady = true
			flags.Force = false
		case "--batch-size", "--batch":
			if i+1 < len(args) {
				value, err := strconv.Atoi(args[i+1])
				if err != nil {
					return nil, nil, fmt.Errorf("%s must be an integer", arg)
				}
				flags.BatchSize = value
				i++
			}
		case "--rate-limit-ms", "--rate-limit-delay-ms":
			if i+1 < len(args) {
				value, err := strconv.Atoi(args[i+1])
				if err != nil {
					return nil, nil, fmt.Errorf("%s must be an integer", arg)
				}
				flags.RateLimitDelayMs = value
				i++
			}
		default:
			if !isFlag(arg) {
				positionals = append(positionals, arg)
			}
		}
	}
	return flags, positionals, nil
}

func convertToCommandsAttachmentFlags(flags *AttachmentFlags) *commands.AttachmentFlags {
	return &commands.AttachmentFlags{
		Project:          flags.Project,
		Entry:            flags.Entry,
		Role:             flags.Role,
		Description:      flags.Description,
		Output:           flags.Output,
		Format:           flags.Format,
		Quiet:            flags.Quiet,
		DryRun:           flags.DryRun,
		Force:            flags.Force,
		SkipReady:        flags.SkipReady,
		BatchSize:        flags.BatchSize,
		RateLimitDelayMs: flags.RateLimitDelayMs,
	}
}

// EntrySearchFlags holds flags for the brain search command (main package mirror).
type EntrySearchFlags struct {
	Type        string
	Status      string
	Tags        string
	Priority    string
	FeatureID   string
	Limit       int
	Sort        string
	Format      string
	Quiet       bool
	NoColor     bool
	NulDelim    bool
	Interactive bool
}

// ParseEntrySearchFlags parses brain search flags.
func ParseEntrySearchFlags(args []string) (*EntrySearchFlags, error) {
	flags := &EntrySearchFlags{Limit: 20}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--type":
			if i+1 < len(args) {
				flags.Type = args[i+1]
				i++
			}
		case "--status":
			if i+1 < len(args) {
				flags.Status = args[i+1]
				i++
			}
		case "--tags":
			if i+1 < len(args) {
				flags.Tags = args[i+1]
				i++
			}
		case "--priority":
			if i+1 < len(args) {
				flags.Priority = args[i+1]
				i++
			}
		case "--feature-id":
			if i+1 < len(args) {
				flags.FeatureID = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				limit := 20
				// A malformed value deliberately leaves the default above in place.
				_, _ = fmt.Sscanf(args[i+1], "%d", &limit)
				flags.Limit = limit
				i++
			}
		case "--sort":
			if i+1 < len(args) {
				flags.Sort = args[i+1]
				i++
			}
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		case "-q", "--quiet":
			flags.Quiet = true
		case "--no-color":
			flags.NoColor = true
		case "-0":
			flags.NulDelim = true
		case "-i", "--interactive":
			flags.Interactive = true
		}
	}

	return flags, nil
}

// convertToCommandsEntrySearchFlags converts main.EntrySearchFlags to commands.EntrySearchFlags.
func convertToCommandsEntrySearchFlags(flags *EntrySearchFlags) *commands.EntrySearchFlags {
	f := &commands.EntrySearchFlags{
		Interactive: flags.Interactive,
	}
	f.Filter.Type = flags.Type
	f.Filter.Status = flags.Status
	f.Filter.Tags = flags.Tags
	f.Filter.Priority = flags.Priority
	f.Filter.FeatureID = flags.FeatureID
	f.Filter.Limit = flags.Limit
	f.Filter.Sort = flags.Sort
	f.Output.Format = flags.Format
	f.Output.Quiet = flags.Quiet
	f.Output.NoColor = flags.NoColor
	if flags.NulDelim {
		f.Output.Delimiter = "\x00"
	} else {
		f.Output.Delimiter = "\n"
	}
	return f
}

// EntryListFlags holds flags for the brain list command (main package mirror).
type EntryListFlags struct {
	Type        string
	Status      string
	Tags        string
	Priority    string
	FeatureID   string
	Limit       int
	Sort        string
	Match       string
	Format      string
	Quiet       bool
	NoColor     bool
	NulDelim    bool
	Interactive bool
}

// ParseEntryListFlags parses brain list flags.
func ParseEntryListFlags(args []string) (*EntryListFlags, error) {
	flags := &EntryListFlags{Limit: 20}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--type":
			if i+1 < len(args) {
				flags.Type = args[i+1]
				i++
			}
		case "--status":
			if i+1 < len(args) {
				flags.Status = args[i+1]
				i++
			}
		case "--tags":
			if i+1 < len(args) {
				flags.Tags = args[i+1]
				i++
			}
		case "--priority":
			if i+1 < len(args) {
				flags.Priority = args[i+1]
				i++
			}
		case "--feature-id":
			if i+1 < len(args) {
				flags.FeatureID = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				limit := 20
				// A malformed value deliberately leaves the default above in place.
				_, _ = fmt.Sscanf(args[i+1], "%d", &limit)
				flags.Limit = limit
				i++
			}
		case "--sort":
			if i+1 < len(args) {
				flags.Sort = args[i+1]
				i++
			}
		case "-m", "--match":
			if i+1 < len(args) {
				flags.Match = args[i+1]
				i++
			}
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		case "-q", "--quiet":
			flags.Quiet = true
		case "--no-color":
			flags.NoColor = true
		case "-0":
			flags.NulDelim = true
		case "-i", "--interactive":
			flags.Interactive = true
		}
	}

	return flags, nil
}

// EntryEditFlags holds flags for the brain edit command (main package mirror).
type EntryEditFlags struct {
	Type        string
	Status      string
	Tags        string
	Priority    string
	FeatureID   string
	Limit       int
	Interactive bool
	Force       bool
	NoColor     bool
	Quiet       bool
	Format      string
}

// ParseEntryEditFlags parses brain edit flags and the positional id-or-path argument.
// Returns the flags, the positional argument (id-or-path), and any error.
func ParseEntryEditFlags(args []string) (*EntryEditFlags, string, error) {
	flags := &EntryEditFlags{Limit: 20}
	var idOrPath string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--type":
			if i+1 < len(args) {
				flags.Type = args[i+1]
				i++
			}
		case "--status":
			if i+1 < len(args) {
				flags.Status = args[i+1]
				i++
			}
		case "--tags":
			if i+1 < len(args) {
				flags.Tags = args[i+1]
				i++
			}
		case "--priority":
			if i+1 < len(args) {
				flags.Priority = args[i+1]
				i++
			}
		case "--feature-id":
			if i+1 < len(args) {
				flags.FeatureID = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				limit := 20
				// A malformed value deliberately leaves the default above in place.
				_, _ = fmt.Sscanf(args[i+1], "%d", &limit)
				flags.Limit = limit
				i++
			}
		case "-i", "--interactive":
			flags.Interactive = true
		case "--force":
			flags.Force = true
		case "--no-color":
			flags.NoColor = true
		case "-q", "--quiet":
			flags.Quiet = true
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		default:
			// First non-flag argument is the id-or-path
			if !isFlag(arg) && idOrPath == "" {
				idOrPath = arg
			}
		}
	}

	return flags, idOrPath, nil
}

// convertToCommandsEntryEditFlags converts main.EntryEditFlags to commands.EntryEditFlags.
func convertToCommandsEntryEditFlags(flags *EntryEditFlags) *commands.EntryEditFlags {
	f := &commands.EntryEditFlags{
		Interactive: flags.Interactive,
		Force:       flags.Force,
		NoColor:     flags.NoColor,
		Quiet:       flags.Quiet,
		Format:      flags.Format,
	}
	f.Filter.Type = flags.Type
	f.Filter.Status = flags.Status
	f.Filter.Tags = flags.Tags
	f.Filter.Priority = flags.Priority
	f.Filter.FeatureID = flags.FeatureID
	f.Filter.Limit = flags.Limit
	return f
}

// AutomationFlags for automation command
type AutomationFlags struct {
	Project string
	Format  string
	Limit   int
	Quiet   bool
}

// ParseAutomationFlags parses automation command flags from args.
func ParseAutomationFlags(args []string) (*AutomationFlags, error) {
	flags := &AutomationFlags{Limit: 20}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--project":
			if i+1 < len(args) {
				flags.Project = args[i+1]
				i++
			}
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				limit := 20
				// A malformed value deliberately leaves the default above in place.
				_, _ = fmt.Sscanf(args[i+1], "%d", &limit)
				flags.Limit = limit
				i++
			}
		case "-q", "--quiet":
			flags.Quiet = true
		}
	}

	return flags, nil
}

// convertToCommandsAutomationFlags converts main.AutomationFlags to commands.AutomationFlags.
func convertToCommandsAutomationFlags(flags *AutomationFlags) *commands.AutomationFlags {
	return &commands.AutomationFlags{
		Project: flags.Project,
		Format:  flags.Format,
		Limit:   flags.Limit,
		Quiet:   flags.Quiet,
	}
}

// GoalFlags for the `automation goal` subcommands.
type GoalFlags struct {
	Project       string   // --project
	Feature       string   // --feature
	Title         string   // --title
	Content       string   // --content
	TriggerSource string   // --trigger-source (task|feature|both)
	SessionMode   string   // --session-mode (continue|fresh)
	Agent         string   // --agent
	Model         string   // --model
	Executor      string   // --executor
	Workdir       string   // --workdir
	Status        string   // --status
	Criteria      []string // --criteria (repeatable)
	Validate      []string // --validate (repeatable)
	Format        string   // --format (json|table)
	Limit         int      // --limit
	Quiet         bool     // -q, --quiet
}

// ParseAutomationGoalFlags parses `automation goal` command flags from args.
func ParseAutomationGoalFlags(args []string) (*GoalFlags, error) {
	flags := &GoalFlags{Limit: 20}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--project":
			if i+1 < len(args) {
				flags.Project = args[i+1]
				i++
			}
		case "--feature":
			if i+1 < len(args) {
				flags.Feature = args[i+1]
				i++
			}
		case "--title":
			if i+1 < len(args) {
				flags.Title = args[i+1]
				i++
			}
		case "--content":
			if i+1 < len(args) {
				flags.Content = args[i+1]
				i++
			}
		case "--trigger-source":
			if i+1 < len(args) {
				flags.TriggerSource = args[i+1]
				i++
			}
		case "--session-mode":
			if i+1 < len(args) {
				flags.SessionMode = args[i+1]
				i++
			}
		case "--agent":
			if i+1 < len(args) {
				flags.Agent = args[i+1]
				i++
			}
		case "--model":
			if i+1 < len(args) {
				flags.Model = args[i+1]
				i++
			}
		case "--executor":
			if i+1 < len(args) {
				flags.Executor = args[i+1]
				i++
			}
		case "--workdir":
			if i+1 < len(args) {
				flags.Workdir = args[i+1]
				i++
			}
		case "--status":
			if i+1 < len(args) {
				flags.Status = args[i+1]
				i++
			}
		case "--criteria":
			if i+1 < len(args) {
				flags.Criteria = append(flags.Criteria, args[i+1])
				i++
			}
		case "--validate":
			if i+1 < len(args) {
				flags.Validate = append(flags.Validate, args[i+1])
				i++
			}
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(args) {
				limit := 20
				// A malformed value deliberately leaves the default above in place.
				_, _ = fmt.Sscanf(args[i+1], "%d", &limit)
				flags.Limit = limit
				i++
			}
		case "-q", "--quiet":
			flags.Quiet = true
		}
	}

	return flags, nil
}

// convertToCommandsGoalFlags converts main.GoalFlags to commands.GoalFlags.
func convertToCommandsGoalFlags(flags *GoalFlags) *commands.GoalFlags {
	return &commands.GoalFlags{
		Project:       flags.Project,
		Feature:       flags.Feature,
		Title:         flags.Title,
		Content:       flags.Content,
		TriggerSource: flags.TriggerSource,
		SessionMode:   flags.SessionMode,
		Agent:         flags.Agent,
		Model:         flags.Model,
		Executor:      flags.Executor,
		Workdir:       flags.Workdir,
		Status:        flags.Status,
		Criteria:      flags.Criteria,
		Validate:      flags.Validate,
		Format:        flags.Format,
		Limit:         flags.Limit,
		Quiet:         flags.Quiet,
	}
}

// MigrateFlags for migrate command
type MigrateFlags struct {
	DryRun  bool
	Force   bool
	Format  string
	Project string
}

// ParseMigrateFlags parses migrate command flags from args.
func ParseMigrateFlags(args []string) (*MigrateFlags, error) {
	flags := &MigrateFlags{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--dry-run":
			flags.DryRun = true
		case "--force":
			flags.Force = true
		case "--format":
			if i+1 < len(args) {
				flags.Format = args[i+1]
				i++
			}
		case "--project":
			if i+1 < len(args) {
				flags.Project = args[i+1]
				i++
			}
		}
	}

	return flags, nil
}

// convertToCommandsMigrateFlags converts main.MigrateFlags to commands.MigrateFlags.
func convertToCommandsMigrateFlags(flags *MigrateFlags) *commands.MigrateFlags {
	return &commands.MigrateFlags{
		DryRun:  flags.DryRun,
		Force:   flags.Force,
		Format:  flags.Format,
		Project: flags.Project,
	}
}

// convertToCommandsEntryListFlags converts main.EntryListFlags to commands.EntryListFlags.
func convertToCommandsEntryListFlags(flags *EntryListFlags) *commands.EntryListFlags {
	f := &commands.EntryListFlags{
		Interactive: flags.Interactive,
	}
	f.Filter.Type = flags.Type
	f.Filter.Status = flags.Status
	f.Filter.Tags = flags.Tags
	f.Filter.Priority = flags.Priority
	f.Filter.FeatureID = flags.FeatureID
	f.Filter.Limit = flags.Limit
	f.Filter.Sort = flags.Sort
	f.Filter.Match = flags.Match
	f.Output.Format = flags.Format
	f.Output.Quiet = flags.Quiet
	f.Output.NoColor = flags.NoColor
	if flags.NulDelim {
		f.Output.Delimiter = "\x00"
	} else {
		f.Output.Delimiter = "\n"
	}
	return f
}

// EmbeddingsFlags for embeddings command
type EmbeddingsFlags struct {
	Project string
	Path    string
	All     bool
	Force   bool
	DryRun  bool
	Verbose bool
}

// ParseEmbeddingsFlags parses embeddings command flags from args.
func ParseEmbeddingsFlags(args []string) (*EmbeddingsFlags, error) {
	flags := &EmbeddingsFlags{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--project":
			if i+1 < len(args) {
				flags.Project = args[i+1]
				i++
			}
		case "--path":
			if i+1 < len(args) {
				flags.Path = args[i+1]
				i++
			}
		case "--all":
			flags.All = true
		case "--force":
			flags.Force = true
		case "--dry-run":
			flags.DryRun = true
		case "-v", "--verbose":
			flags.Verbose = true
		}
	}

	return flags, nil
}

// convertToCommandsEmbeddingsFlags converts main.EmbeddingsFlags to commands.EmbeddingsFlags.
func convertToCommandsEmbeddingsFlags(flags *EmbeddingsFlags) *commands.EmbeddingsFlags {
	return &commands.EmbeddingsFlags{
		Project: flags.Project,
		Path:    flags.Path,
		All:     flags.All,
		Force:   flags.Force,
		DryRun:  flags.DryRun,
		Verbose: flags.Verbose,
	}
}
