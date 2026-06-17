---
name: using-brain
description: "Use when you need to persist knowledge, recall previous work, search semantically, attach files/images, or create durable Brain tasks/automations - provides patterns for effective Brain usage across memory, context, attachments, schedules, and event triggers"
---

# Using the Brain

The Brain is a persistent knowledge store and task/automation system that survives across sessions. Use it to save valuable insights, recall previous work, link related knowledge, attach source files or images, search semantically, and create durable scheduled or event-triggered work.

## Checklist

### When Saving
- [ ] Step 1: Choose correct entry type (summary, report, pattern, learning, etc.)
- [ ] Step 2: Write descriptive title for easy recall
- [ ] Step 3: Add wiki-links `[[title]]` to connect to related entries
- [ ] Step 4: Include relevant tags for categorization
- [ ] Step 5: Set `global: true` for cross-project patterns/learnings
- [ ] Step 6: Review related entry suggestions after saving
- [ ] Step 7: Consider linking to suggested related entries

### When Recalling
- [ ] Step 1: Use `brain_project_context` to load project dream context at session start
- [ ] Step 2: Search before creating to avoid duplicates
- [ ] Step 3: Use `brain_backlinks`, `brain_outlinks`, and `brain_related` to explore the knowledge graph

### When Handling Files or Images
- [ ] Step 1: Upload local files with `brain_attachment_upload`
- [ ] Step 2: Attach them to an entry with `brain_attachment_attach`
- [ ] Step 3: Run `brain_attachment_extract` for PDFs/images/media when text is needed
- [ ] Step 4: Recall entries with `include: ["attachments", "attachment_text"]` when attachment context matters

### When Scheduling or Automating
- [ ] Step 1: Use the `brain-automation` skill for event-triggered or scheduled work
- [ ] Step 2: Prefer scheduled tasks for simple recurring work
- [ ] Step 3: Prefer automation entries for event/webhook/session-triggered generated work
- [ ] Step 4: Add safeguards such as `once_per`, `cooldown`, `max_concurrent`, `max_runs`, or `expires_at`

### Maintenance
- [ ] Step 1: Check `brain_orphans` periodically for unconnected notes
- [ ] Step 2: Review stale/low-value entries monthly (cleanup is automatic)
- [ ] Step 3: Tag important entries with "important" to prevent cleanup

## Key Features

- **Wiki-links**: Connect notes with `[[title]]` syntax
- **Backlinks**: See what notes reference the current note
- **Related suggestions**: Discover connections you might have missed
- **Full-text search**: Find anything by content, title, or tags
- **Semantic and hybrid search**: Use embeddings when exact words are unknown
- **Attachments**: Upload, attach, extract text from, and download local files/images/PDFs
- **Tasks and schedules**: Store executable tasks with dependencies, cron schedules, one-time runs, and feature-level schedules
- **Automations**: Create durable event, cron, webhook, or session-triggered actions
- **Orphan detection**: Find unconnected notes that need linking

## When to Use the Brain

### Save to Brain When:
- You've completed a significant analysis or investigation
- You've created an implementation plan that might be referenced later
- You've discovered a reusable pattern or best practice
- You've written a code walkthrough or architecture explanation
- You've made important decisions that should be remembered
- You need to preserve context before a session ends
- You need to attach a source image, local PDF, screenshot, log, or generated artifact to durable context
- You need future agents to see exact input files instead of relying on conversation text

### Recall from Brain When:
- Starting work on a familiar topic
- Resuming work from a previous session
- Looking for patterns or approaches used before
- Needing context about past decisions
- Searching conceptually when exact keywords may differ
- Reviewing an entry that has attachments or extracted media text

Start with `brain_project_context` when working inside a project checkout. It automatically registers the current Brain client/workspace, resolves the Brain project across hosts and git worktrees, and returns the latest `dream` entry as consolidated context.

Use the `brain-project-context` skill when you need to refresh consolidated project context mid-task because context feels thin or assumptions are starting to pile up.

### Link Notes When:
- A new entry relates to existing patterns or learnings
- You discover connections between different pieces of work
- You want to build a knowledge graph for a topic

### Automate with Brain When:
- The user says to repeat something on a schedule
- The user gives a specific future time for work to happen
- The user wants a task created after another task or feature completes
- The user asks for monitoring, periodic review, reminders, webhooks, or generated follow-ups

Use the `brain-automation` skill for these requests.

## Entry Types

| Type | Use For | Global? | Cleanup |
|------|---------|---------|---------|
| `summary` | Session summaries, key decisions | No | 90 days |
| `report` | Analysis reports, investigations, code reviews | No | 90 days |
| `walkthrough` | Code explanations, architecture overviews | No | 90 days |
| `plan` | Implementation plans, designs, roadmaps | No | 90 days |
| `pattern` | Reusable patterns discovered | **Yes** | Never |
| `learning` | General learnings, best practices | **Yes** | Never |
| `idea` | Quick idea captures during work | Either | 30 days |
| `scratch` | Temporary notes | No | 7 days |
| `decision` | Architectural decisions and ADRs | No | 90 days |
| `exploration` | Research findings and codebase investigations | No | 90 days |
| `task` | Executable work for the Brain runner | No | Until completed/archived |
| `dream` | Consolidated project memory | No | Managed by dream workflow |
| `automation` | Event/cron/webhook/session-triggered actions | No | Until disabled/archived |

## Tool Reference

### `brain_save` - Persist Knowledge
```
brain_save(
  type: "summary" | "report" | "walkthrough" | "plan" | "pattern" | "learning" | "idea" | "scratch" | "decision" | "exploration" | "task" | "dream" | "automation",
  title: string,
  content: string,           // Use [[title]] to link to other entries
  tags?: string[],           // For search and categorization
  relatedEntries?: string[], // Titles or paths of related brain entries to link
  global?: boolean,          // Save to global brain (cross-project)
  project?: string           // Explicit project for project-scoped entries
)
```

**Returns:**
```
✅ Saved to {location}

**Path:** `projects/abc123/plan/x1y2z3a4.md`
**ID:** `x1y2z3a4`           ← 8-char alphanumeric ID for easy reference
**Link:** `[Title](x1y2z3a4)` ← Markdown link for use in other notes
**Title:** {title}
**Type:** {type}
```

**IMPORTANT:** Capture the **ID** (e.g., `x1y2z3a4`) from the response - use this for reliable lookups!

### `brain_recall` - Retrieve Specific Entry
```
brain_recall(
  path?: string,  // Path OR 8-char ID (e.g., "x1y2z3a4" or "projects/abc/plan/file.md")
  title?: string, // Title (exact match required)
  include?: ["attachments"] | ["attachments", "attachment_text"]
)
```

**ID Lookup (Recommended):**
```
brain_recall(path: "x1y2z3a4")  // Use the 8-char ID from brain_save
```

**Path Lookup:**
```
brain_recall(path: "projects/abc123/plan/x1y2z3a4.md")
```

**Title Lookup (Less Reliable):**
```
brain_recall(title: "My Feature Plan")  // Requires exact title match
```

**Returns:** Entry content, metadata, backlinks, and optional attachment metadata/text references.

### `brain_list` - Browse Entries
```
brain_list(
  type?: string,
  status?: string,
  project?: string,
  tags?: string[],
  filename?: string,          // Supports wildcards like "abc*"
  priority?: "high" | "medium" | "low",
  sortBy?: "created" | "modified" | "priority",
  sortOrder?: "asc" | "desc",
  limit?: number,
  offset?: number,
  global?: boolean
)
```

### `brain_search` - Full-Text Search
```
brain_search(
  query: string,
  type?: string,
  status?: string,
  project?: string,
  tags?: string[],
  feature_id?: string,
  priority?: "high" | "medium" | "low",
  limit?: number,
  global?: boolean,
  strategy?: "fts" | "exact" | "like" | "semantic" | "hybrid"
)
```

Use `strategy: "semantic"` when the words may not match exactly. Use `strategy: "hybrid"` when you want both exact keyword strength and conceptual embedding matches.

### `brain_inject` - Load Context
```
brain_inject(
  query: string,       // What context to find
  maxEntries?: number, // Default: 5
  type?: string        // Filter by type
)
```

### `brain_backlinks` / `brain_outlinks` / `brain_related` - Explore Link Graph
```
brain_backlinks(path: "x1y2z3a4")  // Find entries linking TO this entry
brain_outlinks(path: "x1y2z3a4")   // Find entries this entry links TO
brain_related(path: "x1y2z3a4")    // Find entries sharing linked notes
```

**Note:** Use the 8-char ID or full path for the `path` parameter.

### `brain_orphans` - Find Unconnected Notes
```
brain_orphans(
  type?: string,   // Filter by type
  limit?: number   // Default: 20
)
```

**Returns:** Notes with no incoming links (need connection)

### `brain_stats` - View Statistics
```
brain_stats(
  global?: boolean  // Show only global stats
)
```

### Attachment Tools - Files, Images, PDFs, and Media
```
brain_attachment_upload(
  project_id: string,
  file_path: string,
  metadata?: object
)

brain_attachment_attach(
  project_id: string,
  entry_id: string,        // Entry ID or path
  attachment_id: string,
  role?: "source" | "image" | "pdf" | "inline" | string,
  caption?: string
)

brain_attachment_extract(
  project_id: string,
  attachment_id: string,
  entry_id?: string,
  metadata?: object
)

brain_attachment_text(project_id: string, attachment_id: string)
brain_attachment_download(project_id: string, attachment_id: string, output_path: string)
```

Use this flow for screenshots, pasted images saved locally, PDFs, logs, and other files that future agents should inspect exactly.

### Task Scheduling and Automation
For detailed workflows, use the `brain-automation` skill.

Quick project-level automation:
```
brain_save(
  type: "automation",
  title: "Nightly test audit",
  content: "Create a generated run task every night to audit tests and save a report.",
  status: "active",
  project: "<project>",
  trigger: {
    type: "cron",
    schedule: "0 2 * * *",
    once_per: "day",
    cooldown: "20h",
    max_concurrent: 1
  },
  action: {
    type: "create_task",
    title_template: "Nightly test audit {{date}}",
    direct_prompt: "Run the nightly test audit and save findings to Brain.",
    complete_on_idle: true
  }
)
```

Use `type: "task"` with `schedule` only when the user explicitly asks for a scheduled task row. For requests phrased as automations, monitors, periodic checks, or recurring reviews, use `type: "automation"` so the Automations tab shows a collapsible parent with generated run history.

Quick event automation:
```
brain_save(
  type: "automation",
  title: "Follow up after feature completion",
  content: "Create checkout work after a feature completes.",
  status: "active",
  project: "<project>",
  trigger: {
    type: "event",
    event: "feature.completed",
    filter: { project_id: "<project>", feature_id: "<feature-id>" },
    once_per: "feature_id",
    cooldown: "10m",
    max_concurrent: 1
  },
  action: {
    type: "create_task",
    title_template: "Checkout {{feature_id}}",
    direct_prompt: "Use feature-checkout to audit {{feature_id}} against original requests.",
    complete_on_idle: true
  }
)
```

## Linking Patterns

### Pattern 1: Link When Saving
Include wiki-links in your content to connect notes:

```
brain_save(
  type: "plan",
  title: "Auth System Refactoring",
  content: "## Overview\nRefactoring auth based on [[JWT Validation Pattern]].\n\nSee [[Auth Security Report]] for the analysis that led to this plan.\n\n## Implementation\n...",
  tags: ["auth", "security"]
)
```

### Pattern 2: Check Related After Saving
After saving, the tool suggests related entries. Consider linking to them:

```
brain_save(type: "pattern", title: "Rate Limiting Pattern", ...)

# Response includes:
# 💡 Related entries you might want to link:
# - [[API Security Patterns]] (pattern)
# - [[Auth System Refactoring]] (plan)
```

### Pattern 3: Explore Links Before Working
Use graph tools to understand the knowledge graph:

```
brain_search(query: "JWT Validation Pattern", type: "pattern", limit: 3)
brain_backlinks(path: "<entry-id>")
brain_outlinks(path: "<entry-id>")
brain_related(path: "<entry-id>")

# Returns:
# 🔗 Backlinks (3): [[Auth Plan]], [[Security Report]], [[API Walkthrough]]
# ➡️ Outlinks (1): [[Token Refresh Learning]]
# 💡 Related: [[Rate Limiting Pattern]], [[Session Management]]
```

### Pattern 4: Find and Fix Orphans
Periodically check for unconnected notes:

```
brain_orphans(type: "pattern")

# Returns patterns with no incoming links
# Consider linking them from relevant plans or reports
```

## Effective Usage Patterns

### Pattern 1: Save Before Session Ends
When you've done significant work, save a summary with links:

```
brain_save(
  type: "summary",
  title: "Auth System Refactoring - Session 1",
  content: "## What was done\n- Analyzed current auth flow\n- Identified 3 security issues\n- Created migration plan\n\n## Related\n- [[Auth Security Report]] - The analysis\n- [[JWT Validation Pattern]] - Pattern we'll use\n\n## Next steps\n- Implement JWT refresh tokens\n- Add rate limiting",
  tags: ["auth", "security", "in-progress"]
)
```

If exact files, screenshots, PDFs, or logs should be preserved, upload them with `brain_attachment_upload` and attach them to the saved entry.

### Pattern 2: Inject Context at Session Start
When resuming work, pull in relevant context:

```
brain_inject(
  query: "auth system refactoring",
  maxEntries: 3
)
```

### Pattern 3: Save Reusable Patterns Globally
When you discover something useful across projects:

```
brain_save(
  type: "pattern",
  title: "Error Handling Pattern for API Routes",
  content: "## Pattern\nWrap route handlers in try-catch with standardized error response...\n\n## Example\n```typescript\n...\n```\n\n## When to use\n- All API routes\n- Async operations\n\n## Related\n- [[API Response Format Pattern]]\n- [[Logging Best Practices]]",
  tags: ["error-handling", "api", "typescript"],
  global: true
)
```

### Pattern 4: Document Investigation Results
After debugging or investigating an issue:

```
brain_save(
  type: "report",
  title: "Memory Leak Investigation - Dec 2024",
  content: "## Problem\nApp memory growing unbounded after 24h\n\n## Root Cause\nEvent listeners not cleaned up in useEffect\n\n## Solution\nAdded cleanup function to all subscription effects\n\n## Patterns Discovered\n- [[React Cleanup Pattern]] - New pattern from this investigation\n\n## Files affected\n- src/hooks/useSubscription.ts\n- src/components/Dashboard.tsx",
  tags: ["debugging", "memory-leak", "react"]
)
```

For large logs or screenshots, attach the original file so later agents can inspect the raw source.

### Pattern 5: Build Knowledge Graphs
For complex topics, build interconnected notes:

```
# 1. Save the main pattern
brain_save(type: "pattern", title: "Authentication Architecture", ...)

# 2. Save related patterns, linking back
brain_save(
  type: "pattern", 
  title: "JWT Token Validation",
  content: "Part of [[Authentication Architecture]].\n\n..."
)

# 3. Save learnings that reference patterns
brain_save(
  type: "learning",
  title: "Always Validate at Boundaries",
  content: "Learned from [[JWT Token Validation]].\n\n..."
)

# 4. Check the graph
brain_search(query: "Authentication Architecture", type: "pattern", limit: 3)
brain_backlinks(path: "<entry-id>")
brain_outlinks(path: "<entry-id>")
brain_related(path: "<entry-id>")
```

### Pattern 6: Preserve Important Entries
Tag entries you want to keep forever:

```
brain_save(
  type: "learning",
  title: "Project Architecture Decisions",
  content: "...",
  tags: ["important", "architecture"]  // "important" tag prevents cleanup
)
```

## Cleanup Rules

The brain self-cleans based on usage:

**Always Kept:**
- Entries accessed in last 30 days
- Entries accessed more than 2 times
- All `pattern` and `learning` types
- Entries tagged `important`, `core`, or `keep`

**Auto-Removed:**
- `scratch` entries older than 7 days
- `idea` entries older than 30 days without recent access
- Other entries older than 90 days with low access

Cleanup runs automatically based on these rules.

## Architecture

The brain is accessed via the **Brain API** (default: `http://localhost:3333`). All brain tools (`brain_save`, `brain_recall`, etc.) communicate with this API - you don't interact with storage directly.

**Key points:**
- Storage is managed by the Brain API server
- Configure via `BRAIN_API_URL` environment variable if needed
- The API handles indexing, search, and link tracking automatically

## Tips

1. **Use wiki-links liberally** - `[[title]]` connects your knowledge
2. **Be descriptive in titles** - Makes recall and linking easier
3. **Use consistent tags** - Helps with categorization
4. **Save incrementally** - Don't wait until the end
5. **Use global for cross-project knowledge** - Patterns and learnings
6. **Review with `brain_list`** - See what's accumulated
7. **Search before creating** - Avoid duplicates
8. **Check orphans periodically** - Keep the graph connected
9. **Explore links before working** - Understand existing knowledge
10. **Clean up periodically** - Cleanup runs automatically; focus on tagging key entries
