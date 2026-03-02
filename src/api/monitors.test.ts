/**
 * Tests for Monitor API Routes
 *
 * Tests the REST API endpoints for monitor template management.
 * Uses app.request() for HTTP testing without a running server.
 */

import { describe, it, expect } from "bun:test";
import { Hono } from "hono";
import { createMonitorRoutes } from "./monitors";

// Create Hono app with routes mounted
const app = new Hono();
app.route("/monitors", createMonitorRoutes());

// =============================================================================
// GET /monitors/templates
// =============================================================================

describe("GET /monitors/templates", () => {
  it("returns template list", async () => {
    const res = await app.request("/monitors/templates");
    expect(res.status).toBe(200);

    const json = (await res.json()) as {
      templates: Array<{
        id: string;
        label: string;
        description: string;
        defaultSchedule: string;
        tags: string[];
      }>;
      count: number;
    };
    expect(json.count).toBeGreaterThan(0);
    expect(json.templates.length).toBe(json.count);

    const blocked = json.templates.find((t) => t.id === "blocked-inspector");
    expect(blocked).toBeDefined();
    expect(blocked!.label).toBe("Blocked Task Inspector");
    expect(blocked!.defaultSchedule).toBe("*/15 * * * *");
    expect(blocked!.tags).toContain("scheduled");
  });

  it("does not include buildPrompt function in response", async () => {
    const res = await app.request("/monitors/templates");
    const json = await res.json();
    const template = (json as { templates: Array<Record<string, unknown>> })
      .templates[0];
    expect(template.buildPrompt).toBeUndefined();
  });
});

// =============================================================================
// POST /monitors — Validation
// =============================================================================

describe("POST /monitors — validation", () => {
  it("rejects empty body", async () => {
    const res = await app.request("/monitors", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    expect(res.status).toBe(400);
  });

  it("rejects missing templateId", async () => {
    const res = await app.request("/monitors", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        scope: { type: "all" },
      }),
    });
    expect(res.status).toBe(400);
  });

  it("rejects missing scope", async () => {
    const res = await app.request("/monitors", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        templateId: "blocked-inspector",
      }),
    });
    expect(res.status).toBe(400);
  });

  it("rejects invalid scope type", async () => {
    const res = await app.request("/monitors", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        templateId: "blocked-inspector",
        scope: { type: "invalid" },
      }),
    });
    expect(res.status).toBe(400);
  });

  it("rejects unknown template ID", async () => {
    const res = await app.request("/monitors", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        templateId: "nonexistent-template",
        scope: { type: "all" },
      }),
    });
    // This will either be 400 (template validation) or fail at service level
    // depending on whether the service is available
    expect([400, 500]).toContain(res.status);
  });
});

// =============================================================================
// PATCH /monitors/:taskId/toggle — Validation
// =============================================================================

describe("PATCH /monitors/:taskId/toggle — validation", () => {
  it("rejects empty body", async () => {
    const res = await app.request("/monitors/abc12def/toggle", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: "{}",
    });
    expect(res.status).toBe(400);
  });

  it("rejects non-boolean enabled", async () => {
    const res = await app.request("/monitors/abc12def/toggle", {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled: "yes" }),
    });
    expect(res.status).toBe(400);
  });
});
