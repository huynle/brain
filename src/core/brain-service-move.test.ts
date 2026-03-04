/**
 * Brain Service - moveEntry() dependency rewriting tests
 *
 * Tests for Phase 2: when a task is moved between projects,
 * depends_on references in dependent tasks are rewritten.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { parseFrontmatter } from "./note-utils";
import { computeNewDepRef, rewriteDependentFiles } from "./brain-service";
import type { DependentInfo } from "./task-service";

// =============================================================================
// Pure function tests: computeNewDepRef()
// =============================================================================

describe("computeNewDepRef()", () => {
  const movedTaskId = "task-42";
  const sourceProject = "proj-a";
  const targetProject = "proj-b";

  test("source project dependent: bare ref → cross-project ref", () => {
    // A task in proj-a had bare "task-42". Now task-42 is in proj-b.
    // The dependent is still in proj-a, so it needs "proj-b:task-42".
    const result = computeNewDepRef({
      dependentProjectId: sourceProject,
      movedTaskId,
      sourceProjectId: sourceProject,
      targetProjectId: targetProject,
    });
    expect(result).toBe("proj-b:task-42");
  });

  test("target project dependent: cross-project ref → bare ref", () => {
    // A task in proj-b had "proj-a:task-42". Now task-42 is in proj-b (local).
    // The dependent is in proj-b, so it becomes bare "task-42".
    const result = computeNewDepRef({
      dependentProjectId: targetProject,
      movedTaskId,
      sourceProjectId: sourceProject,
      targetProjectId: targetProject,
    });
    expect(result).toBe("task-42");
  });

  test("third project dependent: old cross-project ref → new cross-project ref", () => {
    // A task in proj-c had "proj-a:task-42". Now task-42 is in proj-b.
    // The dependent is in proj-c, so it becomes "proj-b:task-42".
    const result = computeNewDepRef({
      dependentProjectId: "proj-c",
      movedTaskId,
      sourceProjectId: sourceProject,
      targetProjectId: targetProject,
    });
    expect(result).toBe("proj-b:task-42");
  });

  test("dependent in target project gets bare ref (not self-prefixed)", () => {
    // Edge case: make sure we don't produce "proj-b:task-42" for a proj-b dependent
    const result = computeNewDepRef({
      dependentProjectId: "proj-b",
      movedTaskId: "task-99",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });
    expect(result).toBe("task-99");
  });
});

// =============================================================================
// Integration tests: rewriteDependentFiles()
// =============================================================================

describe("rewriteDependentFiles()", () => {
  let testDir: string;

  beforeEach(() => {
    testDir = join(tmpdir(), `brain-move-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
  });

  afterEach(() => {
    if (existsSync(testDir)) {
      rmSync(testDir, { recursive: true, force: true });
    }
  });

  function createTaskFile(
    projectId: string,
    taskId: string,
    dependsOn: string[],
    extraFrontmatter: Record<string, unknown> = {}
  ): string {
    const dir = join(testDir, "projects", projectId, "task");
    mkdirSync(dir, { recursive: true });
    const filePath = join(dir, `${taskId}.md`);
    const relativePath = `projects/${projectId}/task/${taskId}.md`;

    const depsYaml =
      dependsOn.length > 0
        ? `depends_on:\n${dependsOn.map((d) => `  - "${d}"`).join("\n")}\n`
        : "";

    const extraYaml = Object.entries(extraFrontmatter)
      .map(([k, v]) => `${k}: ${v}`)
      .join("\n");

    writeFileSync(
      filePath,
      `---
title: Task ${taskId}
type: task
status: pending
${depsYaml}${extraYaml ? extraYaml + "\n" : ""}---

Content for ${taskId}.
`
    );

    return relativePath;
  }

  test("rewrites bare ref to cross-project when dependent is in source project", () => {
    // Setup: proj-a has task-1 depending on task-2 (bare ref). task-2 moves to proj-b.
    const depPath = createTaskFile("proj-a", "task-1", ["task-2", "task-99"]);

    const dependents: DependentInfo[] = [
      {
        taskId: "task-1",
        taskPath: depPath,
        projectId: "proj-a",
        depRef: "task-2",
      },
    ];

    const result = rewriteDependentFiles({
      brainDir: testDir,
      dependents,
      movedTaskId: "task-2",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      taskId: "task-1",
      project: "proj-a",
      oldRef: "task-2",
      newRef: "proj-b:task-2",
    });

    // Verify actual file content
    const content = readFileSync(join(testDir, depPath), "utf-8");
    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.depends_on).toEqual(["proj-b:task-2", "task-99"]);
  });

  test("rewrites cross-project ref to bare when dependent is in target project", () => {
    // Setup: proj-b has task-10 depending on "proj-a:task-2". task-2 moves to proj-b.
    const depPath = createTaskFile("proj-b", "task-10", [
      "proj-a:task-2",
      "other-dep",
    ]);

    const dependents: DependentInfo[] = [
      {
        taskId: "task-10",
        taskPath: depPath,
        projectId: "proj-b",
        depRef: "proj-a:task-2",
      },
    ];

    const result = rewriteDependentFiles({
      brainDir: testDir,
      dependents,
      movedTaskId: "task-2",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      taskId: "task-10",
      project: "proj-b",
      oldRef: "proj-a:task-2",
      newRef: "task-2",
    });

    // Verify actual file content
    const content = readFileSync(join(testDir, depPath), "utf-8");
    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.depends_on).toEqual(["task-2", "other-dep"]);
  });

  test("rewrites cross-project ref in third project", () => {
    // Setup: proj-c has task-20 depending on "proj-a:task-2". task-2 moves to proj-b.
    const depPath = createTaskFile("proj-c", "task-20", ["proj-a:task-2"]);

    const dependents: DependentInfo[] = [
      {
        taskId: "task-20",
        taskPath: depPath,
        projectId: "proj-c",
        depRef: "proj-a:task-2",
      },
    ];

    const result = rewriteDependentFiles({
      brainDir: testDir,
      dependents,
      movedTaskId: "task-2",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });

    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({
      taskId: "task-20",
      project: "proj-c",
      oldRef: "proj-a:task-2",
      newRef: "proj-b:task-2",
    });

    // Verify actual file content
    const content = readFileSync(join(testDir, depPath), "utf-8");
    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.depends_on).toEqual(["proj-b:task-2"]);
  });

  test("returns empty array when no dependents", () => {
    const result = rewriteDependentFiles({
      brainDir: testDir,
      dependents: [],
      movedTaskId: "task-2",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });

    expect(result).toEqual([]);
  });

  test("rewrites multiple dependents across multiple projects", () => {
    const depPath1 = createTaskFile("proj-a", "task-1", ["task-2"]);
    const depPath2 = createTaskFile("proj-b", "task-10", ["proj-a:task-2"]);
    const depPath3 = createTaskFile("proj-c", "task-20", ["proj-a:task-2"]);

    const dependents: DependentInfo[] = [
      {
        taskId: "task-1",
        taskPath: depPath1,
        projectId: "proj-a",
        depRef: "task-2",
      },
      {
        taskId: "task-10",
        taskPath: depPath2,
        projectId: "proj-b",
        depRef: "proj-a:task-2",
      },
      {
        taskId: "task-20",
        taskPath: depPath3,
        projectId: "proj-c",
        depRef: "proj-a:task-2",
      },
    ];

    const result = rewriteDependentFiles({
      brainDir: testDir,
      dependents,
      movedTaskId: "task-2",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });

    expect(result).toHaveLength(3);

    // Source project: bare → cross-project
    expect(result[0]).toEqual({
      taskId: "task-1",
      project: "proj-a",
      oldRef: "task-2",
      newRef: "proj-b:task-2",
    });

    // Target project: cross-project → bare
    expect(result[1]).toEqual({
      taskId: "task-10",
      project: "proj-b",
      oldRef: "proj-a:task-2",
      newRef: "task-2",
    });

    // Third project: old cross-project → new cross-project
    expect(result[2]).toEqual({
      taskId: "task-20",
      project: "proj-c",
      oldRef: "proj-a:task-2",
      newRef: "proj-b:task-2",
    });

    // Verify all files on disk
    const content1 = readFileSync(join(testDir, depPath1), "utf-8");
    expect(parseFrontmatter(content1).frontmatter.depends_on).toEqual([
      "proj-b:task-2",
    ]);

    const content2 = readFileSync(join(testDir, depPath2), "utf-8");
    expect(parseFrontmatter(content2).frontmatter.depends_on).toEqual([
      "task-2",
    ]);

    const content3 = readFileSync(join(testDir, depPath3), "utf-8");
    expect(parseFrontmatter(content3).frontmatter.depends_on).toEqual([
      "proj-b:task-2",
    ]);
  });

  test("preserves other frontmatter fields when rewriting", () => {
    const depPath = createTaskFile("proj-a", "task-1", ["task-2"], {
      priority: "high",
      projectId: "proj-a",
    });

    const dependents: DependentInfo[] = [
      {
        taskId: "task-1",
        taskPath: depPath,
        projectId: "proj-a",
        depRef: "task-2",
      },
    ];

    rewriteDependentFiles({
      brainDir: testDir,
      dependents,
      movedTaskId: "task-2",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });

    const content = readFileSync(join(testDir, depPath), "utf-8");
    const { frontmatter, body } = parseFrontmatter(content);

    // depends_on rewritten
    expect(frontmatter.depends_on).toEqual(["proj-b:task-2"]);
    // Other fields preserved
    expect(frontmatter.title).toBe("Task task-1");
    expect(frontmatter.type).toBe("task");
    expect(frontmatter.status).toBe("pending");
    expect(frontmatter.priority).toBe("high");
    expect(frontmatter.projectId).toBe("proj-a");
    // Body preserved
    expect(body).toContain("Content for task-1.");
  });

  test("skips rewrite if dependent file does not exist (graceful)", () => {
    const dependents: DependentInfo[] = [
      {
        taskId: "ghost-task",
        taskPath: "projects/proj-a/task/ghost-task.md",
        projectId: "proj-a",
        depRef: "task-2",
      },
    ];

    // Should not throw, just skip
    const result = rewriteDependentFiles({
      brainDir: testDir,
      dependents,
      movedTaskId: "task-2",
      sourceProjectId: "proj-a",
      targetProjectId: "proj-b",
    });

    expect(result).toEqual([]);
  });
});

// =============================================================================
// Schema test: MoveEntryResponseSchema includes updatedDependents
// =============================================================================

import { MoveEntryResponseSchema } from "../api/schemas";

describe("MoveEntryResponseSchema", () => {
  test("accepts response with updatedDependents", () => {
    const data = {
      oldPath: "projects/proj-a/task/ab12cd34.md",
      newPath: "projects/proj-b/task/ab12cd34.md",
      project: "proj-b",
      id: "ab12cd34",
      title: "My Task",
      updatedDependents: [
        {
          taskId: "ef56gh78",
          project: "proj-a",
          oldRef: "ab12cd34",
          newRef: "proj-b:ab12cd34",
        },
      ],
    };

    const result = MoveEntryResponseSchema.safeParse(data);
    expect(result.success).toBe(true);
  });

  test("accepts response with empty updatedDependents", () => {
    const data = {
      oldPath: "projects/proj-a/task/ab12cd34.md",
      newPath: "projects/proj-b/task/ab12cd34.md",
      project: "proj-b",
      id: "ab12cd34",
      title: "My Task",
      updatedDependents: [],
    };

    const result = MoveEntryResponseSchema.safeParse(data);
    expect(result.success).toBe(true);
  });

  test("rejects response without updatedDependents", () => {
    const data = {
      oldPath: "projects/proj-a/task/ab12cd34.md",
      newPath: "projects/proj-b/task/ab12cd34.md",
      project: "proj-b",
      id: "ab12cd34",
      title: "My Task",
    };

    const result = MoveEntryResponseSchema.safeParse(data);
    expect(result.success).toBe(false);
  });
});
