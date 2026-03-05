/**
 * Shared status display constants for consistent TUI rendering
 *
 * Both TaskTree and StatusPopup use these constants to ensure
 * status icons and colors are consistent across the TUI.
 */

import type { EntryStatus } from '../../core/types';

/**
 * Status icons used throughout the TUI
 *
 * Note: Some icons depend on context:
 * - pending: shows as '○' normally, '●' when ready (all deps completed)
 * - in_progress: '▶'
 */
export const STATUS_ICONS: Record<EntryStatus, string> = {
  draft: '○',
  pending: '○',       // Yellow when waiting, green '●' when ready
  active: '●',        // Ready (green)
  in_progress: '▶',   // In Progress (blue)
  blocked: '✗',       // Blocked (red)
  cancelled: '⊘',     // Cancelled (yellow)
  completed: '✓',     // Completed (green dim)
  validated: '✓',     // Validated (green bright)
  superseded: '○',
  archived: '○',
};

/**
 * Ready indicator icon - used for pending tasks with all dependencies completed
 */
export const READY_ICON = '●';

/**
 * Status colors for text rendering
 */
export const STATUS_COLORS: Record<EntryStatus, string> = {
  draft: 'gray',
  pending: 'yellow',      // Yellow for waiting, green when ready
  active: 'blue',
  in_progress: 'cyan',
  blocked: 'red',
  cancelled: 'magenta',
  completed: 'green',
  validated: 'greenBright',
  superseded: 'gray',
  archived: 'gray',
};

/**
 * Get the display icon for a status
 *
 * @param status - The entry status
 * @param isReady - For pending tasks, whether all dependencies are completed
 * @returns The icon character to display
 */
export function getStatusIcon(status: EntryStatus, isReady: boolean = false): string {
  if (status === 'pending' && isReady) {
    return READY_ICON;
  }
  return STATUS_ICONS[status] ?? '?';
}

/**
 * Get the display color for a status
 *
 * @param status - The entry status
 * @param isReady - For pending tasks, whether all dependencies are completed
 * @returns The color string for Ink Text component
 */
export function getStatusColor(status: EntryStatus, isReady: boolean = false): string {
  if (status === 'pending' && isReady) {
    return 'green';
  }
  return STATUS_COLORS[status] ?? 'white';
}

/**
 * Get display label for status (replaces underscores with spaces)
 */
export function getStatusLabel(status: EntryStatus): string {
  return status.replace('_', ' ');
}
