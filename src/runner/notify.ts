/**
 * macOS Notification Utility
 *
 * Sends desktop notifications via osascript. Non-blocking, fire-and-forget.
 * Silently no-ops on non-macOS platforms.
 */

// =============================================================================
// Configuration
// =============================================================================

/**
 * Global flag to enable/disable notifications.
 * Set to false to suppress all notifications without removing hooks.
 */
export let NOTIFICATIONS_ENABLED = true;

/**
 * Enable or disable notifications at runtime.
 */
export function setNotificationsEnabled(enabled: boolean): void {
  NOTIFICATIONS_ENABLED = enabled;
}

// =============================================================================
// Spawn abstraction (for testability)
// =============================================================================

type SpawnFn = (cmd: string[]) => void;

let spawnFn: SpawnFn = (cmd: string[]) => {
  try {
    Bun.spawn(cmd, {
      stdout: "ignore",
      stderr: "ignore",
    });
  } catch {
    // Fire-and-forget: swallow all errors
  }
};

/**
 * Override the spawn function (for testing).
 */
export function setSpawnFn(fn: SpawnFn): void {
  spawnFn = fn;
}

/**
 * Reset spawn function to default Bun.spawn.
 */
export function resetSpawnFn(): void {
  spawnFn = (cmd: string[]) => {
    try {
      Bun.spawn(cmd, {
        stdout: "ignore",
        stderr: "ignore",
      });
    } catch {
      // Fire-and-forget
    }
  };
}

// =============================================================================
// Platform detection abstraction (for testability)
// =============================================================================

let platformFn: () => string = () => process.platform;

/**
 * Override the platform detection function (for testing).
 */
export function setPlatformFn(fn: () => string): void {
  platformFn = fn;
}

/**
 * Reset platform detection to default.
 */
export function resetPlatformFn(): void {
  platformFn = () => process.platform;
}

// =============================================================================
// Core
// =============================================================================

/**
 * Escape a string for safe inclusion in an AppleScript single-quoted string.
 * Replaces single quotes with escaped version and strips control characters.
 */
export function escapeForAppleScript(str: string): string {
  return str
    .replace(/\\/g, "\\\\")       // Escape backslashes first
    .replace(/"/g, '\\"')          // Escape double quotes
    .replace(/[\r\n]+/g, " ")     // Replace newlines with space
    .replace(/[\x00-\x1f]/g, ""); // Strip other control characters
}

/**
 * Send a macOS desktop notification via osascript.
 *
 * Non-blocking, fire-and-forget. Silently no-ops on non-macOS platforms
 * or when notifications are disabled.
 */
export function notify(title: string, message: string, subtitle?: string): void {
  if (!NOTIFICATIONS_ENABLED) return;
  if (platformFn() !== "darwin") return;

  const escapedTitle = escapeForAppleScript(title);
  const escapedMessage = escapeForAppleScript(message);

  let script = `display notification "${escapedMessage}" with title "${escapedTitle}"`;

  if (subtitle) {
    const escapedSubtitle = escapeForAppleScript(subtitle);
    script += ` subtitle "${escapedSubtitle}"`;
  }

  spawnFn(["osascript", "-e", script]);
}

/**
 * Send a macOS notification with a sound.
 */
export function notifyWithSound(
  title: string,
  message: string,
  sound: string,
  subtitle?: string,
): void {
  if (!NOTIFICATIONS_ENABLED) return;
  if (platformFn() !== "darwin") return;

  const escapedTitle = escapeForAppleScript(title);
  const escapedMessage = escapeForAppleScript(message);

  let script = `display notification "${escapedMessage}" with title "${escapedTitle}" sound name "${sound}"`;

  if (subtitle) {
    const escapedSubtitle = escapeForAppleScript(subtitle);
    script += ` subtitle "${escapedSubtitle}"`;
  }

  spawnFn(["osascript", "-e", script]);
}

// =============================================================================
// Convenience Wrappers
// =============================================================================

/**
 * Notify that a task is blocked (needs attention).
 * Uses "Funk" sound to indicate urgency.
 */
export function notifyBlocked(taskTitle: string, reason: string, project?: string): void {
  const subtitle = project ? `Project: ${project}` : undefined;
  notifyWithSound("Task Blocked", `${taskTitle}: ${reason}`, "Funk", subtitle);
}

/**
 * Notify that a task has failed.
 * Uses "Funk" sound to indicate urgency.
 */
export function notifyFailed(taskTitle: string, error: string, project?: string): void {
  const subtitle = project ? `Project: ${project}` : undefined;
  notifyWithSound("Task Failed", `${taskTitle}: ${error}`, "Funk", subtitle);
}

/**
 * Notify that a task has completed.
 * Uses "Pop" sound for positive feedback.
 */
export function notifyCompleted(taskTitle: string, project?: string): void {
  const subtitle = project ? `Project: ${project}` : undefined;
  notifyWithSound("Task Completed", taskTitle, "Pop", subtitle);
}

/**
 * Notify that a feature review has been queued.
 * Sent when the user enables auto-review for a feature via the TUI.
 * Uses "Pop" sound for positive feedback.
 */
export function notifyFeatureReviewQueued(featureId: string, project: string): void {
  const subtitle = `Project: ${project}`;
  notifyWithSound(
    `Review Enabled: ${featureId}`,
    "Auto-review will run when all tasks complete.",
    "Pop",
    subtitle,
  );
}
