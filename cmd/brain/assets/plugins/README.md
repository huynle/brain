# Brain Plugin Assets

This directory contains plugin files that are embedded into the brain CLI binary and installed to target environments (OpenCode, Claude Code).

## Directory Structure

```
plugins/
├── opencode/
│   ├── brain.ts           # Main brain API client plugin
│   ├── brain-planning.ts  # Planning enforcement plugin
│   └── README.md          # OpenCode plugin documentation (placeholder)
└── README.md              # This file
```

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
brain install opencode    # Install to OpenCode
brain status             # Check installation status
```
