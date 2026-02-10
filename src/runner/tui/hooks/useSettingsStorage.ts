/**
 * Hook for persisting TUI settings across restarts
 *
 * Features:
 * - Persist visibleGroups and groupCollapsed settings to JSON file
 * - Load settings on mount with graceful fallback to defaults
 * - Debounced save on change (250ms to batch rapid toggles)
 * - Silent failure on I/O errors (TUI continues with in-memory state)
 */

import { useState, useCallback, useEffect, useRef } from 'react';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'fs';
import { dirname, join } from 'path';
import { homedir } from 'os';

// =============================================================================
// Types
// =============================================================================

export interface TUISettings {
  /** Which status groups to display in the task list */
  visibleGroups: string[];
  /** Collapsed state for each status group */
  groupCollapsed: Record<string, boolean>;
}

export interface UseSettingsStorageOptions {
  /** Directory to store settings file (default: ~/.brain) */
  settingsDir?: string;
  /** Debounce delay in ms for saving (default: 250) */
  debounceMs?: number;
}

/** Setter type that supports both direct value and functional update */
export type SetStateAction<T> = T | ((prev: T) => T);

export interface UseSettingsStorageResult {
  /** Currently visible groups as a Set */
  visibleGroups: Set<string>;
  /** Collapsed state for each group */
  groupCollapsed: Record<string, boolean>;
  /** Update visible groups (supports functional updates) */
  setVisibleGroups: (action: SetStateAction<Set<string>>) => void;
  /** Update collapsed state (supports functional updates) */
  setGroupCollapsed: (action: SetStateAction<Record<string, boolean>>) => void;
  /** Check if settings file exists and was loaded */
  isLoaded: boolean;
}

// =============================================================================
// Default Settings
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
// File I/O Helpers
// =============================================================================

function getDefaultSettingsPath(): string {
  return join(homedir(), '.brain', 'tui-settings.json');
}

/**
 * Load settings from JSON file.
 * Returns null if file doesn't exist or is corrupted.
 */
function loadSettingsFromFile(filePath: string): TUISettings | null {
  try {
    if (!existsSync(filePath)) {
      return null;
    }
    const content = readFileSync(filePath, 'utf-8');
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
    // Corrupted JSON or read error - fall back to defaults
    return null;
  }
}

/**
 * Save settings to JSON file.
 * Creates directory if needed.
 */
function saveSettingsToFile(filePath: string, settings: TUISettings): void {
  try {
    const dir = dirname(filePath);
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true });
    }
    writeFileSync(filePath, JSON.stringify(settings, null, 2), 'utf-8');
  } catch {
    // Silently fail - don't disrupt TUI for file write errors
  }
}

// =============================================================================
// Hook Implementation
// =============================================================================

/**
 * Manage TUI settings with file persistence
 *
 * @param options - Configuration options
 * @returns Settings state and controls
 *
 * @example
 * ```tsx
 * const { visibleGroups, groupCollapsed, setVisibleGroups, setGroupCollapsed } = useSettingsStorage();
 *
 * // Toggle a group's visibility
 * setVisibleGroups(prev => {
 *   const next = new Set(prev);
 *   if (next.has('archived')) {
 *     next.delete('archived');
 *   } else {
 *     next.add('archived');
 *   }
 *   return next;
 * });
 *
 * // Update collapsed state
 * setGroupCollapsed(prev => ({
 *   ...prev,
 *   draft: !prev.draft,
 * }));
 * ```
 */
export function useSettingsStorage(options?: UseSettingsStorageOptions): UseSettingsStorageResult {
  const settingsPath = options?.settingsDir 
    ? join(options.settingsDir, 'tui-settings.json')
    : getDefaultSettingsPath();
  const debounceMs = options?.debounceMs ?? 250;

  // Load initial settings from file
  const [isLoaded, setIsLoaded] = useState(false);
  const [visibleGroups, setVisibleGroupsState] = useState<Set<string>>(() => {
    const loaded = loadSettingsFromFile(settingsPath);
    if (loaded) {
      return new Set(loaded.visibleGroups);
    }
    return new Set(DEFAULT_VISIBLE_GROUPS);
  });

  const [groupCollapsed, setGroupCollapsedState] = useState<Record<string, boolean>>(() => {
    const loaded = loadSettingsFromFile(settingsPath);
    if (loaded) {
      // Merge with defaults to ensure all statuses have a collapsed state
      return { ...DEFAULT_GROUP_COLLAPSED, ...loaded.groupCollapsed };
    }
    return { ...DEFAULT_GROUP_COLLAPSED };
  });

  // Mark as loaded after initial render
  useEffect(() => {
    setIsLoaded(true);
  }, []);

  // Debounced save ref
  const saveTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const settingsPathRef = useRef(settingsPath);
  settingsPathRef.current = settingsPath;

  // Save function (debounced)
  const scheduleSave = useCallback((groups: Set<string>, collapsed: Record<string, boolean>) => {
    if (saveTimeoutRef.current) {
      clearTimeout(saveTimeoutRef.current);
    }
    saveTimeoutRef.current = setTimeout(() => {
      const settings: TUISettings = {
        visibleGroups: Array.from(groups),
        groupCollapsed: collapsed,
      };
      saveSettingsToFile(settingsPathRef.current, settings);
    }, debounceMs);
  }, [debounceMs]);

  // Cleanup timeout on unmount
  useEffect(() => {
    return () => {
      if (saveTimeoutRef.current) {
        clearTimeout(saveTimeoutRef.current);
      }
    };
  }, []);

  // Wrapper to update visible groups and trigger save
  const setVisibleGroups = useCallback((action: SetStateAction<Set<string>>) => {
    setVisibleGroupsState(currentGroups => {
      const newGroups = typeof action === 'function' ? action(currentGroups) : action;
      // Schedule save with latest collapsed state
      setGroupCollapsedState(currentCollapsed => {
        scheduleSave(newGroups, currentCollapsed);
        return currentCollapsed;
      });
      return newGroups;
    });
  }, [scheduleSave]);

  // Wrapper to update collapsed state and trigger save
  const setGroupCollapsed = useCallback((action: SetStateAction<Record<string, boolean>>) => {
    setGroupCollapsedState(currentCollapsed => {
      const newCollapsed = typeof action === 'function' ? action(currentCollapsed) : action;
      // Schedule save with latest visible groups
      setVisibleGroupsState(currentGroups => {
        scheduleSave(currentGroups, newCollapsed);
        return currentGroups;
      });
      return newCollapsed;
    });
  }, [scheduleSave]);

  return {
    visibleGroups,
    groupCollapsed,
    setVisibleGroups,
    setGroupCollapsed,
    isLoaded,
  };
}

export default useSettingsStorage;
