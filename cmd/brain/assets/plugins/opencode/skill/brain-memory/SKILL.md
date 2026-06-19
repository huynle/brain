---
name: brain-memory
description: "Use when an agent should remember or recall important conversation context - saves durable memories with Brain entry types and naturally retrieves relevant memories before responding or acting"
---

# Brain Memory

Use Brain as durable conversational memory. Capture important facts, preferences, decisions, constraints, and session outcomes so future agents can naturally recall them without relying on the current chat transcript.

## Core Principle

Remember what will matter later, not everything that was said. A good memory is concise, scoped, searchable, and useful to a future agent making a decision.

## When to Use

- The user says "remember", "note", "keep track of", "for next time", or similar.
- The user states a durable preference, constraint, identity, environment detail, or working style.
- A conversation produces an important decision, tradeoff, plan, or project fact.
- You are starting or resuming work and prior context could change the answer.
- You finish meaningful work and future agents need the outcome, rationale, or follow-up context.

## When NOT to Use

- The information is trivial, temporary, or already obvious from the repository.
- The user is brainstorming and has not settled on a decision.
- The information is sensitive personal data, credentials, tokens, private keys, or secrets.
- The memory would duplicate an existing Brain entry without adding new value.
- The user explicitly says not to remember it.

## Natural Recall

At the start of project work or when prior context may matter:

1. Call `brain_project_context` to load the current project and latest `dream` memory.
2. Search for relevant memories using `brain_search` or `brain_inject` when the user asks about preferences, history, prior decisions, or project context.
3. Use recalled memory quietly as context. Mention it only when it materially affects your answer or there is uncertainty.

Good recall prompts:

```text
brain_project_context()
brain_inject(query: "user preferences coding style project constraints", maxEntries: 5)
brain_search(query: "deployment decision migration rollback", type: "decision", limit: 5)
brain_search(query: "memory preference <topic>", tags: ["memory"], limit: 5)
```

If recall is low-confidence or contradictory, say so and ask a short clarifying question.

## Memory Types

Brain does not need a separate `memory` entry type. Use the existing type that matches the information:

| Type | Use For | Scope |
|------|---------|-------|
| `summary` | Conversation/session memory and outcomes | Project by default |
| `decision` | Durable project choices, constraints, ADR-like facts | Project |
| `learning` | User preferences or cross-project lessons | Global when broadly applicable |
| `pattern` | Reusable implementation or workflow patterns | Global when reusable |
| `idea` | Potential future work not yet committed | Project or global |
| `scratch` | Short-lived working notes | Project |
| `dream` | Consolidated project memory | Managed by dream workflow |

Always tag memory entries with `memory` plus topic tags such as `preference`, `constraint`, `decision`, `conversation`, `security`, `deployment`, or `workflow`.

## What to Remember

Remember:

- User preferences that should affect future responses or implementation choices.
- Project-specific constraints, conventions, operational facts, and non-obvious setup details.
- Decisions made in conversation and the rationale behind them.
- Production caveats discovered during work, especially security, data migration, deployment, or rollback concerns.
- Important follow-ups, unresolved questions, or handoff notes.
- Links or IDs for important Brain entries, tasks, plans, and reports.

Do not remember:

- Secrets, credentials, tokens, private keys, personal identifiers, or sensitive HR/medical/financial details.
- Raw logs or large pasted content unless summarized and clearly useful.
- Speculative ideas as decisions.
- Information the user likely expects to be ephemeral.

## Capture Workflow

### 1. Decide Whether It Is Durable

Ask: "Will a future agent make a better decision if it knows this?"

If yes, save it. If uncertain and the memory is personal or sensitive, ask before saving.

### 2. Choose Scope

- Project-scoped by default for project facts and session summaries.
- `global: true` only for durable cross-project preferences, workflows, patterns, or learnings.

### 3. Write a Useful Memory

A useful memory has:

- A searchable title.
- Date or context.
- The fact/decision/preference.
- Why it matters.
- Applicability and limits.
- Related Brain links when relevant.

Template:

```markdown
## Memory
<concise fact, preference, or decision>

## Context
<where it came from and why it matters>

## Applies When
<future situations where agents should use it>

## Limits
<when not to apply it, uncertainty, or expiration>

## Related
- <Brain links, task IDs, plan IDs, files, or none>
```

### 4. Save with Brain

Conversation/session memory:

```text
brain_save(
  type: "summary",
  title: "Memory: <short topic>",
  content: "<memory template>",
  tags: ["memory", "conversation", "<topic>"],
  project: "<project>"
)
```

Project decision:

```text
brain_save(
  type: "decision",
  title: "Decision: <short decision>",
  content: "<decision, rationale, consequences>",
  tags: ["memory", "decision", "<topic>"],
  project: "<project>"
)
```

Cross-project user preference or lesson:

```text
brain_save(
  type: "learning",
  title: "Memory: <preference or lesson>",
  content: "<preference, why it matters, when to apply>",
  tags: ["memory", "preference", "<topic>"],
  global: true
)
```

Reusable implementation pattern:

```text
brain_save(
  type: "pattern",
  title: "Pattern: <name>",
  content: "<pattern, example, constraints>",
  tags: ["memory", "pattern", "<topic>"],
  global: true
)
```

### 5. Confirm Briefly

When the user explicitly asked you to remember something, respond with the saved memory ID or path. Keep it short.

Example:

```text
Saved as `a1b2c3d4`: prefer concise final responses unless more detail is requested.
```

## Updating Existing Memories

Before saving a new memory, search for likely duplicates when practical:

```text
brain_search(query: "<topic> memory preference", tags: ["memory"], limit: 5)
```

If an existing memory should change:

- Use `brain_update(path: "<id-or-path>", append: "...")` for additive context.
- Save a new `decision` when a decision supersedes an older one, and link the older entry.
- Do not silently overwrite conflicting memories; note the conflict and ask if needed.

## End-of-Session Memory

At the end of substantial work, save a session summary if it would help future agents:

```text
brain_save(
  type: "summary",
  title: "Session Memory: <work/topic>",
  content: "## Outcome
...

## Decisions
...

## Follow-ups
...

## Important IDs
...",
  tags: ["memory", "session", "<topic>"],
  project: "<project>"
)
```

If the project uses dream mode, session summaries and decisions become source material for future consolidated `dream` context.

## Privacy and Safety

- Do not store secrets or credentials.
- Do not store sensitive personal data unless the user explicitly asks and it is necessary.
- Prefer summarization over raw conversation excerpts.
- If a user asks to forget a memory, locate it with `brain_search` and delete or archive it according to the user's request.
- If a memory could cause harmful personalization or privacy risk, do not save it.

## Anti-Patterns

- Saving every conversation as memory.
- Saving vague memories like "user likes good code".
- Treating a brainstorm as a decision.
- Using `global: true` for project-specific facts.
- Recalling old memory without checking whether the current repository contradicts it.
- Exposing recalled memory unnecessarily in the final response.

## Final Checklist

- [ ] Recalled project dream/context when prior context may matter.
- [ ] Searched for relevant existing memory when appropriate.
- [ ] Saved only durable, useful information.
- [ ] Chose the correct Brain entry type and scope.
- [ ] Added `memory` and topic tags.
- [ ] Avoided secrets and sensitive personal data.
- [ ] Confirmed saved ID/path when the user explicitly asked to remember something.
