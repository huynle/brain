# Debugging TUI Group Visibility

## Problem

Sometimes groups (Ready, Draft, Blocked, etc.) don't appear in the TUI even when they should be visible and contain tasks. This can happen when:

- Viewing the wrong project tab
- Active filters are hiding tasks
- TUI has stale data
- Settings visibility map is misconfigured

## Solution: Debug Logging

Enable debug logging to see exactly why groups are appearing or being skipped:

```bash
# Run brain-runner with debug logging enabled
DEBUG_TUI_GROUPING=1 ./bin/brain-runner start <project> --tui 2> grouping-debug.log

# In another terminal, watch the debug output
tail -f grouping-debug.log
```

## Debug Output Example

When a Draft group is correctly shown:

```
[DEBUG:grouping] GroupTasksByStatusAndFeature called: 5 tasks, 10 visible groups configured
[DEBUG:grouping] Task us7lnrdq: classification= status=draft feature_id= -> group=Draft
[DEBUG:grouping] Group Draft: creating group (visibility=true, 1 tasks, 0 features)
[DEBUG:grouping] Final result: 3 groups created
```

When a Draft group is hidden due to settings:

```
[DEBUG:grouping] GroupTasksByStatusAndFeature called: 5 tasks, 10 visible groups configured
[DEBUG:grouping] Task us7lnrdq: classification= status=draft feature_id= -> group=Draft
[DEBUG:grouping] Group Draft: skipped (visibility=false, 1 tasks hidden)
[DEBUG:grouping] Final result: 2 groups created
```

When a group has no tasks:

```
[DEBUG:grouping] Group Draft: skipped (no tasks)
```

## What to Look For

1. **Task classification** - Check if tasks are being classified into the expected group:
   - `status=draft -> Draft group` ✅
   - `classification=ready -> Ready group` ✅
   
2. **Group visibility** - Check if the group is being created:
   - `Draft: creating group (visibility=true, 1 tasks, 0 features)` ✅
   - `Draft: skipped (visibility=false, 1 tasks hidden)` ❌ (check settings)
   - `Draft: skipped (no tasks)` ⚠️ (wrong project or filter active?)

3. **Final group count** - Count should match visible groups
   - If you expect 4 groups but see "Final result: 2 groups created", something is wrong

## Common Issues

### Issue: Draft group not appearing despite settings showing "■ Draft"

Debug output shows:
```
[DEBUG:grouping] Group Draft: skipped (visibility=false, 1 tasks hidden)
```

**Cause:** Settings file has `"Draft": false` but the UI is showing cached state.

**Fix:** Press `S` to open settings, toggle Draft visibility twice (off then on), then save.

### Issue: Draft group not appearing and no draft tasks in debug output

Debug output shows:
```
[DEBUG:grouping] Group Draft: skipped (no tasks)
```

**Cause:** Either viewing wrong project tab, or filter is active, or no draft tasks exist.

**Fix:** 
- Press `1-9` or `h/l` to switch to correct project tab
- Press `Esc` to clear any active filter
- Verify draft task exists: `curl http://localhost:3333/api/v1/tasks/<project> | jq '.tasks[] | select(.status=="draft")'`

### Issue: Task exists but being classified to wrong group

Debug output shows:
```
[DEBUG:grouping] Task abc123: classification= status=pending feature_id=feat-1 -> group=Ready
```

**Cause:** Task status is `pending`, not `draft`. The API classification logic classifies `pending` → `Ready`.

**Fix:** Update task status to `draft` if it should be in the Draft group.

## Disabling Debug Logging

Debug logging is controlled by the `DEBUG_TUI_GROUPING` environment variable. When not set, no debug output is produced.

To run without debug logging:
```bash
./bin/brain-runner start <project> --tui
```
