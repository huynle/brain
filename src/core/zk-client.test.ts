/**
 * ZK Client - Unit Tests
 *
 * Tests for zk-client utility functions, particularly frontmatter generation.
 */

import { describe, test, expect } from "bun:test";
import {
  generateFrontmatter,
  parseFrontmatter,
  escapeYamlValue,
  unescapeYamlValue,
  extractIdFromPath,
  generateMarkdownLink,
  matchesFilenamePattern,
  formatYamlMultilineValue,
  normalizeTitle,
  sanitizeTitle,
  sanitizeTag,
  sanitizeSimpleValue,
  sanitizeDependsOnEntry,
  serializeFrontmatter,
} from "./zk-client";

// =============================================================================
// Tests for generateFrontmatter - Execution Context Fields
// =============================================================================

describe("generateFrontmatter()", () => {
  describe("basic functionality", () => {
    test("generates minimal frontmatter with title and type", () => {
      const fm = generateFrontmatter({
        title: "Test Entry",
        type: "task",
      });
      
      expect(fm).toContain("title: Test Entry");
      expect(fm).toContain("type: task");
      expect(fm).toContain("status: active"); // default status
    });

    test("includes custom status", () => {
      const fm = generateFrontmatter({
        title: "Test",
        type: "task",
        status: "pending",
      });
      
      expect(fm).toContain("status: pending");
    });

    test("includes tags", () => {
      const fm = generateFrontmatter({
        title: "Test",
        type: "task",
        tags: ["tag1", "tag2"],
      });
      
      expect(fm).toContain("tags:");
      expect(fm).toContain("  - tag1");
      expect(fm).toContain("  - tag2");
      expect(fm).toContain("  - task"); // type added to tags
    });

    test("includes priority", () => {
      const fm = generateFrontmatter({
        title: "Test",
        type: "task",
        priority: "high",
      });
      
      expect(fm).toContain("priority: high");
    });

    test("includes projectId", () => {
      const fm = generateFrontmatter({
        title: "Test",
        type: "task",
        projectId: "my-project",
      });
      
      expect(fm).toContain("projectId: my-project");
    });
  });

  describe("execution context fields for tasks", () => {
    test("includes workdir when provided", () => {
      const fm = generateFrontmatter({
        title: "Task with workdir",
        type: "task",
        workdir: "projects/my-project",
      });
      
      expect(fm).toContain("workdir: projects/my-project");
    });

    test("includes worktree when provided", () => {
      const fm = generateFrontmatter({
        title: "Task with worktree",
        type: "task",
        worktree: "projects/my-project-feature-branch",
      });
      
      expect(fm).toContain("worktree: projects/my-project-feature-branch");
    });

    test("includes git_remote when provided", () => {
      const fm = generateFrontmatter({
        title: "Task with git_remote",
        type: "task",
        git_remote: "git@github.com:user/repo.git",
      });
      
      // git_remote contains special characters (@ and :) so it gets quoted
      expect(fm).toContain('git_remote: "git@github.com:user/repo.git"');
    });

    test("includes git_branch when provided", () => {
      const fm = generateFrontmatter({
        title: "Task with git_branch",
        type: "task",
        git_branch: "feature/new-feature",
      });
      
      expect(fm).toContain("git_branch: feature/new-feature");
    });

    test("includes all execution context fields together", () => {
      const fm = generateFrontmatter({
        title: "Full task context",
        type: "task",
        status: "pending",
        priority: "high",
        workdir: "projects/my-project",
        worktree: "projects/my-project-wt",
        git_remote: "git@github.com:user/repo.git",
        git_branch: "main",
      });
      
      expect(fm).toContain("title: Full task context");
      expect(fm).toContain("type: task");
      expect(fm).toContain("status: pending");
      expect(fm).toContain("priority: high");
      expect(fm).toContain("workdir: projects/my-project");
      expect(fm).toContain("worktree: projects/my-project-wt");
      // git_remote contains special characters (@ and :) so it gets quoted
      expect(fm).toContain('git_remote: "git@github.com:user/repo.git"');
      expect(fm).toContain("git_branch: main");
    });

    test("escapes special characters in workdir path", () => {
      const fm = generateFrontmatter({
        title: "Test",
        type: "task",
        workdir: "projects/my-project: special",
      });
      
      // Should be quoted due to colon
      expect(fm).toContain('workdir: "projects/my-project: special"');
    });

    test("does NOT include execution context for non-task types", () => {
      // generateFrontmatter doesn't discriminate by type - it includes whatever is passed
      // This test documents the current behavior
      const fm = generateFrontmatter({
        title: "Not a task",
        type: "scratch",
        workdir: "projects/my-project",
      });
      
      // Current implementation includes workdir regardless of type
      // The caller (BrainService) is responsible for only passing these for tasks
      expect(fm).toContain("workdir: projects/my-project");
    });

    test("omits execution context fields when not provided", () => {
      const fm = generateFrontmatter({
        title: "Task without context",
        type: "task",
      });
      
      expect(fm).not.toContain("workdir:");
      expect(fm).not.toContain("worktree:");
      expect(fm).not.toContain("git_remote:");
      expect(fm).not.toContain("git_branch:");
    });
  });
});

// =============================================================================
// Tests for parseFrontmatter - Round-trip verification
// =============================================================================

describe("parseFrontmatter()", () => {
  test("parses basic frontmatter", () => {
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

  test("parses frontmatter with priority", () => {
    const content = `---
title: Test
type: task
priority: high
status: active
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.priority).toBe("high");
  });

  test("parses frontmatter with tags", () => {
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
});

// =============================================================================
// Tests for escapeYamlValue / unescapeYamlValue
// =============================================================================

describe("escapeYamlValue()", () => {
  test("returns plain string when no special characters", () => {
    expect(escapeYamlValue("simple")).toBe("simple");
    expect(escapeYamlValue("path/to/file")).toBe("path/to/file");
  });

  test("quotes strings with colons", () => {
    expect(escapeYamlValue("key: value")).toBe('"key: value"');
  });

  test("quotes strings with special YAML characters", () => {
    expect(escapeYamlValue("test#comment")).toBe('"test#comment"');
    expect(escapeYamlValue("value@domain")).toBe('"value@domain"');
  });

  test("escapes internal quotes", () => {
    expect(escapeYamlValue('say "hello"')).toBe('"say \\"hello\\""');
  });

  test("escapes newlines", () => {
    expect(escapeYamlValue("line1\nline2")).toBe('"line1\\nline2"');
  });

  test("escapes carriage returns", () => {
    expect(escapeYamlValue("line1\rline2")).toBe('"line1\\rline2"');
  });

  test("escapes tabs", () => {
    expect(escapeYamlValue("col1\tcol2")).toBe('"col1\\tcol2"');
  });
});

describe("unescapeYamlValue()", () => {
  test("removes surrounding double quotes", () => {
    expect(unescapeYamlValue('"quoted"')).toBe("quoted");
  });

  test("removes surrounding single quotes", () => {
    expect(unescapeYamlValue("'single'")).toBe("single");
  });

  test("unescapes internal escaped quotes", () => {
    expect(unescapeYamlValue('"say \\"hello\\""')).toBe('say "hello"');
  });

  test("handles backslash escapes", () => {
    expect(unescapeYamlValue('"path\\\\to\\\\file"')).toBe("path\\to\\file");
  });

  test("returns unquoted string unchanged", () => {
    expect(unescapeYamlValue("plain")).toBe("plain");
  });
});

// =============================================================================
// Tests for extractIdFromPath
// =============================================================================

describe("extractIdFromPath()", () => {
  test("extracts ID from simple filename", () => {
    expect(extractIdFromPath("abc12def.md")).toBe("abc12def");
  });

  test("extracts ID from path with directories", () => {
    expect(extractIdFromPath("projects/test/task/abc12def.md")).toBe("abc12def");
  });

  test("handles path without .md extension", () => {
    expect(extractIdFromPath("abc12def")).toBe("abc12def");
  });

  test("handles deeply nested path", () => {
    expect(extractIdFromPath("a/b/c/d/e/xyz99abc.md")).toBe("xyz99abc");
  });
});

// =============================================================================
// Tests for generateMarkdownLink
// =============================================================================

describe("generateMarkdownLink()", () => {
  test("generates link with title", () => {
    expect(generateMarkdownLink("abc12def", "My Title")).toBe("[My Title](abc12def)");
  });

  test("generates link without title uses ID", () => {
    expect(generateMarkdownLink("abc12def")).toBe("[abc12def](abc12def)");
  });
});

// =============================================================================
// Tests for matchesFilenamePattern
// =============================================================================

describe("matchesFilenamePattern()", () => {
  test("matches exact ID", () => {
    expect(matchesFilenamePattern("abc12def", "abc12def")).toBe(true);
    expect(matchesFilenamePattern("abc12def", "xyz99abc")).toBe(false);
  });

  test("matches prefix pattern", () => {
    expect(matchesFilenamePattern("abc12def", "abc*")).toBe(true);
    expect(matchesFilenamePattern("abc12def", "xyz*")).toBe(false);
  });

  test("matches suffix pattern", () => {
    expect(matchesFilenamePattern("abc12def", "*def")).toBe(true);
    expect(matchesFilenamePattern("abc12def", "*xyz")).toBe(false);
  });

  test("matches contains pattern", () => {
    expect(matchesFilenamePattern("abc12def", "*12*")).toBe(true);
    expect(matchesFilenamePattern("abc12def", "*99*")).toBe(false);
  });

  test("handles .md extension", () => {
    expect(matchesFilenamePattern("abc12def.md", "abc12def")).toBe(true);
    expect(matchesFilenamePattern("abc12def", "abc12def.md")).toBe(true);
  });
});

// =============================================================================
// Tests for formatYamlMultilineValue
// =============================================================================

describe("formatYamlMultilineValue()", () => {
  test("formats simple single-line value without quoting", () => {
    const result = formatYamlMultilineValue("key", "simple value");
    expect(result).toBe("key: simple value");
  });

  test("uses literal block scalar for multiline content", () => {
    const result = formatYamlMultilineValue("key", "line 1\nline 2\nline 3");
    expect(result).toBe("key: |\n  line 1\n  line 2\n  line 3");
  });

  test("uses literal block scalar for content with special YAML characters", () => {
    const result = formatYamlMultilineValue("key", "value: with colon");
    expect(result).toBe("key: |\n  value: with colon");
  });

  test("handles content with code blocks", () => {
    const content = `Here's some code:
\`\`\`typescript
function hello() {
  console.log("world");
}
\`\`\``;
    const result = formatYamlMultilineValue("user_original_request", content);
    expect(result).toContain("user_original_request: |");
    expect(result).toContain("  Here's some code:");
    expect(result).toContain("  ```typescript");
    expect(result).toContain('  console.log("world");');
  });

  test("handles content with special characters", () => {
    const content = "Use the @decorator and #tags, also: colons";
    const result = formatYamlMultilineValue("key", content);
    expect(result).toBe("key: |\n  Use the @decorator and #tags, also: colons");
  });

  test("preserves empty lines in multiline content", () => {
    const content = "line 1\n\nline 3";
    const result = formatYamlMultilineValue("key", content);
    expect(result).toBe("key: |\n  line 1\n  \n  line 3");
  });

  test("handles content with quotes", () => {
    const content = 'say "hello" and \'world\'';
    const result = formatYamlMultilineValue("key", content);
    expect(result).toBe("key: |\n  say \"hello\" and 'world'");
  });

  test("handles content with backslashes", () => {
    const content = "path\\to\\file";
    const result = formatYamlMultilineValue("key", content);
    // Backslashes alone don't require quoting in YAML
    expect(result).toBe("key: path\\to\\file");
  });

  test("handles content with backslashes and special chars", () => {
    const content = "path\\to\\file: with colon";
    const result = formatYamlMultilineValue("key", content);
    // Colon triggers literal block scalar
    expect(result).toBe("key: |\n  path\\to\\file: with colon");
  });
});

// =============================================================================
// Tests for user_original_request in generateFrontmatter
// =============================================================================

describe("generateFrontmatter() with user_original_request", () => {
  test("includes simple user_original_request", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
      user_original_request: "Add a button",
    });
    expect(fm).toContain("user_original_request: Add a button");
  });

  test("formats multiline user_original_request as literal block scalar", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
      user_original_request: "Add a button\nwith these requirements:\n- Blue color\n- Round corners",
    });
    expect(fm).toContain("user_original_request: |");
    expect(fm).toContain("  Add a button");
    expect(fm).toContain("  with these requirements:");
    expect(fm).toContain("  - Blue color");
  });

  test("handles user_original_request with code blocks", () => {
    const request = `Implement this function:
\`\`\`typescript
interface Config {
  timeout: number;
  retries: number;
}
\`\`\``;
    const fm = generateFrontmatter({
      title: "Implement Config",
      type: "task",
      user_original_request: request,
    });
    expect(fm).toContain("user_original_request: |");
    expect(fm).toContain("  Implement this function:");
    expect(fm).toContain("  ```typescript");
    expect(fm).toContain("  interface Config {");
  });

  test("omits user_original_request when not provided", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
    });
    expect(fm).not.toContain("user_original_request");
  });
});

// =============================================================================
// Tests for user_original_request in parseFrontmatter
// =============================================================================

// =============================================================================
// Tests for sanitization functions
// =============================================================================

describe("sanitizeTitle()", () => {
  test("strips newlines", () => {
    expect(sanitizeTitle("Fix:\nbug")).toBe("Fix: bug");
  });

  test("strips carriage returns", () => {
    expect(sanitizeTitle("Title\r\nhere")).toBe("Title here");
  });

  test("strips null bytes", () => {
    expect(sanitizeTitle("Title\0here")).toBe("Titlehere");
  });

  test("trims whitespace", () => {
    expect(sanitizeTitle("  spaced  ")).toBe("spaced");
  });

  test("collapses internal whitespace", () => {
    expect(sanitizeTitle("too   many   spaces")).toBe("too many spaces");
  });

  test("truncates to 200 chars", () => {
    const longTitle = "a".repeat(250);
    expect(sanitizeTitle(longTitle).length).toBe(200);
  });

  test("preserves YAML-special chars like colons", () => {
    expect(sanitizeTitle("Fix: Update API")).toBe("Fix: Update API");
  });

  test("handles tabs", () => {
    expect(sanitizeTitle("Title\twith\ttabs")).toBe("Title with tabs");
  });

  test("escapes double quotes (templates wrap title in quotes)", () => {
    expect(sanitizeTitle('Say "hello"')).toBe('Say \\"hello\\"');
  });

  test("escapes backslashes", () => {
    expect(sanitizeTitle("path\\to\\file")).toBe("path\\\\to\\\\file");
  });
});

describe("normalizeTitle()", () => {
  test("returns same value for simple title", () => {
    expect(normalizeTitle("Simple Title")).toBe("Simple Title");
  });

  test("replaces newlines with spaces", () => {
    expect(normalizeTitle("Line1\nLine2")).toBe("Line1 Line2");
  });

  test("does NOT escape quotes (unlike sanitizeTitle)", () => {
    expect(normalizeTitle('Say "hello"')).toBe('Say "hello"');
  });

  test("does NOT escape backslashes (unlike sanitizeTitle)", () => {
    expect(normalizeTitle("path\\to\\file")).toBe("path\\to\\file");
  });

  test("preserves colons", () => {
    expect(normalizeTitle("Fix: this bug")).toBe("Fix: this bug");
  });
});

describe("sanitizeTag()", () => {
  test("returns null for empty tag", () => {
    expect(sanitizeTag("")).toBeNull();
    expect(sanitizeTag("   ")).toBeNull();
  });

  test("returns null for tags with colons (YAML corruption)", () => {
    expect(sanitizeTag("bad: tag")).toBeNull();
    expect(sanitizeTag("key:value")).toBeNull();
  });

  test("strips newlines from tags", () => {
    expect(sanitizeTag("bad\ntag")).toBe("badtag");
  });

  test("trims whitespace", () => {
    expect(sanitizeTag("  tag  ")).toBe("tag");
  });

  test("strips carriage returns", () => {
    expect(sanitizeTag("tag\rwith\rcr")).toBe("tagwithcr");
  });

  test("strips null bytes", () => {
    expect(sanitizeTag("tag\0null")).toBe("tagnull");
  });

  test("strips tabs", () => {
    expect(sanitizeTag("tag\twith\ttab")).toBe("tagwithtab");
  });
});

describe("sanitizeSimpleValue()", () => {
  test("strips newlines", () => {
    expect(sanitizeSimpleValue("path/to\n/file")).toBe("path/to /file");
  });

  test("preserves colons (common in git remotes)", () => {
    expect(sanitizeSimpleValue("git@github.com:user/repo")).toBe("git@github.com:user/repo");
  });

  test("strips carriage returns", () => {
    expect(sanitizeSimpleValue("path\r\nto")).toBe("path to");
  });

  test("strips null bytes", () => {
    expect(sanitizeSimpleValue("path\0to")).toBe("pathto");
  });

  test("trims whitespace", () => {
    expect(sanitizeSimpleValue("  path  ")).toBe("path");
  });
});

describe("sanitizeDependsOnEntry()", () => {
  test("strips newlines", () => {
    expect(sanitizeDependsOnEntry("dep\nid")).toBe("depid");
  });

  test("handles quotes (escaping happens at format time)", () => {
    expect(sanitizeDependsOnEntry('abc"def')).toBe('abc"def');
  });

  test("strips carriage returns", () => {
    expect(sanitizeDependsOnEntry("dep\rid")).toBe("depid");
  });

  test("strips null bytes", () => {
    expect(sanitizeDependsOnEntry("dep\0id")).toBe("depid");
  });

  test("trims whitespace", () => {
    expect(sanitizeDependsOnEntry("  depid  ")).toBe("depid");
  });
});

// =============================================================================
// Tests for serializeFrontmatter
// =============================================================================

describe("serializeFrontmatter()", () => {
  test("serializes basic frontmatter", () => {
    const fm = {
      title: "Test Title",
      type: "task",
      status: "active",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("title: Test Title");
    expect(result).toContain("type: task");
    expect(result).toContain("status: active");
  });

  test("preserves all task fields", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "pending",
      created: "2024-01-01T00:00:00Z",
      priority: "high",
      parent_id: "abc12def",
      projectId: "my-project",
      depends_on: ["dep1", "dep2"],
      workdir: "projects/test",
      worktree: "projects/test-feature",
      git_remote: "git@github.com:user/repo",
      git_branch: "feature-branch",
      user_original_request: "Do the thing",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("title: Task");
    expect(result).toContain("created: 2024-01-01T00:00:00Z");
    expect(result).toContain("priority: high");
    expect(result).toContain("parent_id: abc12def");
    expect(result).toContain("depends_on:");
    expect(result).toContain('  - "dep1"');
    expect(result).toContain("workdir: projects/test");
    expect(result).toContain("git_remote:");
    expect(result).toContain("user_original_request: Do the thing");
  });

  test("escapes title with special chars", () => {
    const fm = {
      title: "Fix: the bug",
      type: "task",
      status: "active",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain('title: "Fix: the bug"');
  });

  test("escapes depends_on entries with quotes", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      depends_on: ['abc"def'],
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain('  - "abc\\"def"');
  });

  test("handles tags array", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      tags: ["feature", "urgent"],
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("tags:");
    expect(result).toContain("  - feature");
    expect(result).toContain("  - urgent");
  });

  test("escapes tags with special chars", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      tags: ["tag: with colon"],
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain('  - "tag: with colon"');
  });

  test("handles multiline user_original_request", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      user_original_request: "Line 1\nLine 2\nLine 3",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("user_original_request: |");
    expect(result).toContain("  Line 1");
    expect(result).toContain("  Line 2");
  });

  test("omits undefined fields", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
    };
    const result = serializeFrontmatter(fm);
    expect(result).not.toContain("workdir:");
    expect(result).not.toContain("worktree:");
    expect(result).not.toContain("git_remote:");
    expect(result).not.toContain("git_branch:");
    expect(result).not.toContain("depends_on:");
    expect(result).not.toContain("user_original_request:");
  });
});

describe("parseFrontmatter() with user_original_request", () => {
  test("parses simple single-line user_original_request", () => {
    const content = `---
title: Test Task
type: task
status: pending
user_original_request: Add a simple button
---

Task content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.user_original_request).toBe("Add a simple button");
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
  - 10px padding
---

Task content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.user_original_request).toBe(
      "Add a button with:\n- Blue color\n- Round corners\n- 10px padding"
    );
  });

  test("parses user_original_request with code blocks", () => {
    const content = `---
title: Implement Feature
type: task
status: pending
user_original_request: |
  Implement this function:
  \`\`\`typescript
  function greet(name: string): string {
    return \`Hello, \${name}!\`;
  }
  \`\`\`
---

Implementation notes`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.user_original_request).toContain("Implement this function:");
    expect(frontmatter.user_original_request).toContain("```typescript");
    expect(frontmatter.user_original_request).toContain("function greet(name: string)");
  });

  test("parses user_original_request with special YAML characters", () => {
    const content = `---
title: Test Task
type: task
status: pending
user_original_request: |
  Use the @decorator and #tags
  Also: handle colons properly
  And "quotes" and 'single quotes'
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.user_original_request).toContain("@decorator");
    expect(frontmatter.user_original_request).toContain("#tags");
    expect(frontmatter.user_original_request).toContain('Also: handle colons');
    expect(frontmatter.user_original_request).toContain('"quotes"');
  });

  test("handles missing user_original_request gracefully", () => {
    const content = `---
title: Test Task
type: task
status: pending
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.user_original_request).toBeUndefined();
  });

  test("round-trip: generateFrontmatter -> parseFrontmatter preserves multiline content", () => {
    const originalRequest = `Add a feature with these specs:
1. Must support dark mode
2. Code example:
\`\`\`js
const theme = getTheme();
\`\`\`
3. Special chars: @#$%^&*()`;

    const fm = generateFrontmatter({
      title: "Feature Task",
      type: "task",
      status: "pending",
      user_original_request: originalRequest,
    });

    const fullContent = `---\n${fm}---\n\nTask body`;
    const { frontmatter } = parseFrontmatter(fullContent);
    
    expect(frontmatter.user_original_request).toBe(originalRequest);
  });

  test("round-trip preserves complex request with all edge cases", () => {
    const complexRequest = `User request with everything:
- Bullet points
- Colons: like this
- Quotes: "double" and 'single'
- Code:
  \`\`\`python
  def hello():
      print("world")  # comment
  \`\`\`
- Special: @mentions #hashtags
- Emoji: not recommended but 🎉
- Backslash: path\\to\\file
- URL: https://example.com?foo=bar&baz=qux`;

    const fm = generateFrontmatter({
      title: "Complex Task",
      type: "task",
      user_original_request: complexRequest,
    });

    const fullContent = `---\n${fm}---\n\nBody`;
    const { frontmatter } = parseFrontmatter(fullContent);
    
    expect(frontmatter.user_original_request).toBe(complexRequest);
  });
});
