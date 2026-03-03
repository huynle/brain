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
