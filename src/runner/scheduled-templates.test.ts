/**
 * Tests for Scheduled Task Templates
 *
 * Tests template registry, prompt generation, title/tag builders,
 * and API integration (create, find, toggle) with mocked fetch.
 */

import {
  describe,
  it,
  expect,
  beforeEach,
  afterEach,
  mock,
} from "bun:test";
import {
  TEMPLATES,
  TEMPLATE_LIST,
  buildScheduledTaskTitle,
  buildScheduledTaskTag,
  createScheduledTask,
  findScheduledTask,
  toggleScheduledTask,
  type TemplateScope,
  type ScheduledTaskTemplate,
} from "./scheduled-templates";

// Helper to create a mock fetch function
function createMockFetch(handler: () => Promise<Response>) {
  const mockFn = mock(handler) as unknown as typeof fetch;
  return mockFn;
}

describe("Scheduled Templates", () => {
  let originalFetch: typeof fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  // ========================================
  // Template Registry
  // ========================================

  describe("TEMPLATES registry", () => {
    it("has blocked-inspector template", () => {
      expect(TEMPLATES["blocked-inspector"]).toBeDefined();
    });

    it("blocked-inspector has all required fields", () => {
      const t = TEMPLATES["blocked-inspector"];
      expect(t.id).toBe("blocked-inspector");
      expect(t.label).toBe("Blocked Task Inspector");
      expect(typeof t.description).toBe("string");
      expect(t.description.length).toBeGreaterThan(0);
      expect(t.schedule).toBe("*/15 * * * *");
      expect(typeof t.buildPrompt).toBe("function");
      expect(t.complete_on_idle).toBe(true);
      expect(t.execution_mode).toBe("current_branch");
      expect(Array.isArray(t.tags)).toBe(true);
      expect(t.tags.length).toBeGreaterThan(0);
    });
  });

  describe("TEMPLATE_LIST", () => {
    it("is an array derived from TEMPLATES", () => {
      expect(Array.isArray(TEMPLATE_LIST)).toBe(true);
      expect(TEMPLATE_LIST.length).toBe(Object.keys(TEMPLATES).length);
    });

    it("contains the blocked-inspector template", () => {
      const found = TEMPLATE_LIST.find((t) => t.id === "blocked-inspector");
      expect(found).toBeDefined();
      expect(found).toBe(TEMPLATES["blocked-inspector"]);
    });
  });

  // ========================================
  // buildScheduledTaskTitle
  // ========================================

  describe("buildScheduledTaskTitle", () => {
    const template = TEMPLATES["blocked-inspector"];

    it("builds title for scope 'all'", () => {
      const scope: TemplateScope = { type: "all" };
      const title = buildScheduledTaskTitle(template, scope);
      expect(title).toBe("Blocked Task Inspector: all projects");
    });

    it("builds title for scope 'project'", () => {
      const scope: TemplateScope = { type: "project", project: "brain-api" };
      const title = buildScheduledTaskTitle(template, scope);
      expect(title).toBe("Blocked Task Inspector: project brain-api");
    });

    it("builds title for scope 'feature'", () => {
      const scope: TemplateScope = {
        type: "feature",
        feature_id: "auth-system",
        project: "brain-api",
      };
      const title = buildScheduledTaskTitle(template, scope);
      expect(title).toBe("Blocked Task Inspector: feature auth-system");
    });
  });

  // ========================================
  // buildScheduledTaskTag
  // ========================================

  describe("buildScheduledTaskTag", () => {
    it("builds tag for scope 'all'", () => {
      const scope: TemplateScope = { type: "all" };
      const tag = buildScheduledTaskTag("blocked-inspector", scope);
      expect(tag).toBe("monitor:blocked-inspector:all");
    });

    it("builds tag for scope 'project'", () => {
      const scope: TemplateScope = { type: "project", project: "brain-api" };
      const tag = buildScheduledTaskTag("blocked-inspector", scope);
      expect(tag).toBe("monitor:blocked-inspector:project:brain-api");
    });

    it("builds tag for scope 'feature'", () => {
      const scope: TemplateScope = {
        type: "feature",
        feature_id: "auth-system",
        project: "brain-api",
      };
      const tag = buildScheduledTaskTag("blocked-inspector", scope);
      expect(tag).toBe("monitor:blocked-inspector:feature:auth-system");
    });

    it("produces deterministic tags", () => {
      const scope: TemplateScope = { type: "project", project: "my-proj" };
      const tag1 = buildScheduledTaskTag("blocked-inspector", scope);
      const tag2 = buildScheduledTaskTag("blocked-inspector", scope);
      expect(tag1).toBe(tag2);
    });
  });

  // ========================================
  // buildPrompt
  // ========================================

  describe("buildPrompt", () => {
    const template = TEMPLATES["blocked-inspector"];

    it("generates prompt mentioning 'all projects' for scope all", () => {
      const prompt = template.buildPrompt({ type: "all" });
      expect(prompt).toContain("all projects");
    });

    it("generates prompt mentioning project name for scope project", () => {
      const prompt = template.buildPrompt({
        type: "project",
        project: "brain-api",
      });
      expect(prompt).toContain("brain-api");
    });

    it("generates prompt mentioning feature_id for scope feature", () => {
      const prompt = template.buildPrompt({
        type: "feature",
        feature_id: "auth-system",
        project: "brain-api",
      });
      expect(prompt).toContain("auth-system");
      expect(prompt).toContain("brain-api");
    });
  });

  // ========================================
  // createScheduledTask
  // ========================================

  describe("createScheduledTask", () => {
    const apiBase = "http://localhost:3333";
    const template = TEMPLATES["blocked-inspector"];

    it("sends POST to /api/v1/entries with correct body", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      let capturedBody: string | undefined;

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        capturedBody = options?.body as string;
        return Promise.resolve(
          new Response(
            JSON.stringify({ id: "abc12345", path: "projects/brain-api/task/abc12345.md" }),
            { status: 201 }
          )
        );
      }) as typeof fetch;

      const scope: TemplateScope = { type: "all" };
      const result = await createScheduledTask(template, scope, apiBase);

      expect(capturedUrl).toBe(`${apiBase}/api/v1/entries`);
      expect(capturedMethod).toBe("POST");

      const body = JSON.parse(capturedBody!);
      expect(body.type).toBe("task");
      expect(body.title).toBe("Blocked Task Inspector: all projects");
      expect(body.schedule).toBe("*/15 * * * *");
      expect(body.schedule_enabled).toBe(true);
      expect(body.direct_prompt).toContain("all projects");
      expect(body.complete_on_idle).toBe(true);
      expect(body.execution_mode).toBe("current_branch");
      expect(body.tags).toContain("monitor:blocked-inspector:all");
      expect(body.tags).toContain("scheduled");

      expect(result).toEqual({
        id: "abc12345",
        path: "projects/brain-api/task/abc12345.md",
      });
    });

    it("includes project field for project scope", async () => {
      let capturedBody: string | undefined;

      globalThis.fetch = ((_url: string, options?: RequestInit) => {
        capturedBody = options?.body as string;
        return Promise.resolve(
          new Response(
            JSON.stringify({ id: "abc12345", path: "projects/brain-api/task/abc12345.md" }),
            { status: 201 }
          )
        );
      }) as typeof fetch;

      const scope: TemplateScope = { type: "project", project: "brain-api" };
      await createScheduledTask(template, scope, apiBase);

      const body = JSON.parse(capturedBody!);
      expect(body.project).toBe("brain-api");
      expect(body.feature_id).toBeUndefined();
    });

    it("includes project and feature_id for feature scope", async () => {
      let capturedBody: string | undefined;

      globalThis.fetch = ((_url: string, options?: RequestInit) => {
        capturedBody = options?.body as string;
        return Promise.resolve(
          new Response(
            JSON.stringify({ id: "abc12345", path: "projects/brain-api/task/abc12345.md" }),
            { status: 201 }
          )
        );
      }) as typeof fetch;

      const scope: TemplateScope = {
        type: "feature",
        feature_id: "auth-system",
        project: "brain-api",
      };
      await createScheduledTask(template, scope, apiBase);

      const body = JSON.parse(capturedBody!);
      expect(body.project).toBe("brain-api");
      expect(body.feature_id).toBe("auth-system");
    });

    it("throws on non-ok response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Server error", { status: 500 }))
      );

      const scope: TemplateScope = { type: "all" };
      await expect(
        createScheduledTask(template, scope, apiBase)
      ).rejects.toThrow("Failed to create scheduled task: 500");
    });
  });

  // ========================================
  // findScheduledTask
  // ========================================

  describe("findScheduledTask", () => {
    const apiBase = "http://localhost:3333";

    it("sends GET with correct query params", async () => {
      let capturedUrl: string | undefined;

      globalThis.fetch = ((url: string) => {
        capturedUrl = url;
        return Promise.resolve(
          new Response(
            JSON.stringify({
              entries: [
                {
                  id: "abc12345",
                  path: "projects/brain-api/task/abc12345.md",
                  schedule_enabled: true,
                },
              ],
            }),
            { status: 200 }
          )
        );
      }) as typeof fetch;

      const scope: TemplateScope = { type: "project", project: "brain-api" };
      await findScheduledTask("blocked-inspector", scope, apiBase);

      expect(capturedUrl).toContain("/api/v1/entries?");
      expect(capturedUrl).toContain("type=task");
      expect(capturedUrl).toContain(
        `tags=${encodeURIComponent("monitor:blocked-inspector:project:brain-api")}`
      );
      expect(capturedUrl).toContain("limit=1");
    });

    it("returns entry when found", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              entries: [
                {
                  id: "abc12345",
                  path: "projects/brain-api/task/abc12345.md",
                  schedule_enabled: true,
                },
              ],
            }),
            { status: 200 }
          )
        )
      );

      const scope: TemplateScope = { type: "all" };
      const result = await findScheduledTask("blocked-inspector", scope, apiBase);

      expect(result).toEqual({
        id: "abc12345",
        path: "projects/brain-api/task/abc12345.md",
        enabled: true,
      });
    });

    it("returns enabled=false when schedule_enabled is false", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              entries: [
                {
                  id: "abc12345",
                  path: "projects/brain-api/task/abc12345.md",
                  schedule_enabled: false,
                },
              ],
            }),
            { status: 200 }
          )
        )
      );

      const scope: TemplateScope = { type: "all" };
      const result = await findScheduledTask("blocked-inspector", scope, apiBase);

      expect(result).not.toBeNull();
      expect(result!.enabled).toBe(false);
    });

    it("returns null when no entries found", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ entries: [] }), { status: 200 })
        )
      );

      const scope: TemplateScope = { type: "all" };
      const result = await findScheduledTask("blocked-inspector", scope, apiBase);

      expect(result).toBeNull();
    });

    it("throws on non-ok response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Server error", { status: 500 }))
      );

      const scope: TemplateScope = { type: "all" };
      await expect(
        findScheduledTask("blocked-inspector", scope, apiBase)
      ).rejects.toThrow("Failed to search for scheduled task: 500");
    });
  });

  // ========================================
  // toggleScheduledTask
  // ========================================

  describe("toggleScheduledTask", () => {
    const apiBase = "http://localhost:3333";

    it("sends PATCH with schedule_enabled=true", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      let capturedBody: string | undefined;

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        capturedBody = options?.body as string;
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      await toggleScheduledTask(
        "projects/brain-api/task/abc12345.md",
        true,
        apiBase
      );

      expect(capturedMethod).toBe("PATCH");
      expect(capturedUrl).toContain(
        encodeURIComponent("projects/brain-api/task/abc12345.md")
      );
      expect(JSON.parse(capturedBody!)).toEqual({ schedule_enabled: true });
    });

    it("sends PATCH with schedule_enabled=false", async () => {
      let capturedBody: string | undefined;

      globalThis.fetch = ((_url: string, options?: RequestInit) => {
        capturedBody = options?.body as string;
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      await toggleScheduledTask(
        "projects/brain-api/task/abc12345.md",
        false,
        apiBase
      );

      expect(JSON.parse(capturedBody!)).toEqual({ schedule_enabled: false });
    });

    it("throws on non-ok response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Not found", { status: 404 }))
      );

      await expect(
        toggleScheduledTask("projects/brain-api/task/abc12345.md", true, apiBase)
      ).rejects.toThrow("Failed to toggle scheduled task: 404");
    });
  });
});
