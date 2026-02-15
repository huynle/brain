/**
 * Hook for filtering tasks in the TUI
 *
 * Features:
 * - Filter mode states: off, typing, locked
 * - Case-insensitive matching on title, feature_id, and tags
 * - Integration with flattenFeatureOrder for navigation
 * - Dynamic filtering as user types
 */

import { useState, useCallback, useMemo } from 'react';
import type { TaskDisplay } from '../types';
import { flattenFeatureOrder } from '../components/TaskTree';

// =============================================================================
// Types
// =============================================================================

export type FilterMode = 'off' | 'typing' | 'locked';

export interface UseTaskFilterOptions {
  /** Tasks to filter */
  tasks: TaskDisplay[];
  /** Whether completed section is collapsed (for navigation order) */
  completedCollapsed?: boolean;
  /** Whether draft section is collapsed (for navigation order) */
  draftCollapsed?: boolean;
  /** Set of collapsed feature IDs (for navigation order) */
  collapsedFeatures?: Set<string>;
  /** Set of status groups that should be visible (from Settings > Groups) */
  visibleGroups?: Set<string>;
  /** Whether cancelled section is collapsed (for navigation order) */
  cancelledCollapsed?: boolean;
  /** Whether superseded section is collapsed (for navigation order) */
  supersededCollapsed?: boolean;
  /** Whether archived section is collapsed (for navigation order) */
  archivedCollapsed?: boolean;
}

export interface UseTaskFilterResult {
  /** Current filter text */
  filterText: string;
  /** Current filter mode: off, typing, or locked */
  filterMode: FilterMode;
  /** Tasks after filtering applied */
  filteredTasks: TaskDisplay[];
  /** Navigation order (task IDs + header IDs) for filtered tasks */
  navigationOrder: string[];
  /** Number of tasks matching current filter */
  matchCount: number;
  /** Total number of tasks before filtering */
  totalCount: number;
  /** Activate filter mode (start typing) */
  activate: () => void;
  /** Deactivate filter mode and clear filter */
  deactivate: () => void;
  /** Lock in current filter (stop typing, keep filter active) */
  lockIn: () => void;
  /** Handle a character input (only works in typing mode) */
  handleChar: (char: string) => void;
  /** Handle backspace (only works in typing mode) */
  handleBackspace: () => void;
}

// =============================================================================
// Filter Logic
// =============================================================================

/**
 * Check if a task matches the filter text.
 * Performs case-insensitive substring matching on:
 * - title
 * - feature_id
 * - tags (joined with space)
 * 
 * @param task - The task to check
 * @param filterText - The filter text to match against
 * @returns true if the task matches the filter
 */
export function taskMatchesFilter(task: TaskDisplay, filterText: string): boolean {
  if (!filterText) return true;
  
  const lowerFilter = filterText.toLowerCase();
  
  // Match against title
  if (task.title.toLowerCase().includes(lowerFilter)) {
    return true;
  }
  
  // Match against feature_id
  if (task.feature_id && task.feature_id.toLowerCase().includes(lowerFilter)) {
    return true;
  }
  
  // Match against tags
  if (task.tags.length > 0) {
    const tagsString = task.tags.join(' ').toLowerCase();
    if (tagsString.includes(lowerFilter)) {
      return true;
    }
  }
  
  return false;
}

// =============================================================================
// Hook Implementation
// =============================================================================

/**
 * Manage task filtering with typing and locked states.
 *
 * @param options - Configuration options including tasks and collapsed states
 * @returns Filter state and controls
 *
 * @example
 * ```tsx
 * const {
 *   filterText,
 *   filterMode,
 *   filteredTasks,
 *   navigationOrder,
 *   activate,
 *   deactivate,
 *   lockIn,
 *   handleChar,
 *   handleBackspace,
 * } = useTaskFilter({ tasks, completedCollapsed, draftCollapsed, collapsedFeatures });
 *
 * // User presses "/" to start filtering
 * activate();
 *
 * // User types characters
 * handleChar('a');
 * handleChar('u');
 * handleChar('t');
 * handleChar('h');
 *
 * // User presses Enter to lock in filter
 * lockIn();
 *
 * // User presses Escape to clear filter
 * deactivate();
 * ```
 */
export function useTaskFilter(options: UseTaskFilterOptions): UseTaskFilterResult {
  const {
    tasks,
    completedCollapsed = true,
    draftCollapsed = true,
    collapsedFeatures = new Set<string>(),
    visibleGroups,
    cancelledCollapsed = true,
    supersededCollapsed = true,
    archivedCollapsed = true,
  } = options;

  const [filterText, setFilterText] = useState('');
  const [filterMode, setFilterMode] = useState<FilterMode>('off');

  // Activate filter mode (start typing)
  const activate = useCallback(() => {
    if (filterMode === 'locked') {
      // Unlock - return to typing mode to allow editing
      setFilterMode('typing');
    } else {
      setFilterMode('typing');
    }
  }, [filterMode]);

  // Deactivate filter mode and clear filter
  const deactivate = useCallback(() => {
    setFilterMode('off');
    setFilterText('');
  }, []);

  // Lock in current filter
  const lockIn = useCallback(() => {
    setFilterMode('locked');
  }, []);

  // Handle character input
  const handleChar = useCallback((char: string) => {
    if (filterMode !== 'typing') return;
    setFilterText(prev => prev + char);
  }, [filterMode]);

  // Handle backspace
  const handleBackspace = useCallback(() => {
    if (filterMode !== 'typing') return;
    setFilterText(prev => prev.slice(0, -1));
  }, [filterMode]);

  // Apply filter to tasks
  const filteredTasks = useMemo(() => {
    let result = tasks;
    
    // Apply status visibility filter (from Settings > Groups)
    if (visibleGroups && visibleGroups.size > 0) {
      result = result.filter(task => visibleGroups.has(task.status));
    }
    
    // Apply text filter
    if (filterMode !== 'off' && filterText) {
      result = result.filter(task => taskMatchesFilter(task, filterText));
    }
    
    return result;
  }, [tasks, filterText, filterMode, visibleGroups]);

  // Calculate navigation order for filtered tasks
  const navigationOrder = useMemo(() => {
    return flattenFeatureOrder(
      filteredTasks,
      completedCollapsed,
      draftCollapsed,
      collapsedFeatures,
      cancelledCollapsed,
      supersededCollapsed,
      archivedCollapsed
    );
  }, [filteredTasks, completedCollapsed, draftCollapsed, collapsedFeatures, cancelledCollapsed, supersededCollapsed, archivedCollapsed]);

  // Calculate match and total counts
  const matchCount = filteredTasks.length;
  const totalCount = tasks.length;

  return {
    filterText,
    filterMode,
    filteredTasks,
    navigationOrder,
    matchCount,
    totalCount,
    activate,
    deactivate,
    lockIn,
    handleChar,
    handleBackspace,
  };
}

export default useTaskFilter;
