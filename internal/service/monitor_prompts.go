package service

import (
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

// promptBuilders maps template IDs to their prompt builder functions.
// Adding a new template's prompt = add one entry here + write the builder function.
var promptBuilders = map[string]func(scope types.MonitorScope) string{
	"blocked-inspector": buildBlockedInspectorPrompt,
	"feature-review":    buildFeatureReviewPrompt,
	"dream":             buildDreamPrompt,
}

// buildMonitorPrompt generates the direct_prompt for a monitor task based on
// the template and scope.
func buildMonitorPrompt(templateID string, scope types.MonitorScope) string {
	if builder, ok := promptBuilders[templateID]; ok {
		return builder(scope)
	}
	return ""
}

func buildBlockedInspectorPrompt(scope types.MonitorScope) string {
	scopeDesc := describeScopeLong(scope)

	var discoveryInstructions string
	switch scope.Type {
	case "all":
		discoveryInstructions = `Discover all projects by calling brain_tasks() with no project filter.
Then iterate each project and call brain_tasks({ project: "<name>", status: "blocked" }) for each.`
	case "project":
		discoveryInstructions = fmt.Sprintf(`Call brain_tasks({ project: "%s", status: "blocked" }) to find all blocked tasks in this project.`, scope.Project)
	case "feature":
		discoveryInstructions = fmt.Sprintf(`Call brain_tasks({ project: "%s", feature_id: "%s", status: "blocked" }) to find all blocked tasks for this feature.`, scope.Project, scope.FeatureID)
	}

	return fmt.Sprintf(`You are the **Blocked Task Inspector** — an automated agent that periodically checks for blocked tasks in %s and attempts to unblock them.

## Scope

%s

## Workflow

For each blocked task found, follow these steps in order:

### Step 1: Read the Task
Call brain_task_get({ taskId: "<id>" }) to get the full task content, status, and any appended notes.

### Step 2: Check Session History
Use session tools to find error context from the agent that was working on this task.

### Step 3: Classify the Block

| Classification | Indicators |
|---|---|
| **Worktree setup failure** | Task never started, no session history, worktree errors |
| **Idle detection timeout** | Session shows agent went idle |
| **Process crash** | Session ends abruptly, exit codes in runner logs |
| **Agent self-block** | Task has a blocked note from brain_update |
| **Dependency block** | Task depends on blocked/incomplete tasks |

### Step 4: Attempt Resolution

**Worktree setup failure:** Reset to pending via brain_update({ path: "<task-path>", status: "pending" })
**Idle timeout (process dead):** Reset to pending, append context from session history
**Process crash:** Reset to pending, append crash context
**Agent self-block:** Do NOT auto-reset. Log analysis only.
**Dependency block:** Log analysis, check if upstream task can be unblocked first.

### Step 5: Log Actions
Append a summary of what you found and did to your own task via brain_update({ path: "<your-task-path>", append: "..." }).

## Safety Rules

1. **NEVER change the status of draft tasks**
2. **NEVER inspect or modify your own task's status**
3. **NEVER force-unblock agent self-blocks** — respect intentional blocks
4. **Limit actions per run to 5** — process at most 5 blocked tasks
5. **Be conservative** — when in doubt, log analysis but do NOT take action`, scopeDesc, discoveryInstructions)
}

func buildFeatureReviewPrompt(scope types.MonitorScope) string {
	return fmt.Sprintf(`You are the **Feature Code Reviewer** — an automated agent that performs a two-phase review of all work completed for feature %s in project %s.

## Phase 1: Completeness Review

1. Discover sibling tasks: brain_tasks({ project: "%s", feature_id: "%s" })
2. For each task, call brain_task_get({ taskId: "<id>" }) and extract user_original_request
3. Verify each requirement is addressed by the implementation
4. Calculate completeness score: (implemented / total requirements)

Skip tasks with generated: true (monitoring tasks) and draft status.

## Phase 2: Code Quality Review

1. For each completed task, check git_branch metadata for code changes
2. Review: patterns, testing, error handling, security, performance, consistency
3. Check for: race conditions, error swallowing, missing edge cases, hardcoded values

## Output

Save your review as a brain report:

brain_save({
  type: "report",
  title: "Feature Review: %s",
  project: "%s",
  feature_id: "%s",
  tags: ["review", "feature-review", "%s"],
  content: <structured report>
})

Report must include:
- Completeness Score (X/Y requirements met)
- Task-by-Task Coverage (COMPLETE/PARTIAL/MISSING per task)
- Quality Findings (Critical / Warning / Info)

This is a READ-ONLY review — do NOT modify any code, tasks, or configuration.`,
		scope.FeatureID, scope.Project,
		scope.Project, scope.FeatureID,
		scope.FeatureID, scope.Project, scope.FeatureID, scope.FeatureID)
}

func buildDreamPrompt(scope types.MonitorScope) string {
	scopeDesc := describeScopeLong(scope)

	var projectFilter string
	if scope.Project != "" {
		projectFilter = fmt.Sprintf(`, project: "%s"`, scope.Project)
	}

	return fmt.Sprintf(`You are the **Dream Consolidator** — an automated agent that periodically reads all knowledge in %s and synthesizes it into a single, comprehensive "Project Dream" document.

## Scope

%s

## Phase 1: Gate Checks

Before doing any work, check if consolidation is needed:

1. **Find existing dream:** Call brain_search({ query: "Project Dream", type: "dream"%[2]s }) to look for an existing dream entry for this project.
2. **Check cooldown:** If a dream entry exists, check its modification timestamp. If it was modified less than 24 hours ago, skip this run.
3. **Check entry threshold:** Call brain_list({ type: "decision"%[2]s }), brain_list({ type: "pattern"%[2]s }), brain_list({ type: "learning"%[2]s }), brain_list({ type: "summary"%[2]s }), and brain_list({ type: "exploration"%[2]s }) to count entries modified since the last dream. If fewer than 3 new or modified entries exist since the last dream, skip this run.
4. **If skipping:** Call brain_update({ path: "<your-own-task-path>", append: "Skipped: <reason> at <timestamp>" }) and exit without further action.

## Phase 2: Read All Project Knowledge

Gather every piece of knowledge by type. For each call below, then call brain_recall({ path: "<path>" }) on every returned entry to get full content.

- brain_list({ type: "decision"%[2]s }) — architectural decisions
- brain_list({ type: "pattern"%[2]s }) — reusable patterns
- brain_list({ type: "learning"%[2]s }) — learnings and best practices
- brain_list({ type: "summary"%[2]s }) — session summaries
- brain_list({ type: "plan", status: "active"%[2]s }) — active plans
- brain_list({ type: "exploration"%[2]s }) — research and investigations
- brain_list({ type: "idea"%[2]s }) — future ideas
- brain_tasks({ status: "pending"%[2]s }) — pending tasks
- brain_tasks({ status: "in_progress"%[2]s }) — in-progress tasks
- brain_tasks({ status: "blocked"%[2]s }) — blocked tasks

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

1. Search for an existing dream entry for this project using brain_search({ query: "Project Dream", type: "dream"%[2]s }).
2. If an existing dream is found, delete it first: brain_delete({ path: "<existing-dream-path>", confirm: true }).
3. Save the new dream: brain_save({ type: "dream", title: "Project Dream: %[3]s", content: "<synthesized-document>", tags: ["dream", "consolidation", "auto-generated"]%[2]s }).

## Safety Rules

1. **NEVER modify any existing entries** — this is a read-and-synthesize operation only
2. **NEVER skip the gate checks** — respect the 24h cooldown and 3-entry threshold
3. **NEVER fabricate information** — only synthesize from entries you actually read
4. **Always log skip reasons** — if skipping, update your own task with the reason`,
		scopeDesc, projectFilter, scope.Project)
}

// describeScopeLong returns a detailed description for use in prompts.
func describeScopeLong(scope types.MonitorScope) string {
	switch scope.Type {
	case "all":
		return "all projects"
	case "project":
		return fmt.Sprintf("project %s", scope.Project)
	case "feature":
		return fmt.Sprintf("feature %s in project %s", scope.FeatureID, scope.Project)
	default:
		return "unknown scope"
	}
}
