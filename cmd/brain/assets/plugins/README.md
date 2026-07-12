# Brain Plugin Assets

This directory contains plugin files that are embedded into the brain CLI binary and installed to target environments (OpenCode, Claude Code).

## Directory Structure

```
plugins/
├── opencode/
│   ├── brain-planning.ts  # Planning enforcement plugin
│   ├── agent/             # Brain-aware OpenCode agents
│   ├── command/           # Brain slash commands
│   ├── skill/             # Brain skills (shared — also installed for Claude Code)
│   └── README.md          # OpenCode plugin documentation
└── README.md              # This file
```

The skills under `opencode/skill/` are the canonical, target-agnostic brain
skills (SKILL.md with `name`/`description` frontmatter). `brain install claude`
installs the same skills into `~/.claude/skills/<name>/SKILL.md` for Claude
Code via `assets.ListSharedSkillFiles()`.

> **Note:** The previous `brain.ts` API client plugin has been removed.
> Brain tools are now exposed through the MCP stdio server (`brain mcp`)
> configured directly in OpenCode's MCP settings. See the project README
> for the OpenCode MCP config snippet.

## Plugin Format

Plugin files are TypeScript modules that follow the target's plugin specification:
- **OpenCode**: Uses `@opencode-ai/plugin` SDK
- **Claude Code**: TBD (future implementation)

## Template Placeholders

Plugin files may contain placeholders that are replaced during installation:
- `{{API_URL}}` - Brain API URL (default: http://localhost:3333)
- `{{GENERATED_DATE}}` - Installation timestamp

## Usage

Plugins are embedded at compile time using go:embed and installed via:
```bash
brain install opencode    # Install to OpenCode (~/.config/opencode)
brain install claude      # Install brain skills to Claude Code (~/.claude/skills)
brain install pi          # Install Pi agent bundles (~/.pi)
brain plugin-status       # Check installation status
```
