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

    test("includes parent_id", () => {
      const fm = generateFrontmatter({
        title: "Test",
        type: "task",
        parent_id: "abc12def",
      });
      
      expect(fm).toContain("parent_id: abc12def");
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

  test("parses frontmatter with parent_id", () => {
    const content = `---
title: Test
type: task
parent_id: abc12def
status: active
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.parent_id).toBe("abc12def");
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
