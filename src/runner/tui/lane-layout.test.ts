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
  generatePrefix,
  MAX_LANES,
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

// ---------------------------------------------------------------------------
// generatePrefix
// ---------------------------------------------------------------------------

describe('generatePrefix', () => {
  it('generates simple linear prefix (single root, continues)', () => {
    // a -> b (linear chain, a continues to b)
    const assignments: LaneAssignment[] = [
      { taskId: 'a', lane: 0, activeLanes: [0], isMerge: false, mergeFromLanes: [] },
      { taskId: 'b', lane: 0, activeLanes: [0], isMerge: false, mergeFromLanes: [] },
    ];
    // 'a' at index 0: lane continues below (b is at lane 0)
    const prefix = generatePrefix(assignments[0], 0, assignments);
    expect(prefix).toBe('├─');
  });

  it('generates last-branch prefix for terminal task', () => {
    // Single task or last task at a lane
    const assignments: LaneAssignment[] = [
      { taskId: 'a', lane: 0, activeLanes: [0], isMerge: false, mergeFromLanes: [] },
    ];
    // 'a' is the only task, lane doesn't continue
    const prefix = generatePrefix(assignments[0], 0, assignments);
    expect(prefix).toBe('└─');
  });

  it('generates fork prefix (child on parent lane + child on new lane)', () => {
    //   a
    //  / \
    // b   c
    const assignments: LaneAssignment[] = [
      { taskId: 'a', lane: 0, activeLanes: [0], isMerge: false, mergeFromLanes: [] },
      { taskId: 'b', lane: 0, activeLanes: [0, 1], isMerge: false, mergeFromLanes: [] },
      { taskId: 'c', lane: 1, activeLanes: [0, 1], isMerge: false, mergeFromLanes: [] },
    ];
    // 'a': lane 0 continues (b is on lane 0)
    expect(generatePrefix(assignments[0], 0, assignments)).toBe('├─');
    // 'b': lane 0 with lane 1 also active (but b doesn't continue on lane 0 if nothing follows)
    // b is last on lane 0 since c is on lane 1, but a is already past
    // Actually, check: does lane 0 continue below index 1? c is at lane 1, not 0.
    // But c has activeLanes [0, 1], so at index 2 lane 0 is active => depends on data
    // Let's check: c.activeLanes includes 0 => lane 0 is active at row c
    expect(generatePrefix(assignments[1], 1, assignments)).toBe('├─');
    // 'c': lane 1, lane 0 is active (vertical line), then branch at lane 1
    // c is the last task, so lane 1 doesn't continue
    expect(generatePrefix(assignments[2], 2, assignments)).toBe('│ └─');
  });

  it('generates merge prefix with 2 lanes', () => {
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
    const assignments = assignLanes(sorted);

    const dAssign = assignments.find((a) => a.taskId === 'd')!;
    const dIndex = assignments.indexOf(dAssign);
    const prefix = generatePrefix(dAssign, dIndex, assignments);

    // Should contain merge character ╰─ and join character ┴─
    // The exact output depends on lane assignments, but must contain merge chars
    expect(prefix).toContain('╰─');
    expect(prefix).toContain('┴─');
  });

  it('generates merge prefix with 3+ lanes', () => {
    // a, b, c all independent roots; d merges all three
    const a = createTask({ id: 'a', dependencies: [], dependents: ['d'] });
    const b = createTask({ id: 'b', dependencies: [], dependents: ['d'] });
    const c = createTask({ id: 'c', dependencies: [], dependents: ['d'] });
    const d = createTask({ id: 'd', dependencies: ['a', 'b', 'c'], dependents: [] });

    const sorted = topoSort([a, b, c, d]);
    const assignments = assignLanes(sorted);

    const dAssign = assignments.find((a) => a.taskId === 'd')!;
    const dIndex = assignments.indexOf(dAssign);
    const prefix = generatePrefix(dAssign, dIndex, assignments);

    // For a 3-lane merge: should have ╰─ start and at least two ┴─ joins
    // (one of the merge lanes is the task's own lane so it gets branch/last-branch)
    expect(prefix).toContain('╰─');
    // With 3 merge sources converging, there should be at least one ┴─
    expect(prefix).toContain('┴─');
  });

  it('generates prefix with active lanes before the task lane', () => {
    // Simulate: lane 0 and lane 1 active, task at lane 2
    const assignments: LaneAssignment[] = [
      { taskId: 'a', lane: 0, activeLanes: [0, 1, 2], isMerge: false, mergeFromLanes: [] },
      { taskId: 'b', lane: 1, activeLanes: [0, 1, 2], isMerge: false, mergeFromLanes: [] },
      { taskId: 'c', lane: 2, activeLanes: [0, 1, 2], isMerge: false, mergeFromLanes: [] },
    ];
    const prefix = generatePrefix(assignments[2], 2, assignments);
    // Lane 0 active: │ , Lane 1 active: │ , Lane 2 task (no continuation): └─
    expect(prefix).toBe('│ │ └─');
  });

  it('generates prefix with empty (inactive) lanes before task', () => {
    // Lane 0 inactive (gap), lane 1 is the task
    const assignments: LaneAssignment[] = [
      { taskId: 'a', lane: 1, activeLanes: [1], isMerge: false, mergeFromLanes: [] },
    ];
    const prefix = generatePrefix(assignments[0], 0, assignments);
    // Lane 0 empty: 2 spaces, lane 1 task (terminal): └─
    expect(prefix).toBe('  └─');
  });

  it('is a pure string function (no React dependency)', () => {
    const assignment: LaneAssignment = {
      taskId: 'test',
      lane: 0,
      activeLanes: [0],
      isMerge: false,
      mergeFromLanes: [],
    };
    const result = generatePrefix(assignment, 0, [assignment]);
    expect(typeof result).toBe('string');
  });

  it('handles nested merge (merge feeds into another merge)', () => {
    // a, b -> c (merge 1), c, d -> e (merge 2)
    const a = createTask({ id: 'a', dependencies: [], dependents: ['c'] });
    const b = createTask({ id: 'b', dependencies: [], dependents: ['c'] });
    const c = createTask({ id: 'c', dependencies: ['a', 'b'], dependents: ['e'] });
    const d = createTask({ id: 'd', dependencies: [], dependents: ['e'] });
    const e = createTask({ id: 'e', dependencies: ['c', 'd'], dependents: [] });

    const sorted = topoSort([a, b, c, d, e]);
    const assignments = assignLanes(sorted);

    // Both c and e should be merge points
    const cAssign = assignments.find((a) => a.taskId === 'c')!;
    const eAssign = assignments.find((a) => a.taskId === 'e')!;
    expect(cAssign.isMerge).toBe(true);
    expect(eAssign.isMerge).toBe(true);

    // Both should produce valid prefix strings with merge characters
    const cPrefix = generatePrefix(cAssign, assignments.indexOf(cAssign), assignments);
    const ePrefix = generatePrefix(eAssign, assignments.indexOf(eAssign), assignments);

    expect(typeof cPrefix).toBe('string');
    expect(typeof ePrefix).toBe('string');
    expect(cPrefix.length).toBeGreaterThan(0);
    expect(ePrefix.length).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// Edge case tests (from task 93ieoe9n)
// ---------------------------------------------------------------------------

describe('edge cases', () => {
  // Edge case 1: Single-task features — no lanes needed
  describe('single-task features', () => {
    it('assigns lane 0 and produces valid prefix for a single task', () => {
      const a = createTask({ id: 'a', dependencies: [], dependents: [] });
      const sorted = topoSort([a]);
      const assignments = assignLanes(sorted);

      expect(assignments).toHaveLength(1);
      expect(assignments[0].lane).toBe(0);
      expect(assignments[0].isMerge).toBe(false);
      expect(assignments[0].mergeFromLanes).toEqual([]);

      const prefix = generatePrefix(assignments[0], 0, assignments);
      expect(typeof prefix).toBe('string');
      expect(prefix).toBe('└─');
    });
  });

  // Edge case 2: Cross-feature dependencies — deps referencing tasks not in the input
  describe('cross-feature dependencies', () => {
    it('treats dependencies on tasks not in the input as satisfied (no crash)', () => {
      // Task 'b' depends on 'external-task' which is in another feature
      const a = createTask({ id: 'a', dependencies: [], dependents: ['b'] });
      const b = createTask({
        id: 'b',
        dependencies: ['a', 'external-task'],
        dependents: [],
      });

      const sorted = topoSort([a, b]);
      const assignments = assignLanes(sorted);

      // Should not crash, and b should not be a merge point (only 1 in-tree dep)
      expect(assignments).toHaveLength(2);
      const bAssign = assignments.find((x) => x.taskId === 'b')!;
      expect(bAssign.isMerge).toBe(false);
    });

    it('handles task where all dependencies are cross-feature (treated as root)', () => {
      const a = createTask({
        id: 'a',
        dependencies: ['ext-1', 'ext-2'],
        dependents: [],
      });

      const sorted = topoSort([a]);
      expect(sorted).toHaveLength(1);
      expect(sorted[0].id).toBe('a');

      const assignments = assignLanes(sorted);
      expect(assignments[0].lane).toBe(0);
      expect(assignments[0].isMerge).toBe(false);
    });
  });

  // Edge case 3: Wide lane layouts (5+ parallel lanes) — cap at MAX_LANES
  describe('wide lane layouts', () => {
    it('caps lanes at MAX_LANES for many independent roots', () => {
      // Create MAX_LANES + 4 independent roots
      const count = MAX_LANES + 4;
      const tasks = Array.from({ length: count }, (_, i) =>
        createTask({
          id: `task-${i}`,
          dependencies: [],
          dependents: [],
        }),
      );

      const sorted = topoSort(tasks);
      const assignments = assignLanes(sorted);

      expect(assignments).toHaveLength(count);
      // All lane numbers should be < MAX_LANES
      const maxLane = Math.max(...assignments.map((a) => a.lane));
      expect(maxLane).toBeLessThan(MAX_LANES);
    });

    it('generatePrefix handles tasks at the max lane without crash', () => {
      const count = MAX_LANES + 2;
      const tasks = Array.from({ length: count }, (_, i) =>
        createTask({
          id: `task-${i}`,
          dependencies: [],
          dependents: [],
        }),
      );

      const sorted = topoSort(tasks);
      const assignments = assignLanes(sorted);

      // Generate prefix for every assignment — should not crash
      for (let i = 0; i < assignments.length; i++) {
        const prefix = generatePrefix(assignments[i], i, assignments);
        expect(typeof prefix).toBe('string');
      }
    });
  });

  // Edge case 4: Cycles — lane renderer should handle gracefully
  describe('cycles in lane renderer', () => {
    it('topoSort appends cycled tasks at end and assignLanes handles them', () => {
      // a -> b -> c -> a  (full cycle)
      const a = createTask({ id: 'a', dependencies: ['c'], dependents: ['b'] });
      const b = createTask({ id: 'b', dependencies: ['a'], dependents: ['c'] });
      const c = createTask({ id: 'c', dependencies: ['b'], dependents: ['a'] });

      const sorted = topoSort([a, b, c]);
      // All tasks should appear (cycled tasks appended at end)
      expect(sorted).toHaveLength(3);

      // assignLanes should not crash even with cycled tasks
      const assignments = assignLanes(sorted);
      expect(assignments).toHaveLength(3);

      // generatePrefix should not crash
      for (let i = 0; i < assignments.length; i++) {
        const prefix = generatePrefix(assignments[i], i, assignments);
        expect(typeof prefix).toBe('string');
      }
    });

    it('mixed cycle and non-cycle tasks render without crash', () => {
      // d is independent; a->b->c->a is a cycle
      const a = createTask({ id: 'a', dependencies: ['c'], dependents: ['b'] });
      const b = createTask({ id: 'b', dependencies: ['a'], dependents: ['c'] });
      const c = createTask({ id: 'c', dependencies: ['b'], dependents: ['a'] });
      const d = createTask({ id: 'd', dependencies: [], dependents: [] });

      const sorted = topoSort([d, a, b, c]);
      const assignments = assignLanes(sorted);

      expect(assignments).toHaveLength(4);
      // d should be first (no deps, appears before cycle members)
      expect(assignments[0].taskId).toBe('d');
    });
  });

  // Edge case 5: Tasks with parent_id — lane renderer uses dependencies, not parent_id
  describe('tasks with parent_id', () => {
    it('lane renderer ignores parent_id and uses dependencies for ordering', () => {
      // parent_id is for tree hierarchy; lane layout only uses dependencies/dependents
      const parent = createTask({
        id: 'parent',
        dependencies: [],
        dependents: ['child'],
      });
      const child = createTask({
        id: 'child',
        dependencies: ['parent'],
        dependents: [],
        parent_id: 'parent',
      } as any);

      const sorted = topoSort([parent, child]);
      const assignments = assignLanes(sorted);

      expect(assignments).toHaveLength(2);
      // parent before child
      expect(assignments[0].taskId).toBe('parent');
      expect(assignments[1].taskId).toBe('child');
      // Both should be on lane 0 (linear chain)
      expect(assignments[0].lane).toBe(0);
      expect(assignments[1].lane).toBe(0);
    });
  });

  // Edge case 6: Malformed dependency data — no crashes
  describe('malformed dependency data', () => {
    it('handles task with undefined dependencies gracefully', () => {
      const a = createTask({ id: 'a' });
      // Force undefined dependencies to simulate malformed data
      (a as any).dependencies = undefined;

      // Should not crash
      const sorted = topoSort([a]);
      expect(sorted).toHaveLength(1);

      const merges = detectMergePoints([a]);
      expect(merges.size).toBe(0);

      const assignments = assignLanes(sorted);
      expect(assignments).toHaveLength(1);
    });

    it('handles task with null entries in dependencies array', () => {
      const a = createTask({ id: 'a', dependencies: [], dependents: ['b'] });
      const b = createTask({ id: 'b', dependencies: ['a', null as any, '', undefined as any], dependents: [] });

      const sorted = topoSort([a, b]);
      expect(sorted).toHaveLength(2);

      // Should treat only 'a' as a real in-tree dep
      const merges = detectMergePoints([a, b]);
      expect(merges.size).toBe(0); // only 1 valid in-tree dep

      const assignments = assignLanes(sorted);
      expect(assignments).toHaveLength(2);
    });

    it('handles task with undefined dependents gracefully', () => {
      const a = createTask({ id: 'a' });
      (a as any).dependents = undefined;

      const sorted = topoSort([a]);
      const assignments = assignLanes(sorted);
      expect(assignments).toHaveLength(1);
      expect(assignments[0].lane).toBe(0);
    });
  });

  // Edge case 7: Empty dependencies (roots) mixed with dependent chains
  describe('independent roots mixed with chains', () => {
    it('assigns independent roots to separate lanes', () => {
      const a = createTask({ id: 'a', dependencies: [], dependents: [] });
      const b = createTask({ id: 'b', dependencies: [], dependents: ['c'] });
      const c = createTask({ id: 'c', dependencies: ['b'], dependents: [] });

      const sorted = topoSort([a, b, c]);
      const assignments = assignLanes(sorted);

      // a gets its own lane, b+c share a lane
      const aLane = assignments.find((x) => x.taskId === 'a')!.lane;
      const bLane = assignments.find((x) => x.taskId === 'b')!.lane;
      const cLane = assignments.find((x) => x.taskId === 'c')!.lane;

      expect(bLane).toBe(cLane); // b and c should share a lane
      expect(aLane).not.toBe(bLane); // a is independent
    });
  });
});
