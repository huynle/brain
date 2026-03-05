/**
 * Monitor Service - Unit Tests
 *
 * Tests for MonitorService, focusing on createForFeature() method.
 * Uses a mock BrainService to isolate service logic.
 */

import { describe, it, expect, beforeEach } from "bun:test";
import { MonitorService, MonitorConflictError } from "./monitor-service";
import type { BrainService } from "./brain-service";

// =============================================================================
// Mock BrainService
// =============================================================================

function createMockBrainService(overrides?: {
  listEntries?: Array<{
    id: string;
    path: string;
    title: string;
    tags?: string[];
    generated?: boolean;
    schedule_enabled?: boolean;
    schedule?: string;
  }>;
  saveResult?: { id: string; path: string; title: string; type: string; status: string; link: string };
}): BrainService {
  const entries = overrides?.listEntries ?? [];
  const saveResult = overrides?.saveResult ?? {
    id: "new-task-id",
    path: "projects/test/task/new-task-id.md",
    title: "Monitor: Feature Code Review (feature test-feat)",
    type: "task",
    status: "pending",
    link: "[test](new-task-id)",
  };

  let lastSaveRequest: any = null;

  return {
    list: async (request: any) => {
      // Filter by tags if provided (mimic real BrainService behavior)
      let filtered = entries;
      if (request?.tags?.length) {
        filtered = entries.filter((entry) =>
          request.tags.every((reqTag: string) =>
            (entry.tags ?? []).includes(reqTag),
          ),
        );
      }
      return { entries: filtered, total: filtered.length, limit: request?.limit ?? 200, offset: 0 };
    },
    save: async (request: any) => {
      lastSaveRequest = request;
      return saveResult;
    },
    recall: async (id: string) => {
      const entry = entries.find((e) => e.id === id);
      if (!entry) throw new Error(`Entry not found: ${id}`);
      return entry;
    },
    update: async () => {},
    delete: async () => {},
    // Expose for assertions
    get _lastSaveRequest() {
      return lastSaveRequest;
    },
  } as any;
}

// =============================================================================
// createForFeature tests
// =============================================================================

describe("MonitorService.createForFeature", () => {
  let mockBrainService: ReturnType<typeof createMockBrainService>;
  let service: MonitorService;

  beforeEach(() => {
    mockBrainService = createMockBrainService({
      listEntries: [
        {
          id: "task-001",
          path: "projects/test/task/task-001.md",
          title: "Task 1",
          tags: ["task"],
        },
        {
          id: "task-002",
          path: "projects/test/task/task-002.md",
          title: "Task 2",
          tags: ["task"],
        },
      ],
    });
    service = new MonitorService(mockBrainService as any);
  });

  it("creates a review task with correct metadata", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    const result = await service.createForFeature("feature-review", scope, "brain-api");

    expect(result.id).toBe("new-task-id");
    expect(result.path).toContain("task");
    expect(result.title).toContain("Feature Code Review");
  });

  it("sets status to 'pending'", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.status).toBe("pending");
  });

  it("sets complete_on_idle and execution_mode", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.complete_on_idle).toBe(true);
    expect(req.execution_mode).toBe("current_branch");
  });

  it("sets generated metadata fields", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.generated).toBe(true);
    expect(req.generated_kind).toBe("feature_review");
    expect(req.generated_key).toBe("feature-review:auth-system");
    expect(req.generated_by).toBe("feature-completion-hook");
  });

  it("sets feature_id on the created task", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.feature_id).toBe("auth-system");
  });

  it("does NOT set schedule or schedule_enabled", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.schedule).toBeUndefined();
    expect(req.schedule_enabled).toBeUndefined();
  });

  it("auto-computes depends_on from feature tasks", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.depends_on).toEqual([
      "projects/test/task/task-001.md",
      "projects/test/task/task-002.md",
    ]);
  });

  it("excludes generated tasks from depends_on", async () => {
    mockBrainService = createMockBrainService({
      listEntries: [
        {
          id: "task-001",
          path: "projects/test/task/task-001.md",
          title: "Task 1",
          tags: ["task"],
        },
        {
          id: "gen-001",
          path: "projects/test/task/gen-001.md",
          title: "Generated task",
          tags: ["task", "monitor"],
          generated: true,
        },
      ],
    });
    service = new MonitorService(mockBrainService as any);

    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.depends_on).toEqual(["projects/test/task/task-001.md"]);
    expect(req.depends_on).not.toContain("projects/test/task/gen-001.md");
  });

  it("sets direct_prompt from template buildPrompt", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.direct_prompt).toContain("Feature Code Reviewer");
    expect(req.direct_prompt).toContain("auth-system");
    expect(req.direct_prompt).toContain("brain-api");
  });

  it("includes monitor tag for dedup lookup", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.tags).toContain(
      "monitor:feature-review:feature:auth-system:brain-api",
    );
  });

  it("throws MonitorConflictError if review task already exists", async () => {
    mockBrainService = createMockBrainService({
      listEntries: [
        {
          id: "existing-review",
          path: "projects/test/task/existing-review.md",
          title: "Existing review",
          tags: [
            "monitor",
            "review",
            "monitor:feature-review:feature:auth-system:brain-api",
          ],
          schedule_enabled: false,
          schedule: "",
        },
      ],
    });
    service = new MonitorService(mockBrainService as any);

    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };

    expect(
      service.createForFeature("feature-review", scope, "brain-api"),
    ).rejects.toThrow(MonitorConflictError);
  });

  it("throws for unknown template", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };

    expect(
      service.createForFeature("nonexistent-template", scope, "brain-api"),
    ).rejects.toThrow("Unknown monitor template: nonexistent-template");
  });

  it("sets type to 'task'", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.type).toBe("task");
  });

  it("sets project on the created task", async () => {
    const scope = {
      type: "feature" as const,
      feature_id: "auth-system",
      project: "brain-api",
    };
    await service.createForFeature("feature-review", scope, "brain-api");

    const req = (mockBrainService as any)._lastSaveRequest;
    expect(req.project).toBe("brain-api");
  });
});

// =============================================================================
// delete tests
// =============================================================================

describe("MonitorService.delete", () => {
  it("resolves taskId via recall and calls delete with path", async () => {
    let recalledId: string | null = null as string | null;
    let deletedPath: string | null = null as string | null;

    const mockBrainService = {
      recall: async (id: string) => {
        recalledId = id;
        return {
          id: "abc12def",
          path: "projects/test/task/abc12def.md",
          title: "Review task",
        };
      },
      delete: async (path: string) => {
        deletedPath = path;
      },
    } as any;

    const service = new MonitorService(mockBrainService);
    await service.delete("abc12def");

    expect(recalledId).toBe("abc12def");
    expect(deletedPath).toBe("projects/test/task/abc12def.md");
  });

  it("throws when entry not found", async () => {
    const mockBrainService = {
      recall: async () => {
        throw new Error("No entry found: nonexistent");
      },
      delete: async () => {},
    } as any;

    const service = new MonitorService(mockBrainService);
    expect(service.delete("nonexistent")).rejects.toThrow("No entry found");
  });
});

// =============================================================================
// find tests
// =============================================================================

describe("MonitorService.find", () => {
  it("returns null when no monitor exists for template+scope", async () => {
    const mockBrainService = createMockBrainService({ listEntries: [] });
    const service = new MonitorService(mockBrainService as any);

    const result = await service.find("feature-review", {
      type: "feature",
      feature_id: "auth-system",
      project: "brain-api",
    });
    expect(result).toBeNull();
  });

  it("returns monitor info when one exists", async () => {
    const mockBrainService = createMockBrainService({
      listEntries: [
        {
          id: "existing-review",
          path: "projects/test/task/existing-review.md",
          title: "Feature review",
          tags: [
            "monitor",
            "review",
            "monitor:feature-review:feature:auth-system:brain-api",
          ],
          schedule_enabled: false,
          schedule: "",
        },
      ],
    });
    const service = new MonitorService(mockBrainService as any);

    const result = await service.find("feature-review", {
      type: "feature",
      feature_id: "auth-system",
      project: "brain-api",
    });
    expect(result).not.toBeNull();
    expect(result!.id).toBe("existing-review");
    expect(result!.path).toBe("projects/test/task/existing-review.md");
    expect(result!.enabled).toBe(false);
  });
});

// =============================================================================
// Create/delete/recreate cycle
// =============================================================================

describe("MonitorService create-delete-recreate cycle", () => {
  it("can create, then dedup prevents second create, delete clears, recreate succeeds", async () => {
    // State: tracks created entries and deletion
    const createdEntries: Array<{ id: string; path: string; tags: string[]; generated: boolean }> = [];
    let lastSaveRequest: any = null;
    let deletedPath: string | null = null as string | null;

    const mockBrainService = {
      list: async (request: any) => {
        // Filter by tags for dedup check
        let filtered = createdEntries;
        if (request?.tags?.length) {
          filtered = createdEntries.filter((entry) =>
            request.tags.every((reqTag: string) =>
              (entry.tags ?? []).includes(reqTag),
            ),
          );
        }
        // Also return feature tasks for depends_on computation
        if (request?.feature_id || (!request?.tags?.length && !deletedPath)) {
          const featureTasks = [
            { id: "t1", path: "projects/test/task/t1.md", title: "Task 1", tags: [], generated: false },
            { id: "t2", path: "projects/test/task/t2.md", title: "Task 2", tags: [], generated: false },
          ];
          return {
            entries: [...filtered, ...featureTasks.filter((t) => !filtered.some((f) => f.id === t.id))],
            total: filtered.length + featureTasks.length,
            limit: 200,
            offset: 0,
          };
        }
        return { entries: filtered, total: filtered.length, limit: 200, offset: 0 };
      },
      save: async (request: any) => {
        lastSaveRequest = request;
        const entry = {
          id: "review-001",
          path: "projects/test/task/review-001.md",
          title: request.title,
          tags: request.tags,
          generated: true,
        };
        createdEntries.push(entry);
        return { ...entry, type: "task", status: "pending", link: "[test](review-001)" };
      },
      recall: async (id: string) => {
        const entry = createdEntries.find((e) => e.id === id);
        if (!entry) throw new Error(`Entry not found: ${id}`);
        return entry;
      },
      delete: async (path: string) => {
        deletedPath = path;
        const idx = createdEntries.findIndex((e) => e.path === path);
        if (idx >= 0) createdEntries.splice(idx, 1);
      },
    } as any;

    const service = new MonitorService(mockBrainService);
    const scope = { type: "feature" as const, feature_id: "dark-mode", project: "brain-api" };

    // Step 1: Create succeeds
    const result = await service.createForFeature("feature-review", scope, "brain-api");
    expect(result.id).toBe("review-001");
    expect(lastSaveRequest.depends_on).toContain("projects/test/task/t1.md");
    expect(lastSaveRequest.depends_on).toContain("projects/test/task/t2.md");

    // Step 2: Second create throws MonitorConflictError (dedup)
    expect(
      service.createForFeature("feature-review", scope, "brain-api"),
    ).rejects.toThrow(MonitorConflictError);

    // Step 3: Delete clears it
    await service.delete("review-001");
    expect(deletedPath).toBe("projects/test/task/review-001.md");
    expect(createdEntries.length).toBe(0);

    // Step 4: Recreate succeeds (no stale dedup)
    const result2 = await service.createForFeature("feature-review", scope, "brain-api");
    expect(result2.id).toBe("review-001");
  });
});
