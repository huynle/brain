/**
 * TmuxManager Tests
 *
 * Tests for the tmux dashboard management module.
 * Note: Full integration tests require tmux to be installed and running.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import {
  TmuxManager,
  getTmuxManager,
  resetTmuxManager,
  type StatusInfo,
} from "./tmux-manager";

// =============================================================================
// Test Helpers
// =============================================================================

function createMockStatus(overrides: Partial<StatusInfo> = {}): StatusInfo {
  return {
    projectId: "test-project",
    status: "polling",
    ready: 5,
    running: 2,
    waiting: 3,
    blocked: 1,
    completed: 10,
    recentCompletions: ["Task A", "Task B", "Task C"],
    ...overrides,
  };
}

// =============================================================================
// Unit Tests (no tmux required)
// =============================================================================

describe("TmuxManager", () => {
  let manager: TmuxManager;

  beforeEach(() => {
    resetTmuxManager();
    manager = new TmuxManager();
  });

  afterEach(async () => {
    manager.stopStatusUpdates();
    resetTmuxManager();
  });

  describe("isTmuxAvailable()", () => {
    test("returns boolean for tmux availability", async () => {
      const result = await manager.isTmuxAvailable();
      expect(typeof result).toBe("boolean");
    });
  });

  describe("isInsideTmux()", () => {
    test("returns boolean based on TMUX env var", async () => {
      const result = await manager.isInsideTmux();
      const expected = process.env.TMUX !== undefined;
      expect(result).toBe(expected);
    });
  });

  describe("getLayout()", () => {
    test("returns null before dashboard creation", () => {
      expect(manager.getLayout()).toBeNull();
    });
  });

  describe("getTaskPane()", () => {
    test("returns undefined when no layout", () => {
      expect(manager.getTaskPane("task1")).toBeUndefined();
    });
  });

  describe("singleton", () => {
    test("getTmuxManager returns same instance", () => {
      const instance1 = getTmuxManager();
      const instance2 = getTmuxManager();
      expect(instance1).toBe(instance2);
    });

    test("resetTmuxManager creates new instance", () => {
      const instance1 = getTmuxManager();
      resetTmuxManager();
      const instance2 = getTmuxManager();
      expect(instance1).not.toBe(instance2);
    });
  });

  describe("startStatusUpdates() / stopStatusUpdates()", () => {
    test("can start and stop updates without error", () => {
      // Should not throw
      manager.startStatusUpdates(1000);
      manager.stopStatusUpdates();
    });

    test("can call stopStatusUpdates multiple times", () => {
      manager.startStatusUpdates(1000);
      manager.stopStatusUpdates();
      manager.stopStatusUpdates(); // Should not throw
    });

    test("starting updates clears previous interval", () => {
      manager.startStatusUpdates(1000);
      manager.startStatusUpdates(2000); // Should clear previous
      manager.stopStatusUpdates();
    });
  });

  describe("updateStatusPane() without layout", () => {
    test("does not throw when no layout exists", async () => {
      const status = createMockStatus();
      // Should not throw
      await manager.updateStatusPane(status);
    });
  });

  describe("writeToLogPane() without layout", () => {
    test("does not throw when no layout exists", async () => {
      // Should not throw
      await manager.writeToLogPane("test message");
    });
  });

  describe("addTaskPane() without layout", () => {
    test("throws when dashboard not created", async () => {
      await expect(manager.addTaskPane("task1", "Test Task", "echo hello")).rejects.toThrow(
        "Dashboard not created"
      );
    });
  });

  describe("removeTaskPane() without layout", () => {
    test("returns false when no layout", async () => {
      const result = await manager.removeTaskPane("task1");
      expect(result).toBe(false);
    });
  });

  describe("cleanup() without layout", () => {
    test("completes without error when no layout", async () => {
      // Should not throw
      await manager.cleanup();
    });
  });
});

// =============================================================================
// Integration Tests (require tmux and NOT being inside tmux session)
// =============================================================================

describe("TmuxManager Integration", () => {
  let manager: TmuxManager;
  let tmuxAvailable: boolean;
  let insideTmux: boolean;
  const testSessionName = `test-session-${Date.now()}`;

  beforeEach(async () => {
    resetTmuxManager();
    manager = new TmuxManager();
    tmuxAvailable = await manager.isTmuxAvailable();
    insideTmux = await manager.isInsideTmux();
  });

  afterEach(async () => {
    // Clean up any created dashboard
    try {
      await manager.cleanup();
    } catch {
      // Ignore cleanup errors
    }

    // Also try to kill the test session directly
    if (tmuxAvailable) {
      try {
        await Bun.$`tmux kill-session -t ${testSessionName} 2>/dev/null || true`.quiet();
      } catch {
        // Session might not exist
      }
    }

    resetTmuxManager();
  });

  // Helper to check if we can run tmux tests
  const canRunTmuxTests = () => tmuxAvailable && !insideTmux;

  describe("createDashboard()", () => {
    test("throws when tmux not available", async () => {
      // Only run this test if tmux is NOT available
      if (tmuxAvailable) {
        // Test passes trivially when tmux is available
        expect(true).toBe(true);
        return;
      }

      await expect(manager.createDashboard("test")).rejects.toThrow(
        "tmux is not available"
      );
    });

    test("creates dashboard layout when tmux available", async () => {
      if (!canRunTmuxTests()) {
        // Skip: tmux not available or inside tmux session
        expect(true).toBe(true);
        return;
      }

      const layout = await manager.createDashboard(testSessionName);

      expect(layout).toBeDefined();
      expect(layout.sessionName).toBe(testSessionName);
      expect(layout.windowName).toBe(`dashboard-${testSessionName}`);
      expect(layout.statusPaneId).toBeTruthy();
      expect(layout.logPaneId).toBeTruthy();
      expect(layout.taskPanes).toEqual([]);
    });
  });

  describe("addTaskPane()", () => {
    test("adds task pane to layout", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);

      const paneId = await manager.addTaskPane("task1", "Test Task 1", "sleep 60");

      expect(paneId).toBeTruthy();
      expect(paneId.startsWith("%")).toBe(true);

      const layout = manager.getLayout();
      expect(layout?.taskPanes).toHaveLength(1);
      expect(layout?.taskPanes[0].taskId).toBe("task1");
      expect(layout?.taskPanes[0].paneId).toBe(paneId);
    });

    test("adds multiple task panes", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);

      await manager.addTaskPane("task1", "Test Task 1", "sleep 60");
      await manager.addTaskPane("task2", "Test Task 2", "sleep 60");
      await manager.addTaskPane("task3", "Test Task 3", "sleep 60");

      const layout = manager.getLayout();
      expect(layout?.taskPanes).toHaveLength(3);
    });
  });

  describe("removeTaskPane()", () => {
    test("removes existing task pane", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);
      await manager.addTaskPane("task1", "Test Task 1", "sleep 60");

      const result = await manager.removeTaskPane("task1");

      expect(result).toBe(true);
      expect(manager.getLayout()?.taskPanes).toHaveLength(0);
    });

    test("returns false for non-existent task", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);

      const result = await manager.removeTaskPane("nonexistent");

      expect(result).toBe(false);
    });
  });

  describe("getTaskPane()", () => {
    test("returns pane info for existing task", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);
      const paneId = await manager.addTaskPane("task1", "Test Task 1", "sleep 60");

      const paneInfo = manager.getTaskPane("task1");

      expect(paneInfo).toBeDefined();
      expect(paneInfo?.taskId).toBe("task1");
      expect(paneInfo?.paneId).toBe(paneId);
      expect(paneInfo?.title).toBe("Test Task 1");
    });

    test("returns undefined for non-existent task", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);

      const paneInfo = manager.getTaskPane("nonexistent");

      expect(paneInfo).toBeUndefined();
    });
  });

  describe("isPaneAlive()", () => {
    test("returns true for existing pane", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);
      const layout = manager.getLayout();

      const alive = await manager.isPaneAlive(layout!.statusPaneId);

      expect(alive).toBe(true);
    });

    test("returns false for non-existent pane", async () => {
      if (!tmuxAvailable) {
        expect(true).toBe(true);
        return;
      }

      const alive = await manager.isPaneAlive("%99999");

      expect(alive).toBe(false);
    });
  });

  describe("updateStatusPane()", () => {
    test("updates status display", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);

      const status = createMockStatus();
      // Should not throw
      await manager.updateStatusPane(status);
    });
  });

  describe("writeToLogPane()", () => {
    test("writes to log pane", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);

      // Should not throw
      await manager.writeToLogPane("Test log message");
      await manager.writeToLogPane("Another log message");
    });
  });

  describe("cleanup()", () => {
    test("cleans up dashboard resources", async () => {
      if (!canRunTmuxTests()) {
        expect(true).toBe(true);
        return;
      }

      await manager.createDashboard(testSessionName);
      await manager.addTaskPane("task1", "Test Task 1", "sleep 60");

      await manager.cleanup();

      expect(manager.getLayout()).toBeNull();
    });
  });
});
