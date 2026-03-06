package main

import (
	"fmt"
)

const mainHelp = `brain - Unified Brain API CLI

A single command for server, runner, MCP, and management operations.

USAGE:
  brain [global-flags] <command> [command-flags] [args]

ZERO-ARG SHORTHAND:
  brain                           Start runner TUI for all projects
  brain <project>                 Start runner TUI for specific project
  brain all --max-parallel 5      Start runner TUI for all projects with flags

SERVER MODE:
  brain server                    Start API server (foreground)
  brain server --daemon           Start API server (background)
  brain server --port 3000        Start on custom port

RUNNER MODE:
  brain run <project>             Start runner for project
  brain run start <project>       Start runner
  brain run stop <project>        Stop runner
  brain run status <project>      Show runner status
  brain run logs [-f]             Show runner logs
  brain run list <project>        List all tasks
  brain run ready <project>       List ready tasks
  brain run features <project>    List features
  brain run config                Show runner config

MCP MODE:
  brain mcp                       Start MCP stdio server

SERVER LIFECYCLE:
  brain start                     Start server daemon (legacy compat)
  brain stop                      Stop server daemon
  brain restart                   Restart server
  brain status                    Check server status
  brain health                    Query /health endpoint
  brain logs [-f]                 Show/follow server logs
  brain dev                       Start server in foreground

SETUP & CONFIG:
  brain init [--force]            Initialize ~/.brain directory
  brain doctor [-v] [--fix]       Diagnose configuration
  brain config                    Show current configuration

PLUGIN MANAGEMENT:
  brain install <target>          Install plugin to AI assistant
  brain uninstall <target>        Remove plugin from target
  brain plugin-status             Show installation status

TOKEN MANAGEMENT:
  brain token create --name <n>   Create API token
  brain token list                List API tokens
  brain token revoke <name>       Revoke API token

GLOBAL FLAGS:
  -h, --help                      Show help
  -v, --verbose                   Verbose output
  --version                       Show version

EXAMPLES:
  # Start runner TUI for all projects
  brain

  # Start runner TUI for specific project
  brain myproject

  # Start API server
  brain server

  # Start API server as daemon on custom port
  brain server --daemon --port 3000

  # Initialize brain directory
  brain init

  # Install to OpenCode
  brain install opencode

  # Create API token
  brain token create --name dev

For command-specific help: brain <command> --help
`

const serverHelp = `brain server - Start the Brain API server

USAGE:
  brain server [flags]

FLAGS:
  -p, --port <port>              Server port (default: 3333)
  --host <host>                  Server host (default: localhost)
  -d, --daemon                   Run as background daemon
  --log-file <path>              Log file path
  --tls                          Enable HTTPS
  --tls-cert <path>              TLS certificate path
  --tls-key <path>               TLS private key path

ENVIRONMENT:
  BRAIN_PORT                     Server port
  BRAIN_DIR                      Brain data directory (default: ~/.brain)
  BRAIN_API_URL                  API URL for clients

EXAMPLES:
  # Foreground server
  brain server

  # Background daemon
  brain server --daemon

  # Custom port
  brain server --port 3000

  # HTTPS mode
  brain server --tls --tls-cert cert.pem --tls-key key.pem

LEGACY COMPATIBILITY:
  brain-api [flags]              → brain server [flags]
  brain start                    → brain server --daemon
  brain stop                     → Stop daemon
`

const runnerHelp = `brain run - Start the task runner

USAGE:
  brain <project>                     Start TUI for project (shorthand)
  brain run <project> [flags]         Start runner for project
  brain run start <project>           Start runner
  brain run stop <project>            Stop runner
  brain run status <project>          Show runner status

FLAGS:
  --tui                          Interactive TUI (default for shorthand)
  -f, --foreground               Foreground without TUI
  -b, --background               Background daemon
  -p, --max-parallel <N>         Max concurrent tasks (default: 3)
  --poll-interval <N>            Poll interval seconds (default: 5)
  -w, --workdir <dir>            Working directory
  --agent <name>                 OpenCode agent to use
  -m, --model <name>             Model to use
  -i, --include <pattern>        Include project pattern (repeatable)
  -e, --exclude <pattern>        Exclude project pattern (repeatable)

EXAMPLES:
  # TUI for all projects
  brain

  # TUI for specific project
  brain myproject

  # Foreground without TUI
  brain run myproject --foreground

  # All projects with filtering
  brain all -i 'prod-*' -e 'test-*'

  # Custom concurrency
  brain myproject --max-parallel 5

LEGACY COMPATIBILITY:
  brain-runner <project>         → brain <project>
  brain-runner start <project>   → brain run start <project>
`

const mcpHelp = `brain mcp - Start the MCP (Model Context Protocol) server

USAGE:
  brain mcp [flags]

FLAGS:
  --api-url <url>                Brain API URL (default: http://localhost:3333)

ENVIRONMENT:
  BRAIN_API_URL                  API URL to connect to

EXAMPLES:
  # Start MCP server
  brain mcp

  # Custom API URL
  brain mcp --api-url http://localhost:3000

CONFIGURATION:
  Add to your MCP client config (~/.config/claude/config.json):
  {
    "mcpServers": {
      "brain": {
        "command": "brain",
        "args": ["mcp"],
        "env": {
          "BRAIN_API_URL": "http://localhost:3333"
        }
      }
    }
  }

LEGACY COMPATIBILITY:
  brain-mcp [flags]              → brain mcp [flags]
`

const initHelp = `brain init - Initialize the Brain directory structure

USAGE:
  brain init [flags]

FLAGS:
  --force                        Overwrite existing configuration

EXAMPLES:
  # Initialize brain directory
  brain init

  # Force reinitialize
  brain init --force
`

const doctorHelp = `brain doctor - Diagnose Brain configuration issues

USAGE:
  brain doctor [flags]

FLAGS:
  -v, --verbose                  Show detailed diagnostic information
  --fix                          Attempt to fix detected issues

EXAMPLES:
  # Basic health check
  brain doctor

  # Detailed diagnostics
  brain doctor -v

  # Diagnose and attempt fixes
  brain doctor --fix
`

const installHelp = `brain install - Install Brain plugin to AI assistant

USAGE:
  brain install <target>

TARGETS:
  opencode                       Install to OpenCode
  cursor                         Install to Cursor
  windsurf                       Install to Windsurf

EXAMPLES:
  # Install to OpenCode
  brain install opencode

  # Check installation status
  brain plugin-status
`

const tokenHelp = `brain token - Manage API authentication tokens

USAGE:
  brain token <subcommand> [flags]

SUBCOMMANDS:
  create --name <name>           Create a new API token
  list                           List all API tokens
  revoke <name>                  Revoke an API token

EXAMPLES:
  # Create token
  brain token create --name dev

  # List tokens
  brain token list

  # Revoke token
  brain token revoke dev
`

// ShowHelp displays help based on command
func ShowHelp(command string) {
	switch command {
	case "":
		fmt.Print(mainHelp)
	case "server":
		fmt.Print(serverHelp)
	case "run", "runner":
		fmt.Print(runnerHelp)
	case "mcp":
		fmt.Print(mcpHelp)
	case "init":
		fmt.Print(initHelp)
	case "doctor":
		fmt.Print(doctorHelp)
	case "install":
		fmt.Print(installHelp)
	case "token", "tokens":
		fmt.Print(tokenHelp)
	default:
		fmt.Printf("No help available for command: %s\n\n", command)
		fmt.Print(mainHelp)
	}
}
