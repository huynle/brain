/**
 * Tests for Monitor Templates
 *
 * Tests template registry, title/tag builders, parseMonitorTag round-trip,
 * and prompt generation.
 */

import { describe, it, expect } from "bun:test";
import {
  MONITOR_TEMPLATES,
  MONITOR_TEMPLATE_LIST,
  buildMonitorTitle,
  buildMonitorTag,
  parseMonitorTag,
  type MonitorScope,
} from "./monitor-templates";

// =============================================================================
// Template Registry
// =============================================================================

describe("MONITOR_TEMPLATES registry", () => {
  it("has blocked-inspector template", () => {
    expect(MONITOR_TEMPLATES["blocked-inspector"]).toBeDefined();
  });

  it("blocked-inspector has all required fields", () => {
    const t = MONITOR_TEMPLATES["blocked-inspector"];
    expect(t.id).toBe("blocked-inspector");
    expect(t.label).toBe("Blocked Task Inspector");
    expect(typeof t.description).toBe("string");
    expect(t.description.length).toBeGreaterThan(0);
    expect(t.defaultSchedule).toBe("*/15 * * * *");
    expect(typeof t.buildPrompt).toBe("function");
    expect(Array.isArray(t.tags)).toBe(true);
    expect(t.tags.length).toBeGreaterThan(0);
  });
});

describe("MONITOR_TEMPLATE_LIST", () => {
  it("is an array derived from MONITOR_TEMPLATES", () => {
    expect(Array.isArray(MONITOR_TEMPLATE_LIST)).toBe(true);
    expect(MONITOR_TEMPLATE_LIST.length).toBe(
      Object.keys(MONITOR_TEMPLATES).length,
    );
  });

  it("contains the blocked-inspector template", () => {
    const found = MONITOR_TEMPLATE_LIST.find(
      (t) => t.id === "blocked-inspector",
    );
    expect(found).toBeDefined();
    expect(found).toBe(MONITOR_TEMPLATES["blocked-inspector"]);
  });
});

// =============================================================================
// buildMonitorTitle
// =============================================================================

describe("buildMonitorTitle", () => {
  const template = MONITOR_TEMPLATES["blocked-inspector"];

  it("builds title for scope 'all'", () => {
    const scope: MonitorScope = { type: "all" };
    const title = buildMonitorTitle(template, scope);
    expect(title).toBe("Monitor: Blocked Task Inspector (all projects)");
  });

  it("builds title for scope 'project'", () => {
    const scope: MonitorScope = { type: "project", project: "brain-api" };
    const title = buildMonitorTitle(template, scope);
    expect(title).toBe("Monitor: Blocked Task Inspector (project brain-api)");
  });

  it("builds title for scope 'feature'", () => {
    const scope: MonitorScope = {
      type: "feature",
      feature_id: "auth-system",
      project: "brain-api",
    };
    const title = buildMonitorTitle(template, scope);
    expect(title).toBe(
      "Monitor: Blocked Task Inspector (feature auth-system)",
    );
  });
});

// =============================================================================
// buildMonitorTag
// =============================================================================

describe("buildMonitorTag", () => {
  it("builds tag for scope 'all'", () => {
    const scope: MonitorScope = { type: "all" };
    const tag = buildMonitorTag("blocked-inspector", scope);
    expect(tag).toBe("monitor:blocked-inspector:all");
  });

  it("builds tag for scope 'project'", () => {
    const scope: MonitorScope = { type: "project", project: "brain-api" };
    const tag = buildMonitorTag("blocked-inspector", scope);
    expect(tag).toBe("monitor:blocked-inspector:project:brain-api");
  });

  it("builds tag for scope 'feature'", () => {
    const scope: MonitorScope = {
      type: "feature",
      feature_id: "auth-system",
      project: "brain-api",
    };
    const tag = buildMonitorTag("blocked-inspector", scope);
    expect(tag).toBe(
      "monitor:blocked-inspector:feature:auth-system:brain-api",
    );
  });

  it("produces deterministic tags", () => {
    const scope: MonitorScope = { type: "project", project: "my-proj" };
    const tag1 = buildMonitorTag("blocked-inspector", scope);
    const tag2 = buildMonitorTag("blocked-inspector", scope);
    expect(tag1).toBe(tag2);
  });
});

// =============================================================================
// parseMonitorTag
// =============================================================================

describe("parseMonitorTag", () => {
  it("parses 'all' scope tag", () => {
    const result = parseMonitorTag("monitor:blocked-inspector:all");
    expect(result).toEqual({
      templateId: "blocked-inspector",
      scope: { type: "all" },
    });
  });

  it("parses 'project' scope tag", () => {
    const result = parseMonitorTag(
      "monitor:blocked-inspector:project:brain-api",
    );
    expect(result).toEqual({
      templateId: "blocked-inspector",
      scope: { type: "project", project: "brain-api" },
    });
  });

  it("parses 'feature' scope tag", () => {
    const result = parseMonitorTag(
      "monitor:blocked-inspector:feature:auth-system:brain-api",
    );
    expect(result).toEqual({
      templateId: "blocked-inspector",
      scope: {
        type: "feature",
        feature_id: "auth-system",
        project: "brain-api",
      },
    });
  });

  it("returns null for non-monitor tags", () => {
    expect(parseMonitorTag("scheduled")).toBeNull();
    expect(parseMonitorTag("inspector")).toBeNull();
    expect(parseMonitorTag("")).toBeNull();
  });

  it("returns null for malformed monitor tags", () => {
    expect(parseMonitorTag("monitor:")).toBeNull();
    expect(parseMonitorTag("monitor:template")).toBeNull();
  });

  it("round-trips with buildMonitorTag for all scope types", () => {
    const scopes: MonitorScope[] = [
      { type: "all" },
      { type: "project", project: "brain-api" },
      {
        type: "feature",
        feature_id: "auth-system",
        project: "brain-api",
      },
    ];

    for (const scope of scopes) {
      const tag = buildMonitorTag("blocked-inspector", scope);
      const parsed = parseMonitorTag(tag);
      expect(parsed).not.toBeNull();
      expect(parsed!.templateId).toBe("blocked-inspector");
      expect(parsed!.scope).toEqual(scope);
    }
  });
});

// =============================================================================
// buildPrompt
// =============================================================================

describe("buildPrompt", () => {
  const template = MONITOR_TEMPLATES["blocked-inspector"];

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

  it("returns non-empty string for all scope types", () => {
    const scopes: MonitorScope[] = [
      { type: "all" },
      { type: "project", project: "test-proj" },
      { type: "feature", feature_id: "feat-1", project: "test-proj" },
    ];
    for (const scope of scopes) {
      const prompt = template.buildPrompt(scope);
      expect(typeof prompt).toBe("string");
      expect(prompt.length).toBeGreaterThan(100);
    }
  });

  it("includes safety rules", () => {
    const prompt = template.buildPrompt({ type: "all" });
    expect(prompt).toContain("NEVER change the status of `draft` tasks");
    expect(prompt).toContain("NEVER inspect or modify your own task");
    expect(prompt).toContain("NEVER force-unblock agent self-blocks");
    expect(prompt).toContain("Limit actions per run to 5");
    expect(prompt).toContain("Be conservative");
  });

  it("references correct brain tools", () => {
    const prompt = template.buildPrompt({ type: "all" });
    expect(prompt).toContain("brain_tasks");
    expect(prompt).toContain("brain_task_get");
    expect(prompt).toContain("brain_recall");
    expect(prompt).toContain("brain_search");
  });
});
