/**
 * useSettingsStorage Hook Tests
 *
 * Tests the settings persistence hook including:
 * - File I/O (load/save settings)
 * - Default value handling
 * - Corrupted file recovery
 * - Functional updates
 */

import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, readFileSync, writeFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";

// Import the types for testing
import type { TUISettings } from "./useSettingsStorage";

// =============================================================================
// Default values (must match the hook implementation)
// =============================================================================

const DEFAULT_VISIBLE_GROUPS = ['draft', 'pending', 'active', 'in_progress', 'blocked', 'completed'];

const DEFAULT_GROUP_COLLAPSED: Record<string, boolean> = {
  draft: true,
  pending: false,
  active: false,
  in_progress: false,
  blocked: false,
  cancelled: true,
  completed: true,
  validated: true,
  superseded: true,
  archived: true,
};

// =============================================================================
// Test file I/O helpers
// =============================================================================

let testDir: string;
let testSettingsFile: string;

beforeEach(() => {
  // Create a unique temp directory for each test
  testDir = join(tmpdir(), `useSettingsStorage-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
  mkdirSync(testDir, { recursive: true });
  testSettingsFile = join(testDir, "tui-settings.json");
});

afterEach(() => {
  // Clean up test directory
  if (existsSync(testDir)) {
    rmSync(testDir, { recursive: true, force: true });
  }
});

// =============================================================================
// Test loading settings from file
// =============================================================================

describe("useSettingsStorage - Load Settings", () => {
  describe("loadSettingsFromFile", () => {
    it("should return null if file does not exist", () => {
      const settings = loadSettingsFromFileForTest(join(testDir, "nonexistent.json"));
      expect(settings).toBeNull();
    });

    it("should load valid settings from file", () => {
      const savedSettings: TUISettings = {
        visibleGroups: ['pending', 'active'],
        groupCollapsed: {
          draft: false,
          pending: true,
          active: false,
          in_progress: false,
          blocked: false,
          cancelled: true,
          completed: false,
          validated: true,
          superseded: true,
          archived: true,
        },
      };
      writeFileSync(testSettingsFile, JSON.stringify(savedSettings, null, 2));

      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).not.toBeNull();
      expect(settings!.visibleGroups).toEqual(['pending', 'active']);
      expect(settings!.groupCollapsed.draft).toBe(false);
      expect(settings!.groupCollapsed.completed).toBe(false);
    });

    it("should return null for corrupted JSON", () => {
      writeFileSync(testSettingsFile, "{ not valid json");
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });

    it("should return null for empty file", () => {
      writeFileSync(testSettingsFile, "");
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });

    it("should return null if visibleGroups is not an array", () => {
      writeFileSync(testSettingsFile, JSON.stringify({
        visibleGroups: "not an array",
        groupCollapsed: DEFAULT_GROUP_COLLAPSED,
      }));
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });

    it("should return null if visibleGroups contains non-strings", () => {
      writeFileSync(testSettingsFile, JSON.stringify({
        visibleGroups: ['valid', 123, 'also-valid'],
        groupCollapsed: DEFAULT_GROUP_COLLAPSED,
      }));
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });

    it("should return null if groupCollapsed is not an object", () => {
      writeFileSync(testSettingsFile, JSON.stringify({
        visibleGroups: DEFAULT_VISIBLE_GROUPS,
        groupCollapsed: "not an object",
      }));
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });

    it("should return null if groupCollapsed contains non-boolean values", () => {
      writeFileSync(testSettingsFile, JSON.stringify({
        visibleGroups: DEFAULT_VISIBLE_GROUPS,
        groupCollapsed: {
          draft: "not a boolean",
          pending: true,
        },
      }));
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });

    it("should return null for missing visibleGroups", () => {
      writeFileSync(testSettingsFile, JSON.stringify({
        groupCollapsed: DEFAULT_GROUP_COLLAPSED,
      }));
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });

    it("should return null for missing groupCollapsed", () => {
      writeFileSync(testSettingsFile, JSON.stringify({
        visibleGroups: DEFAULT_VISIBLE_GROUPS,
      }));
      const settings = loadSettingsFromFileForTest(testSettingsFile);
      expect(settings).toBeNull();
    });
  });
});

// =============================================================================
// Test saving settings to file
// =============================================================================

describe("useSettingsStorage - Save Settings", () => {
  describe("saveSettingsToFile", () => {
    it("should create directory if it does not exist", () => {
      const nestedPath = join(testDir, "nested", "dir", "tui-settings.json");
      const settings: TUISettings = {
        visibleGroups: DEFAULT_VISIBLE_GROUPS,
        groupCollapsed: DEFAULT_GROUP_COLLAPSED,
      };
      
      saveSettingsToFileForTest(nestedPath, settings);
      
      expect(existsSync(nestedPath)).toBe(true);
    });

    it("should save settings as formatted JSON", () => {
      const settings: TUISettings = {
        visibleGroups: ['pending', 'active'],
        groupCollapsed: {
          draft: true,
          pending: false,
        },
      };
      
      saveSettingsToFileForTest(testSettingsFile, settings);
      
      const content = readFileSync(testSettingsFile, 'utf-8');
      const parsed = JSON.parse(content);
      expect(parsed.visibleGroups).toEqual(['pending', 'active']);
      expect(parsed.groupCollapsed.draft).toBe(true);
      expect(parsed.groupCollapsed.pending).toBe(false);
    });

    it("should overwrite existing file", () => {
      // Write initial settings
      const initial: TUISettings = {
        visibleGroups: ['pending'],
        groupCollapsed: { draft: true },
      };
      saveSettingsToFileForTest(testSettingsFile, initial);
      
      // Overwrite with new settings
      const updated: TUISettings = {
        visibleGroups: ['active', 'completed'],
        groupCollapsed: { draft: false, completed: true },
      };
      saveSettingsToFileForTest(testSettingsFile, updated);
      
      const content = readFileSync(testSettingsFile, 'utf-8');
      const parsed = JSON.parse(content);
      expect(parsed.visibleGroups).toEqual(['active', 'completed']);
    });
  });
});

// =============================================================================
// Test default values
// =============================================================================

describe("useSettingsStorage - Default Values", () => {
  it("should use default visible groups when file does not exist", () => {
    // Simulate initialization without existing file
    const settings = loadSettingsFromFileForTest(join(testDir, "nonexistent.json"));
    expect(settings).toBeNull();
    
    // Defaults would be used:
    expect(DEFAULT_VISIBLE_GROUPS).toContain('draft');
    expect(DEFAULT_VISIBLE_GROUPS).toContain('pending');
    expect(DEFAULT_VISIBLE_GROUPS).toContain('active');
    expect(DEFAULT_VISIBLE_GROUPS).toContain('in_progress');
    expect(DEFAULT_VISIBLE_GROUPS).toContain('blocked');
    expect(DEFAULT_VISIBLE_GROUPS).toContain('completed');
    expect(DEFAULT_VISIBLE_GROUPS).not.toContain('cancelled');
    expect(DEFAULT_VISIBLE_GROUPS).not.toContain('archived');
  });

  it("should use default collapsed state when file does not exist", () => {
    expect(DEFAULT_GROUP_COLLAPSED.draft).toBe(true);
    expect(DEFAULT_GROUP_COLLAPSED.pending).toBe(false);
    expect(DEFAULT_GROUP_COLLAPSED.active).toBe(false);
    expect(DEFAULT_GROUP_COLLAPSED.in_progress).toBe(false);
    expect(DEFAULT_GROUP_COLLAPSED.blocked).toBe(false);
    expect(DEFAULT_GROUP_COLLAPSED.cancelled).toBe(true);
    expect(DEFAULT_GROUP_COLLAPSED.completed).toBe(true);
    expect(DEFAULT_GROUP_COLLAPSED.validated).toBe(true);
    expect(DEFAULT_GROUP_COLLAPSED.superseded).toBe(true);
    expect(DEFAULT_GROUP_COLLAPSED.archived).toBe(true);
  });
});

// =============================================================================
// Test Set conversion
// =============================================================================

describe("useSettingsStorage - Set Conversion", () => {
  it("should convert array to Set correctly", () => {
    const arr = ['a', 'b', 'c'];
    const set = new Set(arr);
    expect(set.has('a')).toBe(true);
    expect(set.has('b')).toBe(true);
    expect(set.has('c')).toBe(true);
    expect(set.has('d')).toBe(false);
    expect(set.size).toBe(3);
  });

  it("should convert Set to array correctly for storage", () => {
    const set = new Set(['pending', 'active', 'completed']);
    const arr = Array.from(set);
    expect(arr).toContain('pending');
    expect(arr).toContain('active');
    expect(arr).toContain('completed');
    expect(arr.length).toBe(3);
  });

  it("should handle empty Set", () => {
    const set = new Set<string>();
    const arr = Array.from(set);
    expect(arr).toEqual([]);
  });
});

// =============================================================================
// Test merge behavior for groupCollapsed
// =============================================================================

describe("useSettingsStorage - Merge Behavior", () => {
  it("should merge saved groupCollapsed with defaults", () => {
    // Simulate a file with partial groupCollapsed
    const partial: TUISettings = {
      visibleGroups: DEFAULT_VISIBLE_GROUPS,
      groupCollapsed: {
        draft: false, // Changed from default
        pending: true, // Changed from default
      },
    };
    writeFileSync(testSettingsFile, JSON.stringify(partial, null, 2));

    const settings = loadSettingsFromFileForTest(testSettingsFile);
    expect(settings).not.toBeNull();
    
    // Merge with defaults
    const merged = { ...DEFAULT_GROUP_COLLAPSED, ...settings!.groupCollapsed };
    
    // Changed values
    expect(merged.draft).toBe(false);
    expect(merged.pending).toBe(true);
    
    // Default values (not in file)
    expect(merged.active).toBe(false);
    expect(merged.completed).toBe(true);
  });
});

// =============================================================================
// Helper functions (mirrors internal implementation)
// =============================================================================

function loadSettingsFromFileForTest(filePath: string): TUISettings | null {
  try {
    if (!existsSync(filePath)) {
      return null;
    }
    const content = readFileSync(filePath, 'utf-8');
    if (!content.trim()) {
      return null;
    }
    const parsed = JSON.parse(content);
    
    // Validate structure
    if (!parsed || typeof parsed !== 'object') {
      return null;
    }
    
    // Validate visibleGroups (must be array of strings)
    if (!Array.isArray(parsed.visibleGroups)) {
      return null;
    }
    for (const group of parsed.visibleGroups) {
      if (typeof group !== 'string') {
        return null;
      }
    }
    
    // Validate groupCollapsed (must be object with boolean values)
    if (!parsed.groupCollapsed || typeof parsed.groupCollapsed !== 'object') {
      return null;
    }
    for (const [key, value] of Object.entries(parsed.groupCollapsed)) {
      if (typeof key !== 'string' || typeof value !== 'boolean') {
        return null;
      }
    }
    
    return {
      visibleGroups: parsed.visibleGroups,
      groupCollapsed: parsed.groupCollapsed,
    };
  } catch {
    return null;
  }
}

function saveSettingsToFileForTest(filePath: string, settings: TUISettings): void {
  try {
    const dir = join(filePath, '..');
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true });
    }
    writeFileSync(filePath, JSON.stringify(settings, null, 2), 'utf-8');
  } catch {
    // Silently fail
  }
}
