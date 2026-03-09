# Colored Dependency Visualization - Implementation Summary

## Objective
Add colored lane visualization to the Go TUI showing task relationships:
- **Cyan (ColorCyan)**: Upstream dependencies (tasks the selected task depends on)
- **Magenta (ColorMagenta)**: Downstream dependents (tasks that depend on the selected task)

## Current State Analysis

### Infrastructure Already Exists ✅
1. **Color definitions** in `internal/tui/styles.go`:
   - `ColorCyan = lipgloss.Color("6")` (line 16)
   - `ColorMagenta = lipgloss.Color("5")` (line 17)

2. **Lane segment types** in `internal/tui/lanes.go`:
   - `LanePrefixSegmentKind` enum (lines 40-46)
   - `KindNeutral`, `KindUpstream`, `KindDownstream` constants
   - `LanePrefixSegmentContext` struct (lines 398-401)
   - `GeneratePrefixSegments()` function (lines 428-441)

3. **Rendering infrastructure** in `internal/tui/tasktree.go`:
   - `renderLaneTaskLine()` function (line 1737)
   - Lane view mode with `laneTasks` and `laneAssignments` fields

### What's Missing ❌
1. **Graph builder**: Function to compute ancestors/descendants for selected task
2. **Lane mapper**: Function to map task IDs → lane numbers for coloring
3. **Colored rendering**: Connection between relation graph and `GeneratePrefixSegments`

## Implementation Plan (TDD)

### File 1: `internal/tui/lanes_colored_deps_test.go` (NEW)
Create comprehensive test suite with 14 test cases:

#### `buildSelectedTaskRelationGraph` tests (8 cases):
- No selection → empty sets
- Task not found → empty sets
- Root task → 0 ancestors, all descendants
- Leaf task → all ancestors, 0 descendants
- Middle task → both ancestors and descendants
- Diamond dependency → correct ancestor traversal
- Diamond middle selection → correct bidirectional traversal
- Multiple forks → all descendants found

#### `buildSelectedTaskRelationLanes` tests (6 cases):
- Empty assignments → empty context
- No relations → empty context
- Upstream only → UpstreamLanes populated
- Downstream only → DownstreamLanes populated  
- Mixed relations → both maps populated correctly

### File 2: `internal/tui/lanes.go` (ADD ~80 lines)
Add two functions at end of file (after line 627):

```go
// buildSelectedTaskRelationGraph builds ancestor and descendant sets.
// Uses DFS to walk up dependencies and down dependents.
// Returns empty sets if selectedID is "" or not found.
func buildSelectedTaskRelationGraph(tasks []types.ResolvedTask, selectedID string) 
    (ancestors map[string]bool, descendants map[string]bool)

// buildSelectedTaskRelationLanes maps task IDs to lane numbers.
// Populates UpstreamLanes and DownstreamLanes for coloring context.
func buildSelectedTaskRelationLanes(assignments []LaneAssignment, 
    ancestors, descendants map[string]bool) LanePrefixSegmentContext
```

**Algorithm for `buildSelectedTaskRelationGraph`:**
1. Early return if `selectedID == ""` or task not found
2. Build task map and reverse dependency map
3. Walk up: recursive DFS following `DependsOn` links → ancestors
4. Walk down: recursive DFS following reverse deps → descendants

**Algorithm for `buildSelectedTaskRelationLanes`:**
1. For each lane assignment, check if `TaskID` is in ancestors or descendants
2. Add lane number to appropriate set (UpstreamLanes or DownstreamLanes)

### File 3: `internal/tui/tasktree.go` (MODIFY renderLaneTaskLine)
Replace line 1739 (single line) with colored rendering logic (~20 lines):

**Current:**
```go
prefix := GeneratePrefix(assignment, index, tt.laneAssignments, nil)
```

**New:**
```go
// Build relation context if task is selected
var context *LanePrefixSegmentContext
if tt.SelectedID != "" {
	ancestors, descendants := buildSelectedTaskRelationGraph(tt.laneTasks, tt.SelectedID)
	ctx := buildSelectedTaskRelationLanes(tt.laneAssignments, ancestors, descendants)
	context = &ctx
}

// Generate segments and apply colors
segments := GeneratePrefixSegments(assignment, index, tt.laneAssignments, context)
prefix := ""
for _, seg := range segments {
	styledText := seg.Text
	switch seg.Kind {
	case KindUpstream:
		styledText = lipgloss.NewStyle().Foreground(ColorCyan).Render(seg.Text)
	case KindDownstream:
		styledText = lipgloss.NewStyle().Foreground(ColorMagenta).Render(seg.Text)
	}
	prefix += styledText
}
```

## Testing Strategy

### RED Phase ❌
1. Create `lanes_colored_deps_test.go` with all 14 tests
2. Run: `go test ./internal/tui -run TestBuildSelectedTask -v`
3. **Expected:** All tests fail (functions don't exist)

### GREEN Phase ✅
1. Add `buildSelectedTaskRelationGraph` to `lanes.go`
2. Run tests → verify first 8 pass
3. Add `buildSelectedTaskRelationLanes` to `lanes.go`
4. Run tests → verify all 14 pass
5. Update `renderLaneTaskLine` in `tasktree.go`
6. Run full suite: `go test ./internal/tui -v`
7. **Expected:** All tests pass including existing ones

### VERIFY Phase ✅
1. Build: `go build -o brain-runner cmd/runner/main.go`
2. Run TUI: `./brain-runner start brain-api --tui`
3. Press `v` to switch to lane view
4. Use `j/k` to navigate and select different tasks
5. **Expected:** 
   - Cyan lane lines for upstream dependencies
   - Magenta lane lines for downstream dependents
   - Default color for unrelated tasks

## Edge Cases Covered

✅ **No selection** → No coloring (context is nil)  
✅ **Task not found** → No coloring (empty sets returned)  
✅ **Root task** → Only downstream coloring  
✅ **Leaf task** → Only upstream coloring  
✅ **Diamond dependencies** → Correct transitive ancestor detection  
✅ **Cycles** → DFS visited set prevents infinite loops  
✅ **Out-of-tree dependencies** → Filtered by task map lookup  

## Performance Considerations

- Relation graph rebuilt on each render only when selection changes
- DFS with visited sets prevents duplicate traversal
- Map lookups are O(1)
- Total complexity: O(V + E) where V = tasks, E = dependencies

**Potential optimization** (if needed):
- Cache relation graph per selectedID
- Invalidate cache only when task list changes
- Not needed initially unless performance issues observed

## Integration Points

| Component | Role | Impact |
|-----------|------|--------|
| `styles.go` | Provides `ColorCyan`, `ColorMagenta` | No changes needed ✅ |
| `lanes.go` | Lane infrastructure and new functions | Add 2 functions ✅ |
| `tasktree.go` | Rendering logic | Modify 1 function ✅ |
| `types/types.go` | `ResolvedTask` struct | No changes needed ✅ |

## Verification Commands

```bash
# Run new tests only
go test ./internal/tui -run TestBuildSelectedTask -v

# Run all TUI tests
go test ./internal/tui -v

# Check test coverage
go test ./internal/tui -cover

# Build binary
go build -o brain-runner cmd/runner/main.go

# Run TUI
./brain-runner start brain-api --tui
```

## Expected Results

### Test Output
```
=== RUN   TestBuildSelectedTaskRelationGraph_NoSelection
--- PASS: TestBuildSelectedTaskRelationGraph_NoSelection (0.00s)
=== RUN   TestBuildSelectedTaskRelationGraph_TaskNotFound
--- PASS: TestBuildSelectedTaskRelationGraph_TaskNotFound (0.00s)
...
PASS
ok      github.com/huynle/brain-api/internal/tui        0.123s
```

### TUI Behavior
1. Start TUI in any project
2. Press `v` to switch to lane view
3. Select a task with dependencies using `j/k`
4. Observe:
   - Cyan lanes connecting to tasks above (dependencies)
   - Magenta lanes connecting to tasks below (dependents)
   - Clear visual distinction for task relationships

## Files Modified Summary

| File | Change | Lines |
|------|--------|-------|
| `internal/tui/lanes_colored_deps_test.go` | NEW | ~300 |
| `internal/tui/lanes.go` | ADD (end of file) | ~80 |
| `internal/tui/tasktree.go` | MODIFY (line 1739) | +19, -1 |
| **Total** | | **~398** |

## Success Criteria

✅ All 14 new tests pass  
✅ No existing tests broken  
✅ Cyan color appears for upstream dependencies  
✅ Magenta color appears for downstream dependents  
✅ No performance degradation in lane view  
✅ No visual glitches or rendering errors  

## Next Steps

After approval of this design:
1. Create test file with RED phase (failing tests)
2. Implement functions in GREEN phase (passing tests)
3. Update rendering in VERIFY phase (visual confirmation)
4. Run full test suite and manual verification
5. Commit with descriptive message
6. Report completion with test results and screenshots

---

**Implementation ready for execution following strict TDD discipline.**
