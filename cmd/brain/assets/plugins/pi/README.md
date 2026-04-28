# Brain Pi Plugin Assets

Agent bundles and extensions for the [Pi](https://pi.dev) AI coding agent.

## Structure

```
pi/
  brain-agents/
    tdd-dev/          # TDD-focused development agent
      config.json     # Agent configuration
      system-prompt.md # System prompt
      extension.ts    # TDD enforcement extension
    explore/          # Code exploration agent
      config.json     # Agent configuration
      system-prompt.md # System prompt
      extension.ts    # Read-only enforcement extension
  extensions/
    brain-tools.ts    # Brain API integration tools
```

## Installation

```bash
brain install pi           # Install to ~/.pi/
brain install pi --force   # Overwrite existing files
brain install pi --dry-run # Preview what would be installed
brain uninstall pi         # Remove installed files
brain plugin-status        # Check installation status
```

## Agent Bundles

### tdd-dev

Test-driven development agent with enforcement. Blocks file edits that violate the RED-GREEN-REFACTOR-VERIFY cycle.

### explore

Read-only code exploration agent. Blocks file writes and edits to prevent accidental modifications.

## Extensions

### brain-tools.ts

Provides Brain API integration tools (`brain_recall`, `brain_save`, `brain_search`, etc.) for use within Pi agents.
