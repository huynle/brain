# Colored Dependency Visualization Implementation

## Summary

Add colored lane visualization to the Go TUI to show task relationships:
- **Cyan (ColorCyan)**: Upstream dependencies (tasks the selected task depends on)
- **Magenta (ColorMagenta)**: Downstream dependents (tasks that depend on the selected task)

## Files to Modify

### 1. `/internal/tui/lanes_colored_deps_test.go` (NEW FILE)

Create comprehensive tests for two new functions:

#### Test Coverage for `buildSelectedTaskRelationGraph`:
- Empty selection returns empty sets
- Task not found returns empty sets  
- Root task (no dependencies) returns empty ancestors, all descendants
- Leaf task (no dependents) returns all ancestors, empty descendants
- Middle task returns both ancestors and descendants
- Diamond dependency correctly identifies all ancestors
- Multiple forks correctly identifies all descendants

#### Test Coverage for `buildSelectedTaskRelationLanes`:
- Empty assignments returns empty context
- No relations returns empty context
- Upstream relations populate UpstreamLanes map
- Downstream relations populate DownstreamLanes map
- Mixed relations correctly separate upstream/downstream

### 2. `/internal/tui/lanes.go`

Add two new functions after the existing lane assignment logic:

```go
// buildSelectedTaskRelationGraph builds ancestor and descendant sets for a selected task.
// 
// Ancestors: All tasks this task transitively depends on (walk up DependsOn)
// Descendants: All tasks that transitively depend on this task (walk down)
//
// Returns empty sets if selectedID is empty or not found.
func buildSelectedTaskRelationGraph(tasks []types.ResolvedTask, selectedID string) (ancestors map[string]bool, descendants map[string]bool) {
	ancestors = make(map[string]bool)
	descendants = make(map[string]bool)
	
	if selectedID == "" {
		return
	}
	
	// Build task map for quick lookup
	taskMap := make(map[string]types.ResolvedTask)
	for _, t := range tasks {
		taskMap[t.ID] = t
	}
	
	// Check if selected task exists
	selectedTask, exists := taskMap[selectedID]
	if !exists {
		return
	}
	
	// Build reverse dependency map (who depends on whom)
	reverseDeps := make(map[string][]string)
	for _, t := range tasks {
		for _, depID := range t.DependsOn {
			reverseDeps[depID] = append(reverseDeps[depID], t.ID)
		}
	}
	
	// Walk up: find all ancestors (recursive DFS)
	visited := make(map[string]bool)
	var walkUp func(taskID string)
	walkUp = func(taskID string) {
		if visited[taskID] {
			return
		}
		visited[taskID] = true
		
		task, exists := taskMap[taskID]
		if !exists {
			return
		}
		
		for _, depID := range task.DependsOn {
			ancestors[depID] = true
			walkUp(depID)
		}
	}
	walkUp(selectedID)
	
	// Walk down: find all descendants (recursive DFS)
	visitedDown := make(map[string]bool)
	var walkDown func(taskID string)
	walkDown = func(taskID string) {
		if visitedDown[taskID] {
			return
		}
		visitedDown[taskID] = true
		
		for _, dependentID := range reverseDeps[taskID] {
			descendants[dependentID] = true
			walkDown(dependentID)
		}
	}
	walkDown(selectedID)
	
	return
}

// buildSelectedTaskRelationLanes maps task IDs to lane numbers based on lane assignments.
//
// Returns a context with UpstreamLanes and DownstreamLanes sets for coloring.
func buildSelectedTaskRelationLanes(assignments []LaneAssignment, ancestors, descendants map[string]bool) LanePrefixSegmentContext {
	context := LanePrefixSegmentContext{
		UpstreamLanes:   make(map[int]bool),
		DownstreamLanes: make(map[int]bool),
	}
	
	// Map task IDs to their lanes
	for _, assignment := range assignments {
		if ancestors[assignment.TaskID] {
			context.UpstreamLanes[assignment.Lane] = true
		}
		if descendants[assignment.TaskID] {
			context.DownstreamLanes[assignment.Lane] = true
		}
	}
	
	return context
}
```

### 3. `/internal/tui/tasktree.go`

Modify the `renderLaneTaskLine` function (line 1737) to build and use the relation context:

**Current code (line 1739):**
```go
prefix := GeneratePrefix(assignment, index, tt.laneAssignments, nil)
```

**Updated code:**
```go
// Build relation context if a task is selected
var context *LanePrefixSegmentContext
if tt.SelectedID != "" {
	ancestors, descendants := buildSelectedTaskRelationGraph(tt.laneTasks, tt.SelectedID)
	ctx := buildSelectedTaskRelationLanes(tt.laneAssignments, ancestors, descendants)
	context = &ctx
}

// Generate prefix segments with context for coloring
segments := GeneratePrefixSegments(assignment, index, tt.laneAssignments, context)

// Apply colors based on segment kind
prefix := ""
for _, seg := range segments {
	styledText := seg.Text
	switch seg.Kind {
	case KindUpstream:
		styledText = lipgloss.NewStyle().Foreground(ColorCyan).Render(seg.Text)
	case KindDownstream:
		styledText = lipgloss.NewStyle().Foreground(ColorMagenta).Render(seg.Text)
	// KindNeutral: no color, use default
	}
	prefix += styledText
}
```

## Expected Behavior

When a task is selected in lane view:
- Lane lines connecting to upstream dependencies (tasks this one depends on) render in **cyan**
- Lane lines connecting to downstream dependents (tasks that depend on this one) render in **magenta**  
- Lane lines for unrelated tasks render in default color (neutral)

## Testing Strategy (TDD)

1. **RED**: Write tests for `buildSelectedTaskRelationGraph` covering all edge cases
2. **GREEN**: Implement function to pass tests
3. **RED**: Write tests for `buildSelectedTaskRelationLanes`
4. **GREEN**: Implement function to pass tests
5. **REFACTOR**: Clean up, ensure no duplication
6. **VERIFY**: Run full test suite, manually test in TUI

## Manual Verification

After implementation:
```bash
# Start the TUI in lane view mode
bun run src/runner/index.ts start <project> --tui

# Press 'v' to switch to lane view
# Use j/k to select different tasks
# Verify cyan/magenta colors appear for dependencies/dependents
```

## Integration Points

- **Existing infrastructure**: All color definitions, lane types, and segment structures already exist
- **No breaking changes**: Function is backward compatible (nil context = no coloring)
- **Performance**: Relation graph is rebuilt on each render only if selection changes (could optimize with caching if needed)

## Related Code

- `internal/tui/styles.go`: Color definitions (ColorCyan, ColorMagenta)
- `internal/tui/lanes.go`: Lane infrastructure (LanePrefixSegmentKind, GeneratePrefixSegments)
- `internal/tui/tasktree.go`: Rendering logic (renderLaneTaskLine, viewLaneTree)
