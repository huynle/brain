---
name: brain-dream-context
description: Use when an agent needs more project context mid-task - recalls the current project's latest Brain dream through brain_project_context - refreshes durable context without interrupting work
---

# Brain Dream Context

## Overview

**Core Principle:** When context feels thin, refresh from the project dream before guessing.

Brain project dreams are consolidated project memory. Use this skill to reload that memory during implementation, review, debugging, or planning when the current context is not enough.

## When to Use
- You need more project background during implementation, review, debugging, or planning.
- You are unsure about project conventions, prior decisions, or current direction.
- The user says "recall the dream", "load context", or asks for more project memory.
- You are about to infer intent from incomplete context and project memory could reduce guesswork.

## When NOT to Use
- You already loaded the dream recently and nothing has changed.
- The task is trivial or purely local.
- You need a specific entry, plan, or task rather than broad project context.
- You are not working in a project checkout or workspace that Brain can resolve.

## Workflow
1. Call `brain_project_context` before searching manually.
2. Read the returned project, confidence, source, and dream content.
3. Treat high-confidence dream content as the default project memory.
4. If confidence is low or no dream exists, state uncertainty and continue with normal exploration.
5. Do not modify dream entries unless explicitly asked or running a dream consolidation workflow.

## Key Rules
- Prefer the returned dream over scattered summaries because it is the consolidated project memory.
- Use dream context to guide the current task, not to replace direct code inspection.
- Mention uncertainty when resolution confidence is low or no dream was found.
- Do not overwrite, edit, or delete dream entries during normal context recall.

## Checklist
- [ ] Called `brain_project_context` before guessing.
- [ ] Incorporated useful dream context into the current task.
- [ ] Stated uncertainty if no dream or low-confidence resolution.
