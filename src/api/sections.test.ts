/**
 * Brain API - Section Endpoints Tests
 *
 * Tests for section listing and extraction endpoints.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { createApp } from "../server";
import { getConfig } from "../config";
import { parseSections, extractSection } from "./sections";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
const TEST_SUBDIR = `_sections-test-${Date.now()}`;
const TEST_PATH_PREFIX = `projects/${TEST_SUBDIR}`;

// Create full app with all routes
const app = createApp(config);

beforeAll(() => {
  // Create test subdirectories in the real brain dir
  const directories = ["plan", "scratch"];
  for (const dir of directories) {
    const testDir = join(config.brain.brainDir, TEST_PATH_PREFIX, dir);
    if (!existsSync(testDir)) {
      mkdirSync(testDir, { recursive: true });
    }
  }

  // Create test entry with multiple sections
  writeFileSync(
    join(config.brain.brainDir, `${TEST_PATH_PREFIX}/plan/multi-section.md`),
    `---
title: Multi Section Plan
type: plan
status: active
---

# Multi Section Plan

Introduction paragraph.

## Overview

This is the overview section.
It has multiple lines.

## Goals

- Goal 1
- Goal 2
- Goal 3

### Sub-goal A

Details about sub-goal A.

### Sub-goal B

Details about sub-goal B.

## Implementation

Implementation details here.

## Conclusion

Final thoughts.
`
  );

  // Create test entry with no sections
  writeFileSync(
    join(config.brain.brainDir, `${TEST_PATH_PREFIX}/scratch/no-sections.md`),
    `---
title: No Sections Entry
type: scratch
status: active
---

# No Sections Entry

This entry has no h2 or h3 headers.
Just plain content.
`
  );

  // Create test entry with special characters in section titles
  writeFileSync(
    join(config.brain.brainDir, `${TEST_PATH_PREFIX}/plan/special-chars.md`),
    `---
title: Special Characters Plan
type: plan
status: active
---

# Special Characters Plan

## Section with Spaces

Content here.

## Section: With Colon

Content here.

## Section (With Parens)

Content here.

## Section & Ampersand

Content here.
`
  );
});

afterAll(() => {
  // Clean up test directory
  const testDir = join(config.brain.brainDir, "projects", TEST_SUBDIR);
  if (existsSync(testDir)) {
    rmSync(testDir, { recursive: true, force: true });
  }
});

// =============================================================================
// Unit Tests for Section Parsing
// =============================================================================

describe("Section Parsing (Unit)", () => {
  describe("parseSections", () => {
    test("parses h2 and h3 headers", () => {
      const content = `# Title

## Section One

Content.

### Subsection

More content.

## Section Two

Final content.
`;
      const sections = parseSections(content);

      expect(sections).toHaveLength(3);
      expect(sections[0]).toEqual({ title: "Section One", level: 2, line: 3 });
      expect(sections[1]).toEqual({ title: "Subsection", level: 3, line: 7 });
      expect(sections[2]).toEqual({ title: "Section Two", level: 2, line: 11 });
    });

    test("returns empty array for content with no sections", () => {
      const content = `# Title

Just some content without h2 or h3 headers.
`;
      const sections = parseSections(content);

      expect(sections).toHaveLength(0);
    });

    test("ignores h1 and h4+ headers", () => {
      const content = `# H1 Title

## H2 Section

#### H4 Header

##### H5 Header
`;
      const sections = parseSections(content);

      expect(sections).toHaveLength(1);
      expect(sections[0].title).toBe("H2 Section");
    });

    test("handles headers with special characters", () => {
      const content = `## Section: With Colon

## Section (With Parens)

## Section & Ampersand
`;
      const sections = parseSections(content);

      expect(sections).toHaveLength(3);
      expect(sections[0].title).toBe("Section: With Colon");
      expect(sections[1].title).toBe("Section (With Parens)");
      expect(sections[2].title).toBe("Section & Ampersand");
    });
  });

  describe("extractSection", () => {
    const content = `# Title

## Overview

Overview content.

## Goals

- Goal 1
- Goal 2

### Sub-goal A

Sub-goal A details.

### Sub-goal B

Sub-goal B details.

## Implementation

Implementation content.
`;

    test("extracts section by title (case-insensitive)", () => {
      const section = extractSection(content, "overview");

      expect(section).not.toBeNull();
      expect(section!.title).toBe("overview");
      expect(section!.level).toBe(2);
      expect(section!.content).toContain("## Overview");
      expect(section!.content).toContain("Overview content.");
    });

    test("extracts section with subsections by default", () => {
      const section = extractSection(content, "Goals");

      expect(section).not.toBeNull();
      expect(section!.content).toContain("## Goals");
      expect(section!.content).toContain("### Sub-goal A");
      expect(section!.content).toContain("### Sub-goal B");
    });

    test("extracts section without subsections when includeSubsections=false", () => {
      const section = extractSection(content, "Goals", false);

      expect(section).not.toBeNull();
      expect(section!.content).toContain("## Goals");
      expect(section!.content).toContain("- Goal 1");
      expect(section!.content).not.toContain("### Sub-goal A");
    });

    test("returns null for non-existent section", () => {
      const section = extractSection(content, "Non-existent");

      expect(section).toBeNull();
    });

    test("extracts last section correctly", () => {
      const section = extractSection(content, "Implementation");

      expect(section).not.toBeNull();
      expect(section!.content).toContain("## Implementation");
      expect(section!.content).toContain("Implementation content.");
    });
  });
});

// =============================================================================
// API Integration Tests
// =============================================================================

describe("Section Endpoints (API)", () => {
  describe("GET /api/v1/entries/:path/sections", () => {
    test("lists sections from entry with multiple headers", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/multi-section.md/sections`
      );

      expect(res.status).toBe(200);
      const json = await res.json();

      expect(json.sections).toBeInstanceOf(Array);
      expect(json.total).toBeGreaterThan(0);

      // Check section structure
      const overview = json.sections.find((s: { title: string }) => s.title === "Overview");
      expect(overview).toBeDefined();
      expect(overview.level).toBe(2);
      expect(typeof overview.line).toBe("number");

      // Check subsections are included
      const subgoalA = json.sections.find((s: { title: string }) => s.title === "Sub-goal A");
      expect(subgoalA).toBeDefined();
      expect(subgoalA.level).toBe(3);
    });

    test("returns empty sections array for entry with no headers", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/scratch/no-sections.md/sections`
      );

      expect(res.status).toBe(200);
      const json = await res.json();

      expect(json.sections).toBeInstanceOf(Array);
      expect(json.sections).toHaveLength(0);
      expect(json.total).toBe(0);
    });

    test("returns 404 for non-existent entry", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/non-existent.md/sections`
      );

      expect(res.status).toBe(404);
      const json = await res.json();
      expect(json.error).toBe("Not Found");
    });
  });

  describe("GET /api/v1/entries/:path/sections/:title", () => {
    test("gets specific section by exact title", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/multi-section.md/sections/Overview`
      );

      expect(res.status).toBe(200);
      const json = await res.json();

      expect(json.title).toBe("Overview");
      expect(json.level).toBe(2);
      expect(json.content).toContain("## Overview");
      expect(json.content).toContain("This is the overview section.");
      expect(typeof json.line).toBe("number");
    });

    test("gets section with case-insensitive matching", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/multi-section.md/sections/overview`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.content).toContain("## Overview");
    });

    test("includes subsections by default", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/multi-section.md/sections/Goals`
      );

      expect(res.status).toBe(200);
      const json = await res.json();

      expect(json.content).toContain("## Goals");
      expect(json.content).toContain("### Sub-goal A");
      expect(json.content).toContain("### Sub-goal B");
    });

    test("excludes subsections when includeSubsections=false", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/multi-section.md/sections/Goals?includeSubsections=false`
      );

      expect(res.status).toBe(200);
      const json = await res.json();

      expect(json.content).toContain("## Goals");
      expect(json.content).toContain("- Goal 1");
      expect(json.content).not.toContain("### Sub-goal A");
    });

    test("returns 404 for non-existent section", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/multi-section.md/sections/NonExistent`
      );

      expect(res.status).toBe(404);
      const json = await res.json();
      expect(json.error).toBe("Not Found");
      expect(json.message).toContain("Section not found");
    });

    test("returns 404 for non-existent entry", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/non-existent.md/sections/Overview`
      );

      expect(res.status).toBe(404);
      const json = await res.json();
      expect(json.error).toBe("Not Found");
    });

    test("handles URL-encoded section titles with spaces", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/special-chars.md/sections/${encodeURIComponent("Section with Spaces")}`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.content).toContain("## Section with Spaces");
    });

    test("handles URL-encoded section titles with special characters", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/special-chars.md/sections/${encodeURIComponent("Section: With Colon")}`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.content).toContain("## Section: With Colon");
    });

    test("handles section titles with ampersand", async () => {
      const res = await app.request(
        `/api/v1/entries/${TEST_PATH_PREFIX}/plan/special-chars.md/sections/${encodeURIComponent("Section & Ampersand")}`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.content).toContain("## Section & Ampersand");
    });
  });
});
