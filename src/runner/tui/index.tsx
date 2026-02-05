#!/usr/bin/env bun
/**
 * Main entry point for the Ink TUI dashboard
 * 
 * Usage: bun run src/runner/tui/index.tsx [options]
 * 
 * Options:
 *   --api-url <url>       Brain API URL (default: http://localhost:3000)
 *   --project <name>      Project name (default: default)
 *   --poll-interval <ms>  Polling interval in ms (default: 5000)
 * 
 * Programmatic usage:
 *   import { startDashboard } from './tui';
 *   const dashboard = startDashboard({ projectId: 'my-project', apiUrl: 'http://localhost:3000' });
 *   // Later: dashboard.unmount();
 */

import React from 'react';
import { render } from 'ink';
import { App } from './App';
import type { TUIConfig } from './types';
import type { LogEntry } from './types';

// =============================================================================
// Types for programmatic API
// =============================================================================

export interface DashboardOptions {
  /** Project ID to display tasks for (legacy single project) */
  projectId?: string;
  /** Multiple project IDs to display tasks for (Phase 2) */
  projects?: string[];
  /** Brain API URL (default: http://localhost:3000) */
  apiUrl?: string;
  /** Polling interval in ms (default: 5000) */
  pollInterval?: number;
  /** Maximum log entries to keep (default: 100) */
  maxLogs?: number;
  /** Callback when dashboard exits (q pressed or Ctrl+C) */
  onExit?: () => void;
  /** Callback to receive log additions from external sources */
  onLogCallback?: (addLog: (entry: Omit<LogEntry, 'timestamp'>) => void) => void;
  /** Callback to cancel a task by ID and path */
  onCancelTask?: (taskId: string, taskPath: string) => Promise<void>;
  /** Callback to pause a specific project */
  onPause?: (projectId: string) => void | Promise<void>;
  /** Callback to resume a specific project */
  onResume?: (projectId: string) => void | Promise<void>;
  /** Callback to pause all projects */
  onPauseAll?: () => void | Promise<void>;
  /** Callback to resume all projects */
  onResumeAll?: () => void | Promise<void>;
  /** Get current paused projects from TaskRunner */
  getPausedProjects?: () => string[];
}

export interface DashboardHandle {
  /** Unmount the TUI and clean up */
  unmount: () => void;
  /** Wait for the dashboard to exit */
  waitUntilExit: () => Promise<void>;
}

// =============================================================================
// Programmatic API for TaskRunner integration
// =============================================================================

/**
 * Start the Ink TUI dashboard programmatically.
 * 
 * This is the main integration point for TaskRunner to use instead of
 * the bash-based tmux dashboard.
 * 
 * @param options - Dashboard configuration
 * @returns Handle to control the dashboard
 * 
 * @example
 * ```typescript
 * const dashboard = startDashboard({
 *   projectId: 'my-project',
 *   apiUrl: 'http://localhost:3000',
 *   onExit: () => console.log('Dashboard closed'),
 *   onLogCallback: (addLog) => {
 *     // Store addLog for later use
 *     myLogger.setTuiCallback(addLog);
 *   }
 * });
 * 
 * // Later, when shutting down:
 * dashboard.unmount();
 * ```
 */
export function startDashboard(options: DashboardOptions): DashboardHandle {
  // Resolve projects: prefer projects array, fall back to single projectId
  const projects = options.projects ?? (options.projectId ? [options.projectId] : ['default']);
  const isMultiProject = projects.length > 1;
  
  const config: TUIConfig = {
    apiUrl: options.apiUrl ?? 'http://localhost:3000',
    project: projects[0], // Legacy single project (first project for backward compatibility)
    projects: projects,   // Full project list
    activeProject: isMultiProject ? 'all' : projects[0], // Default to 'all' in multi-project mode
    pollInterval: options.pollInterval ?? 5000,
    maxLogs: options.maxLogs ?? 100,
  };

  // Enter alternate screen buffer and clear it (like vim/less do)
  // This gives us a clean fullscreen canvas that restores the previous
  // terminal content when we exit
  process.stdout.write('\x1b[?1049h'); // Enter alternate screen buffer
  process.stdout.write('\x1b[H');      // Move cursor to top-left
  process.stdout.write('\x1b[2J');     // Clear entire screen

  // Render the Ink app
  const { unmount: inkUnmount, waitUntilExit } = render(
    <App 
      config={config} 
      onLogCallback={options.onLogCallback}
      onCancelTask={options.onCancelTask}
      onPause={options.onPause}
      onResume={options.onResume}
      onPauseAll={options.onPauseAll}
      onResumeAll={options.onResumeAll}
      getPausedProjects={options.getPausedProjects}
    />,
    {
      // Patch console to prevent any stray console.log from corrupting the TUI
      patchConsole: true,
    }
  );

  // Wrap unmount to restore normal screen buffer
  const unmount = () => {
    inkUnmount();
    // Exit alternate screen buffer (restores previous terminal content)
    process.stdout.write('\x1b[?1049l');
  };

  // Set up exit callback
  if (options.onExit) {
    waitUntilExit().then(() => {
      // Restore normal screen buffer on exit
      process.stdout.write('\x1b[?1049l');
      options.onExit!();
    });
  }

  return { unmount, waitUntilExit };
}

// =============================================================================
// CLI Entry Point
// =============================================================================

// Parse command line arguments
function parseArgs(): TUIConfig {
  const args = process.argv.slice(2);
  const config: TUIConfig = {
    apiUrl: 'http://localhost:3000',
    project: 'default',
    pollInterval: 5000,
    maxLogs: 100,
  };

  for (let i = 0; i < args.length; i++) {
    switch (args[i]) {
      case '--api-url':
        config.apiUrl = args[++i] || config.apiUrl;
        break;
      case '--project':
        config.project = args[++i] || config.project;
        break;
      case '--poll-interval':
        config.pollInterval = parseInt(args[++i], 10) || config.pollInterval;
        break;
      case '--max-logs':
        config.maxLogs = parseInt(args[++i], 10) || config.maxLogs;
        break;
      case '--help':
      case '-h':
        console.log(`
Brain Task Runner TUI

Usage: bun run src/runner/tui/index.tsx [options]

Options:
  --api-url <url>       Brain API URL (default: http://localhost:3000)
  --project <name>      Project name (default: default)
  --poll-interval <ms>  Polling interval in ms (default: 5000)
  --max-logs <n>        Maximum log entries to keep (default: 100)
  --help, -h            Show this help message
`);
        process.exit(0);
    }
  }

  return config;
}

// Main entry point for CLI usage
function main() {
  const config = parseArgs();

  console.log('Starting Brain Task Runner TUI...');
  console.log(`API URL: ${config.apiUrl}`);
  console.log(`Project: ${config.project}`);
  console.log(`Poll Interval: ${config.pollInterval}ms`);
  console.log('');

  // Render the Ink app
  const { waitUntilExit } = render(<App config={config} />);

  // Wait for the app to exit
  waitUntilExit().then(() => {
    console.log('Goodbye!');
    process.exit(0);
  });
}

// Run if executed directly
if (import.meta.main) {
  main();
}

// Export for testing and programmatic use
export { App, startDashboard as default };
export type { TUIConfig, LogEntry };
