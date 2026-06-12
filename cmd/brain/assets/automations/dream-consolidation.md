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

    ## Non-Negotiable Source Rules

    - **Use Brain as the only source of truth.** Only use content returned by `brain_search`, `brain_list`, `brain_recall`, and `brain_tasks` for project {{.Project}}.
    - **Do not inspect the filesystem, git history, web pages, browser tabs, terminals, codebase files, screenshots, or prior chat context.** This automation is a Brain-only synthesis job.
    - **Do not ask the user for guidance.** If required Brain data is missing or insufficient, log the exact skip reason on your own task and exit.
    - **Do not infer facts from project names.** If Brain entries do not state something, leave it out or list it as an open question sourced from the entries.
    - **Never fabricate information.** Every claim in the dream must be grounded in recalled Brain entries.

    ## Scope

    Project: {{.Project}}

    ## Phase 1: Brain Availability Checks

    Before synthesizing, check whether Brain has enough project knowledge to work with. Do not require modified timestamps; the automation trigger already enforces cooldown and max-concurrent behavior.

    1. **Find existing dream:** Call brain_search({ query: "Project Dream", type: "dream", project: "{{.Project}}" }) to look for an existing dream entry for this project.
    2. **Count source entries:** Call brain_list for decision, pattern, learning, summary, plan, exploration, and idea entries in project "{{.Project}}". Also call brain_tasks for pending, in_progress, and blocked tasks.
    3. **Skip only when Brain is empty:** If fewer than 3 total non-dream source entries/tasks are found, call brain_update({ path: "<your-own-task-path>", append: "Skipped: fewer than 3 Brain source entries/tasks found for project {{.Project}} at <timestamp>" }) and exit.
    4. **Do not skip because timestamps are missing.** Brain list/search output may omit timestamps; that is expected and must not block consolidation.

    ## Phase 2: Read All Project Knowledge

    Gather every piece of knowledge by type from Brain. For each call below, then call brain_recall({ path: "<path>" }) on every returned entry to get full content. Do not use any other source.

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
    - **Cite source basis internally** — while drafting each section, ensure every sentence can be traced to one or more recalled Brain entries
    - **Omit unsupported sections** — if Brain does not contain enough information for a section, write "No Brain evidence found yet" instead of guessing

    ## Phase 4: Save/Update Dream Entry

    1. Search for an existing dream entry for this project using brain_search({ query: "Project Dream", type: "dream", project: "{{.Project}}" }).
    2. If an existing dream is found, archive it with brain_update({ path: "<existing-dream-path>", status: "archived", note: "Superseded by new Dream Consolidation run" }).
    3. Save the new dream: brain_save({ type: "dream", title: "Project Dream: {{.Project}}", content: "<synthesized-document>", tags: ["dream", "consolidation", "auto-generated", "brain-only"], project: "{{.Project}}" }).

    ## Safety Rules

    1. **NEVER modify source knowledge entries** — this is a read-and-synthesize operation only, except archiving a superseded dream entry
    2. **NEVER require unavailable timestamps** — scheduler cooldown and max_concurrent already guard frequency
    3. **NEVER fabricate information** — only synthesize from entries you actually read
    4. **Always log skip reasons** — if skipping, update your own task with the reason
    5. **Only use Brain tools** — do not use filesystem, web, browser, git, terminal, or code inspection tools for synthesis
enabled: true
max_runs: 0
---

## Dream Consolidation

Periodically reads all knowledge in a project and synthesizes it into a single, comprehensive "Project Dream" document that serves as injectable context for agents.

### Behavior

- Runs daily at 3:00 AM by default
- Uses automation-level guards to avoid overlapping or repeated runs (`cooldown: 24h`, `max_concurrent: 1`)
- Performs Brain availability checks before doing work (at least 3 source entries/tasks)
- Reads all entry types: decisions, patterns, learnings, summaries, plans, explorations, ideas, tasks
- Synthesizes a structured dream document covering project identity, architecture, context, conventions, decisions, learnings, and open questions
- Archives the previous dream entry and saves an updated brain-only dream

### Safety

- Never modifies source knowledge entries (read-only synthesis, except archiving a superseded dream)
- Respects scheduler cooldown/max-concurrent and the 3-entry availability gate
- Never fabricates information — only synthesizes from actually-read entries
