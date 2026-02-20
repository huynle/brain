/**
 * Lane Layout Tests
 *
 * Tests for git-graph style lane rendering of task dependencies.
 * Pure algorithm tests - no React/Ink dependency.
 */

import { describe, it, expect } from 'bun:test';
import type { TaskDisplay } from './types';
import {
  topoSort,
  assignLanes,
  detectMergePoints,
  type LaneAssignment,
} from './lane-layout';

// ---------------------------------------------------------------------------
// Test helper
// ---------------------------------------------------------------------------

function createTask(overrides: Partial<TaskDisplay> = {}): TaskDisplay {
  return {
    id: 'task-1',
    path: 'projects/test/task/task-1.md',
    title: 'Test Task',
    status: 'pending',
    priority: 'medium',
    tags: [],
    dependencies: [],
    dependents: [],
    dependencyTitles: [],
    dependentTitles: [],
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// topoSort
// ---------------------------------------------------------------------------

describe('topoSort', () => {
  it('returns empty array for empty input', () => {
    expect(topoSort([])).toEqual([]);
  });

  it('returns single task unchanged', () => {
    const task = createTask({ id: 'a' });
    const result = topoSort([task]);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('a');
  });

  it('sorts a linear chain so dependencies come first', () => {
    // a -> b -> c  (c depends on b, b depends on a)
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: ['c'] });
    const c = createTask({ id: 'c', dependencies: ['b'], dependents: [] });

    const result = topoSort([c, a, b]); // shuffled input
    const ids = result.map((t) => t.id);
    expect(ids).toEqual(['a', 'b', 'c']);
  });

  it('sorts a fork correctly (one root, two leaves)', () => {
    //   a
    //  / \
    // b   c
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b', 'c'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: [] });
    const c = createTask({ id: 'c', dependencies: ['a'], dependents: [] });

    const result = topoSort([c, b, a]);
    const ids = result.map((t) => t.id);
    // 'a' must come before both 'b' and 'c'
    expect(ids.indexOf('a')).toBe(0);
    expect(ids).toContain('b');
    expect(ids).toContain('c');
  });

  it('sorts a diamond (merge) correctly', () => {
    //   a
    //  / \
    // b   c
    //  \ /
    //   d
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b', 'c'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: ['d'] });
    const c = createTask({ id: 'c', dependencies: ['a'], dependents: ['d'] });
    const d = createTask({ id: 'd', dependencies: ['b', 'c'], dependents: [] });

    const result = topoSort([d, c, b, a]);
    const ids = result.map((t) => t.id);
    // a before b,c; b,c before d
    expect(ids.indexOf('a')).toBeLessThan(ids.indexOf('b'));
    expect(ids.indexOf('a')).toBeLessThan(ids.indexOf('c'));
    expect(ids.indexOf('b')).toBeLessThan(ids.indexOf('d'));
    expect(ids.indexOf('c')).toBeLessThan(ids.indexOf('d'));
  });

  it('handles cycles gracefully (appends cycled tasks at end)', () => {
    // a -> b -> c -> a (cycle), d is independent
    const a = createTask({ id: 'a', dependencies: ['c'], dependents: ['b'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: ['c'] });
    const c = createTask({ id: 'c', dependencies: ['b'], dependents: ['a'] });
    const d = createTask({ id: 'd', dependencies: [], dependents: [] });

    const result = topoSort([a, b, c, d]);
    // Should not throw
    expect(result).toHaveLength(4);
    // d (no deps) should appear before the cycled tasks
    const ids = result.map((t) => t.id);
    expect(ids.indexOf('d')).toBeLessThan(ids.indexOf('a'));
    expect(ids.indexOf('d')).toBeLessThan(ids.indexOf('b'));
    expect(ids.indexOf('d')).toBeLessThan(ids.indexOf('c'));
  });

  it('handles tasks with dependencies referencing non-existent IDs', () => {
    const a = createTask({ id: 'a', dependencies: ['nonexistent'], dependents: [] });
    const result = topoSort([a]);
    // Should not crash; treat missing deps as satisfied
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('a');
  });
});

// ---------------------------------------------------------------------------
// detectMergePoints
// ---------------------------------------------------------------------------

describe('detectMergePoints', () => {
  it('returns empty set for empty input', () => {
    expect(detectMergePoints([])).toEqual(new Set());
  });

  it('returns empty set when no task has 2+ dependencies', () => {
    const a = createTask({ id: 'a', dependencies: [] });
    const b = createTask({ id: 'b', dependencies: ['a'] });
    expect(detectMergePoints([a, b])).toEqual(new Set());
  });

  it('detects a task with 2 in-tree dependencies as a merge point', () => {
    const a = createTask({ id: 'a', dependencies: [] });
    const b = createTask({ id: 'b', dependencies: [] });
    const c = createTask({ id: 'c', dependencies: ['a', 'b'] });
    const result = detectMergePoints([a, b, c]);
    expect(result.has('c')).toBe(true);
    expect(result.size).toBe(1);
  });

  it('only counts in-tree dependencies (ignores refs to tasks not in input)', () => {
    const a = createTask({ id: 'a', dependencies: [] });
    // c depends on 'a' (in tree) and 'missing' (not in tree) — only 1 in-tree dep
    const c = createTask({ id: 'c', dependencies: ['a', 'missing'] });
    const result = detectMergePoints([a, c]);
    expect(result.has('c')).toBe(false);
  });

  it('detects multiple merge points', () => {
    const a = createTask({ id: 'a', dependencies: [] });
    const b = createTask({ id: 'b', dependencies: [] });
    const c = createTask({ id: 'c', dependencies: ['a', 'b'] });
    const d = createTask({ id: 'd', dependencies: ['a', 'b'] });
    const result = detectMergePoints([a, b, c, d]);
    expect(result.has('c')).toBe(true);
    expect(result.has('d')).toBe(true);
    expect(result.size).toBe(2);
  });
});

// ---------------------------------------------------------------------------
// assignLanes
// ---------------------------------------------------------------------------

describe('assignLanes', () => {
  it('returns empty array for empty input', () => {
    expect(assignLanes([])).toEqual([]);
  });

  it('assigns lane 0 to a single task', () => {
    const a = createTask({ id: 'a', dependencies: [], dependents: [] });
    const result = assignLanes([a]);
    expect(result).toHaveLength(1);
    expect(result[0].taskId).toBe('a');
    expect(result[0].lane).toBe(0);
    expect(result[0].isMerge).toBe(false);
    expect(result[0].mergeFromLanes).toEqual([]);
  });

  it('assigns lane 0 to all tasks in a linear chain', () => {
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: ['c'] });
    const c = createTask({ id: 'c', dependencies: ['b'], dependents: [] });

    // Input must be topo-sorted
    const result = assignLanes([a, b, c]);
    expect(result.map((r) => r.lane)).toEqual([0, 0, 0]);
  });

  it('opens new lanes on fork', () => {
    //   a
    //  / \
    // b   c
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b', 'c'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: [] });
    const c = createTask({ id: 'c', dependencies: ['a'], dependents: [] });

    const result = assignLanes([a, b, c]);
    // a on lane 0; one child stays on 0, the other gets a new lane
    expect(result[0].lane).toBe(0); // a
    const lanes = new Set(result.map((r) => r.lane));
    expect(lanes.size).toBe(2); // two distinct lanes used
  });

  it('marks merge points and records mergeFromLanes', () => {
    //   a
    //  / \
    // b   c
    //  \ /
    //   d
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b', 'c'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: ['d'] });
    const c = createTask({ id: 'c', dependencies: ['a'], dependents: ['d'] });
    const d = createTask({ id: 'd', dependencies: ['b', 'c'], dependents: [] });

    const sorted = topoSort([a, b, c, d]);
    const result = assignLanes(sorted);

    const dAssignment = result.find((r) => r.taskId === 'd')!;
    expect(dAssignment.isMerge).toBe(true);
    expect(dAssignment.mergeFromLanes.length).toBeGreaterThanOrEqual(1);
  });

  it('tracks activeLanes correctly', () => {
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b', 'c'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: [] });
    const c = createTask({ id: 'c', dependencies: ['a'], dependents: [] });

    const result = assignLanes([a, b, c]);
    // At the first task (a), only lane 0 is active
    expect(result[0].activeLanes).toContain(0);
  });

  it('reuses freed lanes', () => {
    // a forks to b,c; b finishes (no dependents), then d forks from c
    //   a
    //  / \
    // b   c
    //     |
    //     d
    //     |
    //     e
    const a = createTask({ id: 'a', dependencies: [], dependents: ['b', 'c'] });
    const b = createTask({ id: 'b', dependencies: ['a'], dependents: [] });
    const c = createTask({ id: 'c', dependencies: ['a'], dependents: ['d'] });
    const d = createTask({ id: 'd', dependencies: ['c'], dependents: ['e'] });
    const e = createTask({ id: 'e', dependencies: ['d'], dependents: [] });

    const sorted = topoSort([a, b, c, d, e]);
    const result = assignLanes(sorted);

    // The maximum lane used should be at most 1 (fork from a creates lane 1,
    // but after b ends, lane could be reused)
    const maxLane = Math.max(...result.map((r) => r.lane));
    expect(maxLane).toBeLessThanOrEqual(1);
  });

  it('handles independent tasks (no deps between them)', () => {
    const a = createTask({ id: 'a', dependencies: [], dependents: [] });
    const b = createTask({ id: 'b', dependencies: [], dependents: [] });

    const result = assignLanes([a, b]);
    // Both should get assignments without crashing
    expect(result).toHaveLength(2);
  });
});
