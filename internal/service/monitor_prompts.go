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
