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

    test("includes generated metadata fields when provided", () => {
      const fm = generateFrontmatter({
        title: "Generated task",
        type: "task",
        generated: true,
        generated_kind: "gap_task",
        generated_key: "feature-checkout:missing-tests",
        generated_by: "feature-checkout",
      } as any);

      expect(fm).toContain("generated: true");
      expect(fm).toContain("generated_kind: gap_task");
      expect(fm).toContain("generated_key: feature-checkout:missing-tests");
      expect(fm).toContain("generated_by: feature-checkout");
    });

    test("preserves explicit generated false in generated frontmatter", () => {
      const fm = generateFrontmatter({
        title: "Manual task",
        type: "task",
        generated: false,
      } as any);

      expect(fm).toContain("generated: false");
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

    test("includes merge intent fields when provided", () => {
      const fm = generateFrontmatter({
        title: "Task with merge intent",
        type: "task",
        merge_target_branch: "main",
        merge_policy: "auto_merge",
        merge_strategy: "squash",
        open_pr_before_merge: true,
        execution_mode: "worktree",
      });

      expect(fm).toContain("merge_target_branch: main");
      expect(fm).toContain("merge_policy: auto_merge");
      expect(fm).toContain("merge_strategy: squash");
      expect(fm).toContain("open_pr_before_merge: true");
      expect(fm).toContain("execution_mode: worktree");
    });

    test("includes all execution context fields together", () => {
      const fm = generateFrontmatter({
        title: "Full task context",
        type: "task",
        status: "pending",
        priority: "high",
        workdir: "projects/my-project",
        git_remote: "git@github.com:user/repo.git",
        git_branch: "main",
      });
      
      expect(fm).toContain("title: Full task context");
      expect(fm).toContain("type: task");
      expect(fm).toContain("status: pending");
      expect(fm).toContain("priority: high");
      expect(fm).toContain("workdir: projects/my-project");
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

  describe("depends_on field", () => {
    test("includes depends_on when provided", () => {
      const fm = generateFrontmatter({
        title: "Task with deps",
        type: "task",
        depends_on: ["abc12def", "xyz99876"],
      });
      
      expect(fm).toContain("depends_on:");
      expect(fm).toContain('  - "abc12def"');
      expect(fm).toContain('  - "xyz99876"');
    });

    test("omits depends_on when empty array", () => {
      const fm = generateFrontmatter({
        title: "Task without deps",
        type: "task",
        depends_on: [],
      });
      
      expect(fm).not.toContain("depends_on:");
    });

    test("omits depends_on when not provided", () => {
      const fm = generateFrontmatter({
        title: "Task without deps",
        type: "task",
      });
      
      expect(fm).not.toContain("depends_on:");
    });

    test("escapes special characters in depends_on entries", () => {
      const fm = generateFrontmatter({
        title: "Task",
        type: "task",
        depends_on: ['abc"def', "xyz\\123"],
      });
      
      expect(fm).toContain('  - "abc\\"def"');
      expect(fm).toContain('  - "xyz\\\\123"');
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

  test("parses new sessions map format", () => {
    const content = `---
title: Test Entry
type: task
status: pending
sessions:
  ses_new111aaa:
    timestamp: "2026-02-22T10:30:00.000Z"
    cron_id: "cron_123"
    run_id: "run_456"
  ses_new222bbb:
    timestamp: "2026-02-22T10:31:00.000Z"
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.sessions).toEqual({
      ses_new111aaa: {
        timestamp: "2026-02-22T10:30:00.000Z",
        cron_id: "cron_123",
        run_id: "run_456",
      },
      ses_new222bbb: {
        timestamp: "2026-02-22T10:31:00.000Z",
      },
    });
  });

  test("normalizes legacy session_ids and session_timestamps into sessions map", () => {
    const content = `---
title: Legacy Sessions
type: task
status: pending
session_ids:
  - ses_old111aaa
  - ses_old222bbb
session_timestamps:
  ses_old111aaa: "2026-02-22T11:00:00.000Z"
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.sessions).toEqual({
      ses_old111aaa: { timestamp: "2026-02-22T11:00:00.000Z" },
      ses_old222bbb: { timestamp: "" },
    });
    expect(frontmatter.session_ids).toBeUndefined();
    expect(frontmatter.session_timestamps).toBeUndefined();
  });

  test("prefers new sessions format when mixed with legacy fields", () => {
    const content = `---
title: Mixed Sessions
type: task
status: pending
sessions:
  ses_new999zzz:
    timestamp: "2026-02-22T12:00:00.000Z"
session_ids:
  - ses_old999zzz
session_timestamps:
  ses_old999zzz: "1999-01-01T00:00:00.000Z"
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.sessions).toEqual({
      ses_new999zzz: { timestamp: "2026-02-22T12:00:00.000Z" },
    });
    expect((frontmatter.sessions as Record<string, { timestamp: string }>).ses_old999zzz).toBeUndefined();
  });

  test("parses schedule frontmatter fields on tasks", () => {
    const content = `---
title: Scheduled Task
type: task
status: pending
schedule: "0 */2 * * *"
next_run: "2026-03-01T10:00:00.000Z"
runs:
  - run_id: "run_001"
    status: completed
    started: "2026-03-01T08:00:00.000Z"
    completed: "2026-03-01T08:02:00.000Z"
    duration: "120s"
    tasks: "3"
    failed_task: ""
    skip_reason: ""
  - run_id: "run_002"
    status: skipped
    started: "2026-03-01T09:00:00.000Z"
    completed: "2026-03-01T09:00:10.000Z"
    duration: "10s"
    tasks: "0"
    failed_task: ""
    skip_reason: "dependency pending"
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.schedule).toBe("0 */2 * * *");
    expect(frontmatter.next_run).toBe("2026-03-01T10:00:00.000Z");
    expect(frontmatter.runs).toEqual([
      {
        run_id: "run_001",
        status: "completed",
        started: "2026-03-01T08:00:00.000Z",
        completed: "2026-03-01T08:02:00.000Z",
        duration: "120s",
        tasks: "3",
        failed_task: "",
        skip_reason: "",
      },
      {
        run_id: "run_002",
        status: "skipped",
        started: "2026-03-01T09:00:00.000Z",
        completed: "2026-03-01T09:00:10.000Z",
        duration: "10s",
        tasks: "0",
        failed_task: "",
        skip_reason: "dependency pending",
      },
    ]);
  });

  test("parses run_finalizations metadata map", () => {
    const content = `---
title: Finalized Task
type: task
status: completed
run_finalizations:
  run_20260225_001:
    status: completed
    finalized_at: "2026-02-25T10:05:00.000Z"
    session_id: ses_abc123
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.run_finalizations).toEqual({
      run_20260225_001: {
        status: "completed",
        finalized_at: "2026-02-25T10:05:00.000Z",
        session_id: "ses_abc123",
      },
    });
  });

  test("keeps backward compatibility when schedule fields are missing", () => {
    const content = `---
title: Legacy Task
type: task
status: active
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.schedule).toBeUndefined();
    expect(frontmatter.next_run).toBeUndefined();
    expect(frontmatter.runs).toBeUndefined();
  });

  test("parses generated metadata fields", () => {
    const content = `---
title: Generated Task
type: task
status: pending
generated: true
generated_kind: gap_task
generated_key: feature-checkout:missing-tests
generated_by: feature-checkout
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.generated).toBe(true);
    expect(frontmatter.generated_kind).toBe("gap_task");
    expect(frontmatter.generated_key).toBe("feature-checkout:missing-tests");
    expect(frontmatter.generated_by).toBe("feature-checkout");
  });

  test("parses merge intent metadata fields", () => {
    const content = `---
title: Merge Intent Task
type: task
status: pending
merge_target_branch: main
merge_policy: auto_merge
merge_strategy: squash
open_pr_before_merge: false
execution_mode: current_branch
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.merge_target_branch).toBe("main");
    expect(frontmatter.merge_policy).toBe("auto_merge");
    expect(frontmatter.merge_strategy).toBe("squash");
    expect(frontmatter.open_pr_before_merge).toBe(false);
    expect(frontmatter.execution_mode).toBe("current_branch");
  });

  test("parses explicit generated false", () => {
    const content = `---
title: Manual Task
type: task
status: pending
generated: false
---

Body`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.generated).toBe(false);
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

  test("returns null for tags with colon+space (YAML key-value corruption)", () => {
    expect(sanitizeTag("bad: tag")).toBeNull();
    expect(sanitizeTag("key: value")).toBeNull();
  });

  test("allows bare colons without trailing space (safe in YAML list items)", () => {
    expect(sanitizeTag("key:value")).toBe("key:value");
    expect(sanitizeTag("monitor:feature-review:feature:my-feat:my-proj")).toBe("monitor:feature-review:feature:my-feat:my-proj");
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
      merge_target_branch: "main",
      merge_policy: "auto_merge",
      merge_strategy: "squash",
      open_pr_before_merge: false,
      execution_mode: "worktree",
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
    expect(result).toContain("merge_target_branch: main");
    expect(result).toContain("merge_policy: auto_merge");
    expect(result).toContain("merge_strategy: squash");
    expect(result).toContain("open_pr_before_merge: false");
    expect(result).toContain("execution_mode: worktree");
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

  test("serializes only unified sessions map format", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      sessions: {
        ses_new111aaa: {
          timestamp: "2026-02-22T13:00:00.000Z",
          cron_id: "cron_789",
          run_id: "run_987",
        },
        ses_new222bbb: {
          timestamp: "2026-02-22T13:01:00.000Z",
        },
      },
      session_ids: ["ses_old111aaa"],
      session_timestamps: { ses_old111aaa: "1999-01-01T00:00:00.000Z" },
    };

    const result = serializeFrontmatter(fm);
    expect(result).toContain("sessions:");
    expect(result).toContain("  ses_new111aaa:");
    expect(result).toContain('    timestamp: "2026-02-22T13:00:00.000Z"');
    expect(result).toContain('    cron_id: cron_789');
    expect(result).toContain('    run_id: run_987');
    expect(result).toContain("  ses_new222bbb:");
    expect(result).toContain('    timestamp: "2026-02-22T13:01:00.000Z"');
    expect(result).not.toContain("session_ids:");
    expect(result).not.toContain("session_timestamps:");
  });

  test("serializes schedule frontmatter fields on tasks", () => {
    const fm = {
      title: "Scheduled Task",
      type: "task",
      status: "pending",
      schedule: "0 */2 * * *",
      next_run: "2026-03-01T10:00:00.000Z",
      runs: [
        {
          run_id: "run_001",
          status: "completed",
          started: "2026-03-01T08:00:00.000Z",
          completed: "2026-03-01T08:02:00.000Z",
          duration: "120s",
          tasks: "3",
          failed_task: "",
          skip_reason: "",
        },
      ],
    };

    const result = serializeFrontmatter(fm);
    expect(result).toContain('schedule: "0 */2 * * *"');
    expect(result).toContain('next_run: "2026-03-01T10:00:00.000Z"');
    expect(result).not.toContain("cron_ids:");
    expect(result).toContain("runs:");
    expect(result).toContain('  - run_id: run_001');
    expect(result).toContain('    status: completed');
    expect(result).toContain('    started: "2026-03-01T08:00:00.000Z"');
    expect(result).toContain('    completed: "2026-03-01T08:02:00.000Z"');
    expect(result).toContain('    duration: 120s');
    expect(result).toContain('    tasks: 3');
    expect(result).toContain('    failed_task: ""');
    expect(result).toContain('    skip_reason: ""');
  });

  test("serializes run_finalizations metadata map", () => {
    const fm = {
      title: "Finalized Task",
      type: "task",
      status: "completed",
      run_finalizations: {
        run_20260225_001: {
          status: "completed",
          finalized_at: "2026-02-25T10:05:00.000Z",
          session_id: "ses_abc123",
        },
      },
    };

    const result = serializeFrontmatter(fm);
    expect(result).toContain("run_finalizations:");
    expect(result).toContain("  run_20260225_001:");
    expect(result).toContain("    status: completed");
    expect(result).toContain('    finalized_at: "2026-02-25T10:05:00.000Z"');
    expect(result).toContain("    session_id: ses_abc123");
  });

  test("serializes generated metadata fields", () => {
    const fm = {
      title: "Generated Task",
      type: "task",
      status: "pending",
      generated: true,
      generated_kind: "feature_checkout",
      generated_key: "feature-checkout:task-1",
      generated_by: "feature-checkout",
    };

    const result = serializeFrontmatter(fm);
    expect(result).toContain("generated: true");
    expect(result).toContain("generated_kind: feature_checkout");
    expect(result).toContain('generated_key: "feature-checkout:task-1"');
    expect(result).toContain("generated_by: feature-checkout");
  });

  test("serializes explicit generated false", () => {
    const fm = {
      title: "Manual Task",
      type: "task",
      status: "pending",
      generated: false,
    };

    const result = serializeFrontmatter(fm);
    expect(result).toContain("generated: false");
  });
});

describe("generateFrontmatter() with sessions", () => {
  test("generates unified sessions map and omits legacy fields", () => {
    const fm = generateFrontmatter({
      title: "Task",
      type: "task",
      sessions: {
        ses_new111aaa: {
          timestamp: "2026-02-22T14:00:00.000Z",
          cron_id: "cron_abc",
        },
      },
    });

    expect(fm).toContain("sessions:");
    expect(fm).toContain("  ses_new111aaa:");
    expect(fm).toContain('    timestamp: "2026-02-22T14:00:00.000Z"');
    expect(fm).toContain("    cron_id: cron_abc");
    expect(fm).not.toContain("session_ids:");
    expect(fm).not.toContain("session_timestamps:");
  });
});

describe("schedule frontmatter round-trip", () => {
  test("generateFrontmatter and parseFrontmatter round-trip schedule fields", () => {
    const generated = generateFrontmatter({
      title: "Schedule Round Trip",
      type: "task",
      status: "pending",
      schedule: "*/15 * * * *",
      next_run: "2026-03-01T11:15:00.000Z",
      runs: [
        {
          run_id: "run_100",
          status: "completed",
          started: "2026-03-01T11:00:00.000Z",
          completed: "2026-03-01T11:01:30.000Z",
          duration: "90s",
          tasks: "2",
          failed_task: "",
          skip_reason: "",
        },
      ],
    } as any);

    const content = `---\n${generated}---\n\nBody`;
    const { frontmatter } = parseFrontmatter(content);

    expect(frontmatter.schedule).toBe("*/15 * * * *");
    expect(frontmatter.next_run).toBe("2026-03-01T11:15:00.000Z");
    expect(frontmatter.runs).toEqual([
      {
        run_id: "run_100",
        status: "completed",
        started: "2026-03-01T11:00:00.000Z",
        completed: "2026-03-01T11:01:30.000Z",
        duration: "90s",
        tasks: "2",
        failed_task: "",
        skip_reason: "",
      },
    ]);
  });

  test("serialize then parse preserves schedule fields", () => {
    const original = {
      title: "Schedule Serialize Round Trip",
      type: "task",
      status: "active",
      schedule: "0 0 * * *",
      next_run: "2026-03-02T00:00:00.000Z",
      runs: [
        {
          run_id: "run_nightly_1",
          status: "failed",
          started: "2026-03-01T00:00:00.000Z",
          completed: "2026-03-01T00:00:30.000Z",
          duration: "30s",
          tasks: "4",
          failed_task: "abc12def",
          skip_reason: "",
        },
      ],
    };

    const serialized = serializeFrontmatter(original);
    const content = `---\n${serialized}---\n\nBody`;
    const { frontmatter } = parseFrontmatter(content);

    expect(frontmatter.schedule).toBe(original.schedule);
    expect(frontmatter.next_run).toBe(original.next_run);
    expect(frontmatter.runs).toEqual(original.runs);
  });
});

describe("schedule_enabled frontmatter round-trip", () => {
  test("serializeFrontmatter emits schedule_enabled: false", () => {
    const fm = {
      title: "Disabled Schedule",
      type: "task",
      status: "completed",
      schedule: "*/5 * * * *",
      schedule_enabled: false,
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("schedule_enabled: false");
  });

  test("serializeFrontmatter emits schedule_enabled: true", () => {
    const fm = {
      title: "Enabled Schedule",
      type: "task",
      status: "active",
      schedule: "*/5 * * * *",
      schedule_enabled: true,
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("schedule_enabled: true");
  });

  test("serializeFrontmatter omits schedule_enabled when undefined", () => {
    const fm = {
      title: "No Flag",
      type: "task",
      status: "active",
      schedule: "*/5 * * * *",
    };
    const result = serializeFrontmatter(fm);
    expect(result).not.toContain("schedule_enabled");
  });

  test("parseFrontmatter reads schedule_enabled: false as boolean false", () => {
    const content = `---\ntitle: Test\ntype: task\nstatus: completed\nschedule: "*/5 * * * *"\nschedule_enabled: false\n---\n\nBody`;
    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.schedule_enabled).toBe(false);
  });

  test("parseFrontmatter reads schedule_enabled: true as boolean true", () => {
    const content = `---\ntitle: Test\ntype: task\nstatus: active\nschedule: "*/5 * * * *"\nschedule_enabled: true\n---\n\nBody`;
    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.schedule_enabled).toBe(true);
  });

  test("parseFrontmatter leaves schedule_enabled undefined when absent", () => {
    const content = `---\ntitle: Test\ntype: task\nstatus: active\nschedule: "*/5 * * * *"\n---\n\nBody`;
    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.schedule_enabled).toBeUndefined();
  });

  test("round-trip: serialize then parse preserves schedule_enabled: false", () => {
    const original = {
      title: "Round Trip False",
      type: "task",
      status: "completed",
      schedule: "0 * * * *",
      schedule_enabled: false,
    };
    const serialized = serializeFrontmatter(original);
    const content = `---\n${serialized}---\n\nBody`;
    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.schedule_enabled).toBe(false);
  });
});

// =============================================================================
// Tests for direct_prompt, agent, model in generateFrontmatter
// =============================================================================

describe("generateFrontmatter() with OpenCode execution options", () => {
  test("includes simple direct_prompt", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
      direct_prompt: "Run the tests and fix failures",
    });
    expect(fm).toContain("direct_prompt: Run the tests and fix failures");
  });

  test("formats multiline direct_prompt as literal block scalar", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
      direct_prompt: "Step 1: Read the code\nStep 2: Fix the bug\nStep 3: Run tests",
    });
    expect(fm).toContain("direct_prompt: |");
    expect(fm).toContain("  Step 1: Read the code");
    expect(fm).toContain("  Step 2: Fix the bug");
    expect(fm).toContain("  Step 3: Run tests");
  });

  test("formats direct_prompt with special YAML characters using block scalar", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
      direct_prompt: 'Use the @decorator and handle "quotes" properly',
    });
    expect(fm).toContain("direct_prompt: |");
    expect(fm).toContain('  Use the @decorator and handle "quotes" properly');
  });

  test("includes agent when provided", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
      agent: "explore",
    });
    expect(fm).toContain("agent: explore");
  });

  test("includes model when provided", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
      model: "anthropic/claude-sonnet-4-20250514",
    });
    // model contains '/' which doesn't need quoting
    expect(fm).toContain("model: anthropic/claude-sonnet-4-20250514");
  });

  test("includes all three OpenCode execution options together", () => {
    const fm = generateFrontmatter({
      title: "Full Task",
      type: "task",
      direct_prompt: "Do the work",
      agent: "tdd-dev",
      model: "anthropic/claude-sonnet-4-20250514",
    });
    expect(fm).toContain("direct_prompt: Do the work");
    expect(fm).toContain("agent: tdd-dev");
    expect(fm).toContain("model: anthropic/claude-sonnet-4-20250514");
  });

  test("omits OpenCode execution options when not provided", () => {
    const fm = generateFrontmatter({
      title: "Test Task",
      type: "task",
    });
    expect(fm).not.toContain("direct_prompt:");
    expect(fm).not.toContain("agent:");
    expect(fm).not.toContain("model:");
  });
});

// =============================================================================
// Tests for direct_prompt, agent, model in serializeFrontmatter
// =============================================================================

describe("serializeFrontmatter() with OpenCode execution options", () => {
  test("serializes simple direct_prompt", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      direct_prompt: "Run the tests",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("direct_prompt: Run the tests");
  });

  test("serializes multiline direct_prompt as literal block scalar", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      direct_prompt: "Line 1\nLine 2\nLine 3",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("direct_prompt: |");
    expect(result).toContain("  Line 1");
    expect(result).toContain("  Line 2");
  });

  test("serializes agent", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      agent: "explore",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("agent: explore");
  });

  test("serializes model", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
      model: "anthropic/claude-sonnet-4-20250514",
    };
    const result = serializeFrontmatter(fm);
    expect(result).toContain("model: anthropic/claude-sonnet-4-20250514");
  });

  test("omits OpenCode execution options when not present", () => {
    const fm = {
      title: "Task",
      type: "task",
      status: "active",
    };
    const result = serializeFrontmatter(fm);
    expect(result).not.toContain("direct_prompt:");
    expect(result).not.toContain("agent:");
    expect(result).not.toContain("model:");
  });
});

// =============================================================================
// Tests for direct_prompt, agent, model in parseFrontmatter
// =============================================================================

describe("parseFrontmatter() with OpenCode execution options", () => {
  test("parses simple direct_prompt", () => {
    const content = `---
title: Test Task
type: task
status: pending
direct_prompt: Run the tests
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.direct_prompt).toBe("Run the tests");
  });

  test("parses multiline direct_prompt (literal block scalar)", () => {
    const content = `---
title: Test Task
type: task
status: pending
direct_prompt: |
  Step 1: Read the code
  Step 2: Fix the bug
  Step 3: Run tests
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.direct_prompt).toBe(
      "Step 1: Read the code\nStep 2: Fix the bug\nStep 3: Run tests"
    );
  });

  test("parses agent field", () => {
    const content = `---
title: Test Task
type: task
status: pending
agent: explore
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.agent).toBe("explore");
  });

  test("parses model field", () => {
    const content = `---
title: Test Task
type: task
status: pending
model: anthropic/claude-sonnet-4-20250514
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.model).toBe("anthropic/claude-sonnet-4-20250514");
  });

  test("parses all three OpenCode execution options together", () => {
    const content = `---
title: Full Task
type: task
status: pending
direct_prompt: Do the work
agent: tdd-dev
model: anthropic/claude-sonnet-4-20250514
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.direct_prompt).toBe("Do the work");
    expect(frontmatter.agent).toBe("tdd-dev");
    expect(frontmatter.model).toBe("anthropic/claude-sonnet-4-20250514");
  });

  test("handles missing OpenCode execution options gracefully", () => {
    const content = `---
title: Test Task
type: task
status: pending
---

Content`;

    const { frontmatter } = parseFrontmatter(content);
    expect(frontmatter.direct_prompt).toBeUndefined();
    expect(frontmatter.agent).toBeUndefined();
    expect(frontmatter.model).toBeUndefined();
  });

  test("round-trip: generateFrontmatter -> parseFrontmatter preserves all OpenCode options", () => {
    const fm = generateFrontmatter({
      title: "Round Trip Task",
      type: "task",
      status: "pending",
      direct_prompt: "Step 1: Read\nStep 2: Fix\nSpecial: @#$%",
      agent: "tdd-dev",
      model: "anthropic/claude-sonnet-4-20250514",
    });

    const fullContent = `---\n${fm}---\n\nTask body`;
    const { frontmatter } = parseFrontmatter(fullContent);

    expect(frontmatter.direct_prompt).toBe("Step 1: Read\nStep 2: Fix\nSpecial: @#$%");
    expect(frontmatter.agent).toBe("tdd-dev");
    expect(frontmatter.model).toBe("anthropic/claude-sonnet-4-20250514");
  });

  test("round-trip preserves direct_prompt with code blocks", () => {
    const prompt = `Fix this function:
\`\`\`typescript
function add(a: number, b: number): number {
  return a - b; // bug: should be +
}
\`\`\`
Then run the tests.`;

    const fm = generateFrontmatter({
      title: "Fix Bug",
      type: "task",
      direct_prompt: prompt,
    });

    const fullContent = `---\n${fm}---\n\nBody`;
    const { frontmatter } = parseFrontmatter(fullContent);

    expect(frontmatter.direct_prompt).toBe(prompt);
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
