/**
 * Frontmatter Module - Unit Tests
 *
 * Tests for frontmatter functions extracted from zk-client.ts.
 * These verify the module exports work correctly and functions
 * maintain identical behavior after extraction.
 */

import { describe, test, expect } from "bun:test";
import {
  parseFrontmatter,
  serializeFrontmatter,
  generateFrontmatter,
  escapeYamlValue,
  unescapeYamlValue,
  formatYamlMultilineValue,
  normalizeTitle,
  sanitizeTitle,
  sanitizeTag,
  sanitizeSimpleValue,
  sanitizeDependsOnEntry,
} from "./frontmatter";
import type { GenerateFrontmatterOptions } from "./frontmatter";

// =============================================================================
// Module Export Verification
// =============================================================================

describe("frontmatter module exports", () => {
  test("all expected functions are exported", () => {
    expect(typeof parseFrontmatter).toBe("function");
    expect(typeof serializeFrontmatter).toBe("function");
    expect(typeof generateFrontmatter).toBe("function");
    expect(typeof escapeYamlValue).toBe("function");
    expect(typeof unescapeYamlValue).toBe("function");
    expect(typeof formatYamlMultilineValue).toBe("function");
    expect(typeof normalizeTitle).toBe("function");
    expect(typeof sanitizeTitle).toBe("function");
    expect(typeof sanitizeTag).toBe("function");
    expect(typeof sanitizeSimpleValue).toBe("function");
    expect(typeof sanitizeDependsOnEntry).toBe("function");
  });
});

// =============================================================================
// parseFrontmatter - Core behavior verification
// =============================================================================

describe("parseFrontmatter()", () => {
  test("parses basic frontmatter with title, type, status", () => {
    const content = `---
title: Test Entry
type: task
status: pending
---

Body content here.`;

    const { frontmatter, body } = parseFrontmatter(content);
    expect(frontmatter.title).toBe("Test Entry");
    expect(frontmatter.type).toBe("task");
    expect(frontmatter.status).toBe("pending");
    expect(body).toBe("Body content here.");
  });

  test("returns empty frontmatter for content without frontmatter", () => {
    const { frontmatter, body } = parseFrontmatter("Just plain text");
    expect(frontmatter).toEqual({});
    expect(body).toBe("Just plain text");
  });

  test("parses tags array", () => {
    const content = `---
title: Test
type: task
tags:
  - tag1
  - tag2
status: active
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.tags).toEqual(["tag1", "tag2"]);
  });

  test("parses quoted title with special characters", () => {
    const content = `---
title: "Test: With Colon"
type: task
status: active
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.title).toBe("Test: With Colon");
  });

  test("parses depends_on array", () => {
    const content = `---
title: Task
type: task
status: active
depends_on:
  - "abc12def"
  - "xyz99876"
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.depends_on).toEqual(["abc12def", "xyz99876"]);
  });

  test("parses multiline user_original_request (literal block scalar)", () => {
    const content = `---
title: Test Task
type: task
status: pending
user_original_request: |
  Add a button with:
  - Blue color
  - Round corners
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.user_original_request).toBe(
      "Add a button with:\n- Blue color\n- Round corners"
    );
  });
});

// =============================================================================
// escapeYamlValue / unescapeYamlValue
// =============================================================================

describe("escapeYamlValue()", () => {
  test("returns plain string when no special characters", () => {
    expect(escapeYamlValue("simple")).toBe("simple");
  });

  test("quotes strings with colons", () => {
    expect(escapeYamlValue("key: value")).toBe('"key: value"');
  });

  test("escapes internal quotes", () => {
    expect(escapeYamlValue('say "hello"')).toBe('"say \\"hello\\""');
  });

  test("escapes newlines", () => {
    expect(escapeYamlValue("line1\nline2")).toBe('"line1\\nline2"');
  });
});

describe("unescapeYamlValue()", () => {
  test("removes surrounding double quotes", () => {
    expect(unescapeYamlValue('"quoted"')).toBe("quoted");
  });

  test("unescapes internal escaped quotes", () => {
    expect(unescapeYamlValue('"say \\"hello\\""')).toBe('say "hello"');
  });

  test("handles backslash escapes", () => {
    expect(unescapeYamlValue('"path\\\\to\\\\file"')).toBe("path\\to\\file");
  });
});

// =============================================================================
// formatYamlMultilineValue
// =============================================================================

describe("formatYamlMultilineValue()", () => {
  test("formats simple single-line value without quoting", () => {
    expect(formatYamlMultilineValue("key", "simple value")).toBe("key: simple value");
  });

  test("uses literal block scalar for multiline content", () => {
    expect(formatYamlMultilineValue("key", "line 1\nline 2")).toBe(
      "key: |\n  line 1\n  line 2"
    );
  });

  test("uses literal block scalar for content with special YAML characters", () => {
    expect(formatYamlMultilineValue("key", "value: with colon")).toBe(
      "key: |\n  value: with colon"
    );
  });
});

// =============================================================================
// Sanitization helpers
// =============================================================================

describe("sanitizeTitle()", () => {
  test("strips newlines", () => {
    expect(sanitizeTitle("Fix:\nbug")).toBe("Fix: bug");
  });

  test("escapes double quotes", () => {
    expect(sanitizeTitle('Say "hello"')).toBe('Say \\"hello\\"');
  });

  test("escapes backslashes", () => {
    expect(sanitizeTitle("path\\to\\file")).toBe("path\\\\to\\\\file");
  });

  test("truncates to 200 chars", () => {
    expect(sanitizeTitle("a".repeat(250)).length).toBe(200);
  });
});

describe("normalizeTitle()", () => {
  test("replaces newlines with spaces", () => {
    expect(normalizeTitle("Line1\nLine2")).toBe("Line1 Line2");
  });

  test("does NOT escape quotes", () => {
    expect(normalizeTitle('Say "hello"')).toBe('Say "hello"');
  });
});

describe("sanitizeTag()", () => {
  test("returns null for empty tag", () => {
    expect(sanitizeTag("")).toBeNull();
  });

  test("returns null for tags with colon+space", () => {
    expect(sanitizeTag("bad: tag")).toBeNull();
  });

  test("allows bare colons", () => {
    expect(sanitizeTag("key:value")).toBe("key:value");
  });
});

describe("sanitizeSimpleValue()", () => {
  test("strips newlines", () => {
    expect(sanitizeSimpleValue("path/to\n/file")).toBe("path/to /file");
  });

  test("preserves colons", () => {
    expect(sanitizeSimpleValue("git@github.com:user/repo")).toBe("git@github.com:user/repo");
  });
});

describe("sanitizeDependsOnEntry()", () => {
  test("strips newlines", () => {
    expect(sanitizeDependsOnEntry("dep\nid")).toBe("depid");
  });

  test("trims whitespace", () => {
    expect(sanitizeDependsOnEntry("  depid  ")).toBe("depid");
  });
});

// =============================================================================
// generateFrontmatter
// =============================================================================

describe("generateFrontmatter()", () => {
  test("generates minimal frontmatter", () => {
    const fm = generateFrontmatter({ title: "Test", type: "task" });
    expect(fm).toContain("title: Test");
    expect(fm).toContain("type: task");
    expect(fm).toContain("status: active");
  });

  test("includes depends_on", () => {
    const fm = generateFrontmatter({
      title: "Task",
      type: "task",
      depends_on: ["abc12def"],
    });
    expect(fm).toContain("depends_on:");
    expect(fm).toContain('  - "abc12def"');
  });
});

// =============================================================================
// serializeFrontmatter
// =============================================================================

describe("serializeFrontmatter()", () => {
  test("serializes basic frontmatter", () => {
    const result = serializeFrontmatter({
      title: "Test",
      type: "task",
      status: "active",
    });
    expect(result).toContain("title: Test");
    expect(result).toContain("type: task");
    expect(result).toContain("status: active");
  });

  test("escapes title with special chars", () => {
    const result = serializeFrontmatter({
      title: "Fix: the bug",
      type: "task",
      status: "active",
    });
    expect(result).toContain('title: "Fix: the bug"');
  });
});

// =============================================================================
// Round-trip verification
// =============================================================================

describe("round-trip: generate -> parse", () => {
  test("preserves all fields through generate then parse", () => {
    const generated = generateFrontmatter({
      title: "Round Trip",
      type: "task",
      status: "pending",
      tags: ["feature"],
      priority: "high",
      depends_on: ["dep1"],
      user_original_request: "Do the thing\nwith details",
    });

    const content = `---\n${generated}---\n\nBody`;
    const { frontmatter, body } = parseFrontmatter(content);

    expect(frontmatter.title).toBe("Round Trip");
    expect(frontmatter.type).toBe("task");
    expect(frontmatter.status).toBe("pending");
    expect(frontmatter.priority).toBe("high");
    expect(frontmatter.depends_on).toEqual(["dep1"]);
    expect(frontmatter.user_original_request).toBe("Do the thing\nwith details");
    expect(body).toBe("Body");
  });

  test("serialize then parse preserves fields", () => {
    const original = {
      title: "Serialize Round Trip",
      type: "task",
      status: "active",
      tags: ["tag1"],
      depends_on: ["abc"],
    };

    const serialized = serializeFrontmatter(original);
    const content = `---\n${serialized}---\n\nBody`;
    const { frontmatter } = parseFrontmatter(content);

    expect(frontmatter.title).toBe(original.title);
    expect(frontmatter.type).toBe(original.type);
    expect(frontmatter.status).toBe(original.status);
    expect(frontmatter.tags).toEqual(original.tags);
    expect(frontmatter.depends_on).toEqual(original.depends_on);
  });
});
