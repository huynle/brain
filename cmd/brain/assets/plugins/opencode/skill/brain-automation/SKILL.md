---
name: brain-automation
description: Use when the user wants Brain to do something later, repeatedly, or in response to an event - creates scheduled tasks or automation entries with triggers, actions, retries, and execution metadata - turns recurring or event-driven requests into durable Brain work
---

# Brain Automation

## Overview

**Core Principle:** If the user asks for repeatable, delayed, or event-driven work, encode it in Brain instead of relying on conversation memory.

Brain supports two timed-work shapes, but user-facing project automations should use automation entries:
- **Automation entries:** Save `type: "automation"` with `trigger`, `action`, and optional `retry`. This is the default for user requests like "create an automation", "run this every N minutes", "monitor X", or "do Y when Z happens". These appear in the Automations tab like Feature Code Review, Blocked Task Inspector, and Dream Consolidation, and their generated runs can be expanded/collapsed.
- **Scheduled tasks:** Save `type: "task"` with `schedule`, `run_once_at`, or feature schedule fields only when the user explicitly asks for a scheduled task or when creating internal runner work. Scheduled tasks show up as task rows, not as collapsible automation entries.

## When to Use
- The user asks to do something at a specific time, after a delay, or on a recurring schedule.
- The user asks to run something when a task, feature, status change, webhook, or runner event happens.
- The user asks for follow-up work after a feature completes.
- The user asks for monitoring, reminders, periodic reviews, recurring reports, or cleanup jobs.
- A workflow needs deduplication, cooldowns, retries, executor selection, or target workdir control.

## When NOT to Use
- The work should happen immediately in the current session.
- The user is brainstorming and has not asked to persist or schedule anything.
- The trigger condition is vague enough that you need one clarifying question first.
- The request would create destructive unattended work without explicit user approval.

## Workflow
1. Identify whether this is a user-facing automation or an explicit scheduled task. Default to `type: "automation"` for project-level automations.
2. Resolve the destination Brain project before saving. Use `project_context` when working from a checkout, but do not assume the current checkout is the right project for personal, cross-project, office, or external-system automations.
3. Ask one clarifying question if the destination project is not obvious from the user's words or workspace. Example: "Which Brain project should own this automation?"
4. Ask one clarifying question only if the trigger time/event or action is ambiguous.
5. Save the durable Brain entry with the minimum fields needed, including explicit `project: "<project>"`.
6. Include `user_original_request` verbatim for generated tasks or task-like work.
7. Set safeguards: `once_per`, `cooldown`, `max_concurrent`, `max_runs`, `expires_at`, or retry limits when appropriate.
8. Report the created entry ID/path, owning project, trigger/schedule, and what will happen.

## Project Selection Rules
- If the user names a project, use that project.
- If the automation is clearly tied to the current repository, use the project returned by `project_context`.
- If the automation is personal productivity, office activity, cross-project summarization, or not tied to the current repository, ask which Brain project should own it.
- If the user names an output path like `/tmp`, do not infer the Brain project from the path. The output path and the owning Brain project are separate decisions.
- Always set `project` explicitly in `save`; do not rely on the plugin's default project when creating automations.

## Default Pattern: Cron Automation Entry

Use this for user-facing project automations that should run repeatedly. This is the right shape for requests such as "check Teams every 5 minutes", "monitor blockers hourly", or "summarize activity every day".

```
save(
  type: "automation",
  title: "Monitor Teams activity for project work log",
  content: "Every 5 minutes, create a read-only task to inspect recent Teams activity and save concise project-work notes to Brain.",
  status: "active",
  project: "<explicit-project>",
  trigger: {
    type: "cron",
    schedule: "*/5 * * * *",
    once_per: "5m",
    cooldown: "4m",
    max_concurrent: 1
  },
  action: {
    type: "create_task",
    title_template: "Teams activity work-log check {{time}}",
    direct_prompt: "Use the daily-office-chromeuse skill. Read recent Microsoft Teams activity only. Extract concise, timestamped project/work evidence and save new findings to Brain. Do not send messages or modify external systems.",
    agent: "assistant",
    executor: "opencode",
    target_workdir: "<absolute-workdir>",
    complete_on_idle: true
  },
  retry: { max_attempts: 1, timeout: "20m" }
)
```

Expected UI behavior: one parent row in the Automations tab. Each cron firing creates a generated task/run under that automation, and the row can expand/collapse to show run history.

## Scheduled Task Patterns

### Explicit Scheduled Task
Use this only when the user specifically asks for a scheduled task rather than an automation entry. This will appear in the Tasks tab and as a task row in the Automations tab, not like the built-in Automation rows.

```
save(
  type: "task",
  title: "Weekly Dependency Audit Task",
  content: "Audit dependencies, identify risky updates, and save a report.",
  status: "active",
  project: "<project>",
  schedule: "0 9 * * MON",
  timezone: "America/Denver",
  schedule_enabled: true,
  max_runs: 0,
  user_original_request: "<verbatim user request>",
  direct_prompt: "Run the weekly dependency audit and save findings to Brain.",
  agent: "tdd-dev",
  executor: "opencode"
)
```

### One-Time Future Task
Use this when the user says "do X at time Y".

```
save(
  type: "task",
  title: "Run Release Readiness Check",
  content: "Run release readiness checks and report blockers.",
  status: "active",
  project: "<project>",
  run_once_at: "2026-06-12T15:00:00Z",
  timezone: "America/Denver",
  user_original_request: "<verbatim user request>",
  direct_prompt: "Run release readiness checks and save a summary report.",
  complete_on_idle: true
)
```

### Feature-Level Schedule
Use this when a whole feature should run on a schedule, not just one task.

```
save(
  type: "task",
  title: "Nightly Performance Feature Gate",
  content: "Gate task that schedules the performance-check feature.",
  status: "active",
  project: "<project>",
  feature_id: "performance-check",
  feature_schedule: "0 2 * * *",
  feature_timezone: "America/Denver",
  feature_expires_at: "2026-07-01T00:00:00Z"
)
```

## Event Automation Patterns

### Post-Feature Follow-Up
Use `trigger.event: "feature.completed"` when a follow-up should start after a feature completes.

```
save(
  type: "automation",
  title: "Create checkout after feature completion",
  content: "When the feature completes, create a checkout task to verify original requirements.",
  status: "active",
  project: "<project>",
  trigger: {
    type: "event",
    event: "feature.completed",
    filter: { project_id: "<project>", feature_id: "<feature-id>" },
    once_per: "feature_id",
    cooldown: "10m",
    max_concurrent: 1,
    ignore_automation_events: true
  },
  action: {
    type: "create_task",
    title_template: "Checkout {{feature_id}}",
    direct_prompt: "Use the feature-checkout skill to audit completed tasks for {{feature_id}}.",
    agent: "tdd-dev",
    executor: "opencode",
    target_workdir: "<absolute-workdir>",
    complete_on_idle: true
  },
  retry: { max_attempts: 2, backoff: "5m", timeout: "30m" }
)
```

### Task or Status Event
Use event filters when only specific task transitions should fire.

```
save(
  type: "automation",
  title: "Notify when critical task completes",
  content: "Create a follow-up report when the critical task moves to completed.",
  status: "active",
  project: "<project>",
  trigger: {
    type: "event",
    event: "task.completed",
    filter: { project_id: "<project>", task_id: "<task-id>", to_status: "completed" },
    once_per: "task_id",
    max_concurrent: 1
  },
  action: {
    type: "create_task",
    title_template: "Summarize completed task {{task_id}}",
    direct_prompt: "Summarize task {{task_id}}, include commit/results, and save a Brain report.",
    complete_on_idle: true
  }
)
```

### Cron Automation Entry
Use this when a cron trigger should create generated run tasks under a collapsible automation parent. For user-facing project-level automations, prefer this over `type: "task"` schedules.

```
save(
  type: "automation",
  title: "Daily stale task review",
  content: "Create a task each weekday to review stale Brain tasks.",
  status: "active",
  project: "<project>",
  trigger: {
    type: "cron",
    schedule: "0 8 * * MON-FRI",
    once_per: "day",
    cooldown: "20h",
    max_concurrent: 1
  },
  action: {
    type: "create_task",
    title_template: "Daily stale task review {{date}}",
    direct_prompt: "Review stale pending or blocked tasks and save recommendations.",
    complete_on_idle: true
  }
)
```

## Field Guide

| Need | Field |
|------|-------|
| User asks for an automation on cron | `type: "automation"` + `trigger.type: "cron"` + `action.type: "create_task"` |
| User asks to monitor/review/summarize periodically | `type: "automation"` + cron trigger |
| User explicitly asks for a scheduled task | `schedule` on `type: "task"` |
| User explicitly asks for one scheduled task in the future | `run_once_at` on `type: "task"` |
| Schedule an entire feature | `feature_schedule` or `feature_run_once_at` |
| React to event | `type: "automation"` + `trigger.type: "event"` |
| React to webhook | `type: "automation"` + `trigger.type: "webhook"` + `webhook` |
| Create generated task | `action.type: "create_task"` |
| Run script | `action.type: "script"` + `command` |
| Prevent duplicates | `once_per` |
| Avoid rapid repeats | `cooldown` |
| Bound concurrency | `max_concurrent` |
| Limit retry loops | `retry.max_attempts` |
| Select executor | `executor` or `action.executor` |
| Force target repo | `target_workdir` or `action.target_workdir` |
| Complete generated tasks when idle | `action.complete_on_idle: true` |

## Safety Rules
- Use `status: "active"` only when the user clearly wants the automation enabled now.
- Use `status: "draft"` when proposing an automation for review.
- Include `expires_at`, `max_runs`, or `cooldown` for recurring work that could run indefinitely.
- Do not create `type: "task"` + `schedule` for requests phrased as "automation", "monitor", "check every", or "repeat every" unless the user explicitly asks for a scheduled task row.
- Do not write project-level automations to the current repo project merely because that is where the chat is running; choose or ask for the owning Brain project.
- Set `action.complete_on_idle: true` for generated automation tasks unless there is a concrete reason to keep them open after the executor becomes idle. The runtime also defaults automation-generated tasks to true.
- For generated tasks, include enough context in `direct_prompt` that a future agent can execute without this conversation.
- For cross-project work, set `project` and `target_workdir` explicitly.
- Never schedule destructive unattended actions unless the user explicitly asked for unattended execution.

## Checklist
- [ ] Classified user-facing project automation requests as `type: "automation"`, not scheduled task rows.
- [ ] Confirmed or asked for the owning Brain project before saving.
- [ ] Captured project, trigger/schedule, action, and target workdir.
- [ ] Set `action.complete_on_idle: true` for generated automation tasks unless intentionally disabled.
- [ ] Added deduplication/cooldown/concurrency safeguards where useful.
- [ ] Included `user_original_request` for task-like work.
- [ ] Reported created ID/path and the exact trigger behavior.
