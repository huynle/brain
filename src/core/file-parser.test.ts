/**
 * Tests for file-parser.ts
 *
 * Covers: parseFile, extractLinks, computeChecksum
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { parseFile, extractLinks, computeChecksum } from "./file-parser";
import type { ParsedFile, ExtractedLink } from "./file-parser";

// =============================================================================
// Test Helpers
// =============================================================================

let tempDir: string;

beforeEach(() => {
  tempDir = mkdtempSync(join(tmpdir(), "file-parser-test-"));
});

afterEach(() => {
  rmSync(tempDir, { recursive: true, force: true });
});

/**
 * Helper: write a markdown file in the temp dir and return its relative path.
 */
function writeTestFile(relativePath: string, content: string): string {
  const fullPath = join(tempDir, relativePath);
  // Ensure parent dirs exist
  const dir = fullPath.substring(0, fullPath.lastIndexOf("/"));
  if (dir) {
    const { mkdirSync } = require("node:fs");
    mkdirSync(dir, { recursive: true });
  }
  writeFileSync(fullPath, content, "utf-8");
  return relativePath;
}

// =============================================================================
// computeChecksum
// =============================================================================

describe("computeChecksum()", () => {
  test("returns SHA-256 hex string", () => {
    const result = computeChecksum("hello world");
    // SHA-256 of "hello world" is a known value
    expect(result).toBe(
      "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
    );
  });

  test("is deterministic for same content", () => {
    const content = "some test content\nwith newlines\n";
    expect(computeChecksum(content)).toBe(computeChecksum(content));
  });

  test("differs for different content", () => {
    expect(computeChecksum("aaa")).not.toBe(computeChecksum("bbb"));
  });

  test("handles empty string", () => {
    const result = computeChecksum("");
    // SHA-256 of empty string is a known value
    expect(result).toBe(
      "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    );
    expect(result).toHaveLength(64); // SHA-256 hex is 64 chars
  });
});

// =============================================================================
// extractLinks
// =============================================================================

describe("extractLinks()", () => {
  test("extracts standard markdown links", () => {
    const md = "Check out [my note](abc12def) for details.";
    const links = extractLinks(md);
    expect(links).toHaveLength(1);
    expect(links[0].href).toBe("abc12def");
    expect(links[0].title).toBe("my note");
    expect(links[0].type).toBe("markdown");
  });

  test("classifies http/https links as url type", () => {
    const md = "Visit [Google](https://google.com) and [HTTP](http://example.com).";
    const links = extractLinks(md);
    expect(links).toHaveLength(2);
    expect(links[0].type).toBe("url");
    expect(links[0].href).toBe("https://google.com");
    expect(links[1].type).toBe("url");
    expect(links[1].href).toBe("http://example.com");
  });

  test("excludes image links ![alt](src)", () => {
    const md = "Here is ![an image](image.png) and [a link](abc12def).";
    const links = extractLinks(md);
    expect(links).toHaveLength(1);
    expect(links[0].href).toBe("abc12def");
    expect(links[0].title).toBe("a link");
  });

  test("extracts multiple links", () => {
    const md = "See [note A](aaa11111) and [note B](bbb22222) and [note C](ccc33333).";
    const links = extractLinks(md);
    expect(links).toHaveLength(3);
    expect(links[0].href).toBe("aaa11111");
    expect(links[1].href).toBe("bbb22222");
    expect(links[2].href).toBe("ccc33333");
  });

  test("returns empty array for text with no links", () => {
    const md = "Just some plain text without any links.";
    const links = extractLinks(md);
    expect(links).toHaveLength(0);
  });

  test("handles empty string", () => {
    expect(extractLinks("")).toHaveLength(0);
  });

  test("handles link with empty title", () => {
    const md = "Click [](abc12def) here.";
    const links = extractLinks(md);
    expect(links).toHaveLength(1);
    expect(links[0].title).toBe("");
    expect(links[0].href).toBe("abc12def");
  });

  test("provides snippet context ±50 chars", () => {
    // Build a string where the link is at a known position
    const prefix = "A".repeat(60);
    const suffix = "B".repeat(60);
    const md = `${prefix}[my link](target123)${suffix}`;
    const links = extractLinks(md);
    expect(links).toHaveLength(1);

    const snippet = links[0].snippet;
    // Snippet should start 50 chars before the match
    expect(snippet.startsWith("A".repeat(50))).toBe(true);
    // Snippet should contain the link itself
    expect(snippet).toContain("[my link](target123)");
    // Snippet should end 50 chars after the match
    expect(snippet.endsWith("B".repeat(50))).toBe(true);
  });

  test("snippet handles link at start of text", () => {
    const md = "[start link](abc12def) followed by text.";
    const links = extractLinks(md);
    expect(links).toHaveLength(1);
    // Snippet should start from beginning (no negative index)
    expect(links[0].snippet.startsWith("[start link]")).toBe(true);
  });

  test("snippet handles link at end of text", () => {
    const md = "Some text then [end link](xyz99abc)";
    const links = extractLinks(md);
    expect(links).toHaveLength(1);
    expect(links[0].snippet.endsWith("(xyz99abc)")).toBe(true);
  });

  test("handles links with paths as href", () => {
    const md = "See [related](projects/test/task/abc12def.md) for more.";
    const links = extractLinks(md);
    expect(links).toHaveLength(1);
    expect(links[0].href).toBe("projects/test/task/abc12def.md");
    expect(links[0].type).toBe("markdown");
  });
});

// =============================================================================
// parseFile
// =============================================================================

describe("parseFile()", () => {
  test("parses file with valid frontmatter and body", () => {
    const content = `---
title: My Test Note
type: summary
status: active
tags:
  - test
  - example
priority: high
projectId: my-project
feature_id: feat-123
created: 2026-01-15T10:00:00.000Z
---

This is the first paragraph of the note.

This is the second paragraph with a [link](abc12def).
`;
    const path = writeTestFile("projects/test/summary/xyz99abc.md", content);
    const result = parseFile(path, tempDir);

    expect(result.path).toBe(path);
    expect(result.shortId).toBe("xyz99abc");
    expect(result.title).toBe("My Test Note");
    expect(result.type).toBe("summary");
    expect(result.status).toBe("active");
    expect(result.priority).toBe("high");
    expect(result.projectId).toBe("my-project");
    expect(result.featureId).toBe("feat-123");
    expect(result.tags).toEqual(["test", "example"]);
    expect(result.created).toBe("2026-01-15T10:00:00.000Z");
    expect(result.modified).toBeTruthy(); // file mtime
    expect(result.rawContent).toBe(content);
    expect(result.body).toContain("This is the first paragraph");
    expect(result.checksum).toHaveLength(64); // SHA-256 hex
    expect(result.links).toHaveLength(1);
    expect(result.links[0].href).toBe("abc12def");
  });

  test("extracts short ID from filename", () => {
    const content = "---\ntitle: Test\n---\nBody text.";
    const path = writeTestFile("abc12def.md", content);
    const result = parseFile(path, tempDir);
    expect(result.shortId).toBe("abc12def");
  });

  test("extracts short ID from nested path", () => {
    const content = "---\ntitle: Nested\n---\nBody.";
    const path = writeTestFile("a/b/c/xyz99abc.md", content);
    const result = parseFile(path, tempDir);
    expect(result.shortId).toBe("xyz99abc");
  });

  test("uses shortId as title when frontmatter has no title", () => {
    const content = "---\ntype: scratch\n---\nSome body.";
    const path = writeTestFile("notitle1.md", content);
    const result = parseFile(path, tempDir);
    expect(result.title).toBe("notitle1");
  });

  test("handles file with no frontmatter", () => {
    const content = "Just plain markdown without frontmatter.\n\nSecond paragraph.";
    const path = writeTestFile("nofm1234.md", content);
    const result = parseFile(path, tempDir);

    expect(result.shortId).toBe("nofm1234");
    expect(result.title).toBe("nofm1234"); // falls back to shortId
    expect(result.body).toBe(content); // entire content is body
    expect(result.metadata).toEqual({});
    expect(result.tags).toEqual([]);
    expect(result.type).toBeUndefined();
    expect(result.status).toBeUndefined();
  });

  test("handles empty file", () => {
    const content = "";
    const path = writeTestFile("empty123.md", content);
    const result = parseFile(path, tempDir);

    expect(result.shortId).toBe("empty123");
    expect(result.title).toBe("empty123");
    expect(result.body).toBe("");
    expect(result.rawContent).toBe("");
    expect(result.wordCount).toBe(0);
    expect(result.lead).toBe("");
    expect(result.links).toHaveLength(0);
    expect(result.checksum).toHaveLength(64);
  });

  test("handles malformed YAML frontmatter", () => {
    // parseFrontmatter returns {} for malformed YAML, body = full content
    const content = "---\ntitle: [invalid yaml\n---\nBody text here.";
    const path = writeTestFile("badfm123.md", content);
    const result = parseFile(path, tempDir);

    // parseFrontmatter will still parse what it can or return partial results
    // The important thing is it doesn't throw
    expect(result.shortId).toBe("badfm123");
    expect(result.rawContent).toBe(content);
  });

  test("computes correct word count", () => {
    const content = "---\ntitle: Word Count Test\n---\none two three four five";
    const path = writeTestFile("wc123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.wordCount).toBe(5);
  });

  test("word count handles multiple whitespace types", () => {
    const content = "---\ntitle: WC\n---\nword1  word2\tword3\nword4";
    const path = writeTestFile("wc234567.md", content);
    const result = parseFile(path, tempDir);
    expect(result.wordCount).toBe(4);
  });

  test("computes deterministic checksum", () => {
    const content = "---\ntitle: Checksum\n---\nSame content.";
    const path = writeTestFile("ck123456.md", content);
    const result1 = parseFile(path, tempDir);
    const result2 = parseFile(path, tempDir);
    expect(result1.checksum).toBe(result2.checksum);
    expect(result1.checksum).toBe(computeChecksum(content));
  });

  test("extracts links from body", () => {
    const content = `---
title: Links Test
---

See [note A](aaa11111) and [note B](bbb22222).

Also check [Google](https://google.com).

![image](should-be-excluded.png)
`;
    const path = writeTestFile("lk123456.md", content);
    const result = parseFile(path, tempDir);

    expect(result.links).toHaveLength(3);
    expect(result.links[0].href).toBe("aaa11111");
    expect(result.links[0].type).toBe("markdown");
    expect(result.links[1].href).toBe("bbb22222");
    expect(result.links[1].type).toBe("markdown");
    expect(result.links[2].href).toBe("https://google.com");
    expect(result.links[2].type).toBe("url");
  });

  test("uses file ctime when frontmatter has no created field", () => {
    const content = "---\ntitle: No Created\n---\nBody.";
    const path = writeTestFile("nc123456.md", content);
    const result = parseFile(path, tempDir);

    // created should be an ISO string (from file ctime)
    expect(result.created).toMatch(/^\d{4}-\d{2}-\d{2}T/);
    // modified should also be an ISO string
    expect(result.modified).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });

  test("uses frontmatter created when available", () => {
    const content = "---\ntitle: Has Created\ncreated: 2025-06-15T12:00:00.000Z\n---\nBody.";
    const path = writeTestFile("hc123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.created).toBe("2025-06-15T12:00:00.000Z");
  });

  test("metadata contains all frontmatter fields", () => {
    const content = `---
title: Metadata Test
type: task
status: pending
priority: medium
projectId: proj-1
feature_id: feat-1
---

Body.
`;
    const path = writeTestFile("md123456.md", content);
    const result = parseFile(path, tempDir);

    expect(result.metadata.title).toBe("Metadata Test");
    expect(result.metadata.type).toBe("task");
    expect(result.metadata.status).toBe("pending");
    expect(result.metadata.priority).toBe("medium");
    expect(result.metadata.projectId).toBe("proj-1");
    expect(result.metadata.feature_id).toBe("feat-1");
  });
});

// =============================================================================
// Lead Extraction
// =============================================================================

describe("lead extraction", () => {
  test("extracts first paragraph as lead", () => {
    const content = `---
title: Lead Test
---

First paragraph here.

Second paragraph should not appear.
`;
    const path = writeTestFile("ld123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.lead).toBe("First paragraph here.");
  });

  test("strips markdown formatting from lead", () => {
    const content = `---
title: Formatted Lead
---

This has **bold** and *italic* and \`code\` and [a link](target).
`;
    const path = writeTestFile("fl123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.lead).toBe("This has bold and italic and code and a link.");
  });

  test("strips heading markers from lead", () => {
    const content = "---\ntitle: Heading Lead\n---\n\n## This is a heading\n\nParagraph.";
    const path = writeTestFile("hl123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.lead).toBe("This is a heading");
  });

  test("truncates lead to 200 characters", () => {
    const longParagraph = "A".repeat(250);
    const content = `---\ntitle: Long Lead\n---\n\n${longParagraph}\n\nSecond paragraph.`;
    const path = writeTestFile("ll123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.lead).toHaveLength(200);
    expect(result.lead).toBe("A".repeat(200));
  });

  test("returns empty lead for empty body", () => {
    const content = "---\ntitle: Empty Body\n---\n";
    const path = writeTestFile("eb123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.lead).toBe("");
  });

  test("skips blank lines to find first paragraph", () => {
    const content = "---\ntitle: Blank Lines\n---\n\n\n\nActual first paragraph.\n\nSecond.";
    const path = writeTestFile("bl123456.md", content);
    const result = parseFile(path, tempDir);
    expect(result.lead).toBe("Actual first paragraph.");
  });
});
