# OpenCode Brain Assets

Resources installed by `brain install opencode` to make OpenCode work with Brain projects, plans, and task queues.

## Core Plugins

- `brain.ts` - Brain API tools for entries, tasks, automations, attachments, and project context.

## Agent

- `agent/brain-planner.md` - Coordination-only agent that converts plans into production-ready Brain task graphs and delegates implementation. It does not edit code directly.

## Skills

- `skill/brain-project-context` - Resolve the current workspace to a Brain project and load current project context.
- `skill/brain-project-planning` - Turn a plan into dependency-aware Brain tasks with production readiness checks, `feature_id`, worktree metadata, and queue verification.
- `skill/brain-runner-queue` - Execute queued Brain tasks with the appropriate depth and verification.
- `skill/feature-checkout` - Audit completed feature tasks against original requests and create follow-up tasks for gaps.
- `skill/using-brain` - General Brain usage patterns for memory, search, attachments, tasks, and automations.
- `skill/brain-automation` - Create durable scheduled or event-driven Brain automations.
- `skill/brain-memory` - Capture and naturally recall durable conversational memory using existing Brain entry types.

## Commands

- `command/plan-to-tasks.md` - Thin slash-command entry point that loads `brain-project-planning` and converts a plan into queued tasks.
- `command/do.md` - General Brain task creation and delegation workflow.
- `command/work.md` - Work on existing Brain tasks.

## Installation

Installed automatically via:

```bash
brain install opencode
```

Target location: `~/.config/opencode/`.
