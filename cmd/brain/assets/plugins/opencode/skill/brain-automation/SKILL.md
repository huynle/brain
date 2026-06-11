---
name: brain-automation
description: Use when the user wants Brain to do something later, repeatedly, or in response to an event - creates scheduled tasks or automation entries with triggers, actions, retries, and execution metadata - turns recurring or event-driven requests into durable Brain work
---

# Brain Automation

## Overview

**Core Principle:** If the user asks for repeatable, delayed, or event-driven work, encode it in Brain instead of relying on conversation memory.

Brain supports two automation shapes:
- **Scheduled tasks:** Save `type: "task"` with `schedule`, `run_once_at`, or feature schedule fields when the work itself should run on a clock.
- **Automation entries:** Save `type: "automation"` with `trigger`, `action`, and optional `retry` when an event, webhook, session, or cron trigger should create tasks or run scripts.

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
1. Identify whether this is a scheduled task or an automation entry.
2. Ask one clarifying question only if the trigger time/event, project, or action is ambiguous.
3. Resolve project context with `brain_project_context` when working from a checkout.
4. Save the durable Brain entry with the minimum fields needed.
5. Include `user_original_request` verbatim for generated tasks or task-like work.
6. Set safeguards: `once_per`, `cooldown`, `max_concurrent`, `max_runs`, `expires_at`, or retry limits when appropriate.
7. Report the created entry ID/path, trigger/schedule, and what will happen.

## Scheduled Task Patterns

### Recurring Task
Use this when the same task should run repeatedly.

```
brain_save(
  type: "task",
  title: "Weekly Dependency Audit",
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
brain_save(
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
brain_save(
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
brain_save(
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
    target_workdir: "<absolute-workdir>"
  },
  retry: { max_attempts: 2, backoff: "5m", timeout: "30m" }
)
```

### Task or Status Event
Use event filters when only specific task transitions should fire.

```
brain_save(
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
    direct_prompt: "Summarize task {{task_id}}, include commit/results, and save a Brain report."
  }
)
```

### Cron Automation Entry
Use this when the trigger creates dynamic work, not when a single fixed task should simply run on a cron.

```
brain_save(
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
    direct_prompt: "Review stale pending or blocked tasks and save recommendations."
  }
)
```

## Field Guide

| Need | Field |
|------|-------|
| Repeat a task on cron | `schedule` on `type: "task"` |
| Run once in the future | `run_once_at` on `type: "task"` |
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

## Safety Rules
- Use `status: "active"` only when the user clearly wants the automation enabled now.
- Use `status: "draft"` when proposing an automation for review.
- Include `expires_at`, `max_runs`, or `cooldown` for recurring work that could run indefinitely.
- For generated tasks, include enough context in `direct_prompt` that a future agent can execute without this conversation.
- For cross-project work, set `project` and `target_workdir` explicitly.
- Never schedule destructive unattended actions unless the user explicitly asked for unattended execution.

## Checklist
- [ ] Classified the request as scheduled task, feature schedule, or automation entry.
- [ ] Captured project, trigger/schedule, action, and target workdir.
- [ ] Added deduplication/cooldown/concurrency safeguards where useful.
- [ ] Included `user_original_request` for task-like work.
- [ ] Reported created ID/path and the exact trigger behavior.
