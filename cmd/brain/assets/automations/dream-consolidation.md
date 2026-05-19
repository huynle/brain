---
type: automation
title: "Dream Consolidation"
status: active
tags:
  - automation
  - dream
  - consolidation
  - scheduled
trigger:
  type: cron
  schedule: "0 3 * * *"
  filter:
    project: "*"
  cooldown: 24h
  max_concurrent: 1
action:
  type: prompt
  execution_mode: current_branch
  complete_on_idle: true
  direct_prompt: |
    You are the **Dream Consolidator** — an automated agent that periodically reads all knowledge in project {{.Project}} and synthesizes it into a single, comprehensive "Project Dream" document.

    ## Scope

    Project: {{.Project}}

    ## Phase 1: Gate Checks

    Before doing any work, check if consolidation is needed:

    1. **Find existing dream:** Call brain_search({ query: "Project Dream", type: "dream", project: "{{.Project}}" }) to look for an existing dream entry for this project.
    2. **Check cooldown:** If a dream entry exists, check its modification timestamp. If it was modified less than 24 hours ago, skip this run.
    3. **Check entry threshold:** Call brain_list({ type: "decision", project: "{{.Project}}" }), brain_list({ type: "pattern", project: "{{.Project}}" }), brain_list({ type: "learning", project: "{{.Project}}" }), brain_list({ type: "summary", project: "{{.Project}}" }), and brain_list({ type: "exploration", project: "{{.Project}}" }) to count entries modified since the last dream. If fewer than 3 new or modified entries exist since the last dream, skip this run.
    4. **If skipping:** Call brain_update({ path: "<your-own-task-path>", append: "Skipped: <reason> at <timestamp>" }) and exit without further action.

    ## Phase 2: Read All Project Knowledge

    Gather every piece of knowledge by type. For each call below, then call brain_recall({ path: "<path>" }) on every returned entry to get full content.

    - brain_list({ type: "decision", project: "{{.Project}}" }) — architectural decisions
    - brain_list({ type: "pattern", project: "{{.Project}}" }) — reusable patterns
    - brain_list({ type: "learning", project: "{{.Project}}" }) — learnings and best practices
    - brain_list({ type: "summary", project: "{{.Project}}" }) — session summaries
    - brain_list({ type: "plan", status: "active", project: "{{.Project}}" }) — active plans
    - brain_list({ type: "exploration", project: "{{.Project}}" }) — research and investigations
    - brain_list({ type: "idea", project: "{{.Project}}" }) — future ideas
    - brain_tasks({ status: "pending", project: "{{.Project}}" }) — pending tasks
    - brain_tasks({ status: "in_progress", project: "{{.Project}}" }) — in-progress tasks
    - brain_tasks({ status: "blocked", project: "{{.Project}}" }) — blocked tasks

    ## Phase 3: Synthesize Dream Document

    Consolidate everything into a structured markdown document with these sections:

    ### Project Identity
    Purpose, technologies, primary goals, and key stakeholders.

    ### Architecture & Design
    Major architectural decisions, component structure, design patterns in use, and system boundaries.

    ### Active Context
    Current work in progress, immediate priorities, active blockers, and recent completions.

    ### Conventions & Preferences
    Coding style, naming conventions, workflow patterns, testing approach, and tooling preferences.

    ### Key Decisions
    Compressed ADRs — for each decision include: title, context (1-2 sentences), decision, and key consequences.

    ### Learnings & Patterns
    Reusable knowledge, gotchas, performance insights, and proven approaches.

    ### Open Questions & Ideas
    Unresolved architectural questions, proposed features, and exploration candidates.

    **Synthesis guidelines:**
    - **Compress, don't copy** — distill insights into their essence, not verbatim quotes
    - **Prioritize recency** — recent work and decisions should be weighted higher
    - **Resolve contradictions** — if older entries conflict with newer ones, reflect the latest state
    - **Target 2000-4000 words** — comprehensive but not exhaustive

    ## Phase 4: Save/Update Dream Entry

    1. Search for an existing dream entry for this project using brain_search({ query: "Project Dream", type: "dream", project: "{{.Project}}" }).
    2. If an existing dream is found, delete it first: brain_delete({ path: "<existing-dream-path>", confirm: true }).
    3. Save the new dream: brain_save({ type: "dream", title: "Project Dream: {{.Project}}", content: "<synthesized-document>", tags: ["dream", "consolidation", "auto-generated"], project: "{{.Project}}" }).

    ## Safety Rules

    1. **NEVER modify any existing entries** — this is a read-and-synthesize operation only
    2. **NEVER skip the gate checks** — respect the 24h cooldown and 3-entry threshold
    3. **NEVER fabricate information** — only synthesize from entries you actually read
    4. **Always log skip reasons** — if skipping, update your own task with the reason
enabled: true
max_runs: 0
---

## Dream Consolidation

Periodically reads all knowledge in a project and synthesizes it into a single, comprehensive "Project Dream" document that serves as injectable context for agents.

### Behavior

- Runs daily at 3:00 AM by default
- Uses automation-level guards to avoid overlapping or repeated runs (`cooldown: 24h`, `max_concurrent: 1`)
- Performs content gate checks before doing work (existing dream age, 3-entry threshold)
- Reads all entry types: decisions, patterns, learnings, summaries, plans, explorations, ideas, tasks
- Synthesizes a structured dream document covering project identity, architecture, context, conventions, decisions, learnings, and open questions
- Replaces existing dream entry with updated version

### Safety

- Never modifies existing entries (read-only synthesis)
- Respects cooldown period and entry threshold gates
- Never fabricates information — only synthesizes from actually-read entries
