---
name: brain-project-context
description: Use when starting a new session, resuming project work, or needing previous project context - resolves the current workspace to a Brain project and loads the latest project dream - provides consolidated context without user metadata prompts
---

# Brain Project Context

## Overview

**Core Principle:** Load durable project context before making assumptions.

Two steps, not one. `context_get` tells you WHICH project you are in;
`search` + `recall` load what is known about it. There is no single tool
that does both.

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

### 1. Resolve the project

```text
context_get()
```

Takes no arguments. Reports the ambient execution context this MCP server
resolved at startup:

- **Project** — the project every tool call defaults to when `project` is omitted.
- **Workdir**, **git remote**, **git branch**.
- The client/host identity stamped on tasks this server creates.

If it reports `⚠ COULD NOT DETERMINE`, the process is not running inside a
recognised git repository under your home directory. There is no safe
default: pass `project` explicitly on every subsequent call. It will not
guess from the directory name, deliberately — guessing is how unrelated
entries end up filed under a project named after a folder.

Note the project is resolved **from the server's own working directory**,
not from anything the client sends. Over the stdio transport (`brain mcp`)
that is your checkout. Over the HTTP transport it is the API host, which
is shared by every client — so on HTTP, treat the reported project as a
default to override, not as an answer.

### 2. Load the project dream

`context_get` does NOT return the dream. Load it yourself:

```text
search(query: "dream", project: "<project>", type: "dream")
recall(path: "<newest hit>")
```

The dream is the consolidated project memory — prefer it over scattered
summaries. If there is no dream, say so and continue with normal
exploration rather than inventing prior context.

### 3. Use the resolved project everywhere after

Pass the resolved project id explicitly on later `save` / `search` /
`tasks` calls rather than relying on the default, so the work is filed
where you think it is.

## Key Rules

- Do not ask the user to identify host, workspace, or project metadata unless resolution is clearly wrong and no safe fallback exists.
- Prefer the dream over scattered summaries because it is the consolidated project memory.
- Mention when no dream was found instead of inventing prior context.
- Do not overwrite or edit dream entries unless explicitly asked or running a dream consolidation workflow.
- Remember that Brain's search and graph tools read a SQLite index, not the
  markdown on disk. A file written out of band — a git pull into the brain
  dir, a manual edit — is invisible to `search` until something re-indexes
  it, and the filesystem watcher is off by default. If an entry you know
  exists cannot be found, that is the likely reason.

## Checklist

- [ ] Called `context_get` when prior context could matter.
- [ ] Loaded the project dream separately (it is not part of `context_get`).
- [ ] Used the resolved project ID for later Brain searches/saves.
- [ ] Stated uncertainty if the project could not be determined or no dream existed.
