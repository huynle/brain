---
name: brain-project-context
description: Use when starting a new session, resuming project work, or needing previous project context - resolves the current workspace through Brain's client registry and loads the latest project dream - provides consolidated context without user metadata prompts
---

# Brain Project Context

## Overview

**Core Principle:** Load durable project context before making assumptions.

Brain can resolve the current workspace to a project across hosts, worktrees, and checkout paths. The `project_context` tool registers the current Brain client and workspace automatically, resolves the project, and returns the latest `dream` entry for that project.

## When to Use

- At the start of a new coding session.
- When resuming work in an existing repository.
- When the user asks for previous context, project history, or what has been happening.
- Before planning or implementing work that may depend on prior decisions.
- When the current project name is ambiguous because of git worktrees or multiple machines.

## When NOT to Use

- For one-off questions that clearly do not need project context.
- When Brain API is unavailable and the user wants to proceed without memory.
- When already loaded in the current session and no workspace/project change occurred.

## Workflow

1. Call `project_context` before searching manually.
2. Read the returned project, confidence, source, and dream content.
3. Treat high-confidence dream content as the default project context.
4. If no dream exists, continue with normal exploration and consider saving a session summary later.
5. If confidence is low or the project looks wrong, use `search` with likely project names before relying on the result.

## Key Rules

- Do not ask the user to identify host, workspace, or project metadata unless resolution is clearly wrong and no safe fallback exists.
- Prefer the returned dream over scattered summaries because it is the consolidated project memory.
- Mention when no dream was found instead of inventing prior context.
- Do not overwrite or edit dream entries unless explicitly asked or running a dream consolidation workflow.

## Checklist

- [ ] Called `project_context` when prior context could matter.
- [ ] Used the resolved project ID for later Brain searches/saves.
- [ ] Incorporated the dream context into planning or implementation.
- [ ] Stated uncertainty if resolution confidence was low or no dream existed.
