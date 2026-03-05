/**
 * Tests for macOS notification utility
 *
 * Tests notify functions with mocked Bun.spawn — does NOT actually call osascript.
 */

import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import {
  notify,
  notifyBlocked,
  notifyFailed,
  notifyCompleted,
  notifyWithSound,
  escapeForAppleScript,
  setNotificationsEnabled,
  setSpawnFn,
  resetSpawnFn,
  setPlatformFn,
  resetPlatformFn,
  NOTIFICATIONS_ENABLED,
} from "./notify";

describe("notify", () => {
  let spawnCalls: string[][] = [];

  beforeEach(() => {
    spawnCalls = [];
    setSpawnFn((cmd: string[]) => {
      spawnCalls.push(cmd);
    });
    setPlatformFn(() => "darwin");
    setNotificationsEnabled(true);
  });

  afterEach(() => {
    resetSpawnFn();
    resetPlatformFn();
    setNotificationsEnabled(true);
  });

  // ===========================================================================
  // escapeForAppleScript
  // ===========================================================================

  describe("escapeForAppleScript", () => {
    it("should pass through simple strings unchanged", () => {
      expect(escapeForAppleScript("hello world")).toBe("hello world");
    });

    it("should escape double quotes", () => {
      expect(escapeForAppleScript('say "hello"')).toBe('say \\"hello\\"');
    });

    it("should escape backslashes", () => {
      expect(escapeForAppleScript("path\\to\\file")).toBe("path\\\\to\\\\file");
    });

    it("should replace newlines with spaces", () => {
      expect(escapeForAppleScript("line1\nline2\rline3")).toBe("line1 line2 line3");
    });

    it("should strip control characters", () => {
      expect(escapeForAppleScript("hello\x00world\x1f")).toBe("helloworld");
    });

    it("should handle combined escaping", () => {
      const input = 'He said "hello"\nOn line\\2\x00';
      const expected = 'He said \\"hello\\" On line\\\\2';
      expect(escapeForAppleScript(input)).toBe(expected);
    });

    it("should handle empty string", () => {
      expect(escapeForAppleScript("")).toBe("");
    });

    it("should handle single quotes (passed through)", () => {
      expect(escapeForAppleScript("it's fine")).toBe("it's fine");
    });
  });

  // ===========================================================================
  // notify()
  // ===========================================================================

  describe("notify()", () => {
    it("should construct correct osascript command", () => {
      notify("Test Title", "Test message");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][0]).toBe("osascript");
      expect(spawnCalls[0][1]).toBe("-e");
      expect(spawnCalls[0][2]).toBe(
        'display notification "Test message" with title "Test Title"'
      );
    });

    it("should include subtitle when provided", () => {
      notify("Title", "Message", "Subtitle");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toBe(
        'display notification "Message" with title "Title" subtitle "Subtitle"'
      );
    });

    it("should escape special characters in title and message", () => {
      notify('Title with "quotes"', "Message\nwith newlines");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain('Title with \\"quotes\\"');
      expect(spawnCalls[0][2]).toContain("Message with newlines");
    });

    it("should not spawn when NOTIFICATIONS_ENABLED is false", () => {
      setNotificationsEnabled(false);
      notify("Title", "Message");

      expect(spawnCalls).toHaveLength(0);
    });

    it("should not spawn on non-darwin platform", () => {
      setPlatformFn(() => "linux");
      notify("Title", "Message");

      expect(spawnCalls).toHaveLength(0);
    });

    it("should not spawn on windows platform", () => {
      setPlatformFn(() => "win32");
      notify("Title", "Message");

      expect(spawnCalls).toHaveLength(0);
    });
  });

  // ===========================================================================
  // notifyWithSound()
  // ===========================================================================

  describe("notifyWithSound()", () => {
    it("should include sound name in osascript command", () => {
      notifyWithSound("Title", "Message", "Funk");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toBe(
        'display notification "Message" with title "Title" sound name "Funk"'
      );
    });

    it("should include subtitle and sound", () => {
      notifyWithSound("Title", "Message", "Pop", "Sub");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toBe(
        'display notification "Message" with title "Title" sound name "Pop" subtitle "Sub"'
      );
    });

    it("should not spawn when disabled", () => {
      setNotificationsEnabled(false);
      notifyWithSound("Title", "Message", "Funk");

      expect(spawnCalls).toHaveLength(0);
    });

    it("should not spawn on non-darwin", () => {
      setPlatformFn(() => "linux");
      notifyWithSound("Title", "Message", "Funk");

      expect(spawnCalls).toHaveLength(0);
    });
  });

  // ===========================================================================
  // Convenience wrappers
  // ===========================================================================

  describe("notifyBlocked()", () => {
    it("should send blocked notification with Funk sound", () => {
      notifyBlocked("My Task", "Idle detection timeout");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain('with title "Task Blocked"');
      expect(spawnCalls[0][2]).toContain("My Task: Idle detection timeout");
      expect(spawnCalls[0][2]).toContain('sound name "Funk"');
    });

    it("should include project as subtitle when provided", () => {
      notifyBlocked("My Task", "Idle timeout", "brain-api");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain('subtitle "Project: brain-api"');
    });

    it("should not include subtitle when project is omitted", () => {
      notifyBlocked("My Task", "Idle timeout");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).not.toContain("subtitle");
    });
  });

  describe("notifyFailed()", () => {
    it("should send failed notification with Funk sound", () => {
      notifyFailed("My Task", "Process exited unexpectedly");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain('with title "Task Failed"');
      expect(spawnCalls[0][2]).toContain("My Task: Process exited unexpectedly");
      expect(spawnCalls[0][2]).toContain('sound name "Funk"');
    });

    it("should include project as subtitle when provided", () => {
      notifyFailed("My Task", "Crash", "my-project");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain('subtitle "Project: my-project"');
    });
  });

  describe("notifyCompleted()", () => {
    it("should send completed notification with Pop sound", () => {
      notifyCompleted("My Task");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain('with title "Task Completed"');
      expect(spawnCalls[0][2]).toContain("My Task");
      expect(spawnCalls[0][2]).toContain('sound name "Pop"');
    });

    it("should include project as subtitle when provided", () => {
      notifyCompleted("My Task", "brain-api");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain('subtitle "Project: brain-api"');
    });
  });

  // ===========================================================================
  // Edge cases
  // ===========================================================================

  describe("edge cases", () => {
    it("should handle empty strings gracefully", () => {
      notify("", "");

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toBe(
        'display notification "" with title ""'
      );
    });

    it("should handle very long messages without crashing", () => {
      const longMessage = "a".repeat(1000);
      notify("Title", longMessage);

      expect(spawnCalls).toHaveLength(1);
      expect(spawnCalls[0][2]).toContain(longMessage);
    });

    it("should re-enable notifications after being disabled", () => {
      setNotificationsEnabled(false);
      notify("Title", "Message");
      expect(spawnCalls).toHaveLength(0);

      setNotificationsEnabled(true);
      notify("Title", "Message");
      expect(spawnCalls).toHaveLength(1);
    });
  });
});
