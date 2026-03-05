/**
 * Project Filter
 *
 * Utility module for filtering projects based on glob patterns.
 * Used by the multi-project dashboard to resolve "all" to a filtered list.
 */

import { getRunnerConfig } from "./config";

// =============================================================================
// Types
// =============================================================================

export interface ProjectFilter {
  includes: string[]; // glob patterns (e.g., 'prod-*', 'team-*')
  excludes: string[]; // glob patterns (e.g., 'test-*', 'legacy-*')
}

// =============================================================================
// Glob Matching
// =============================================================================

/**
 * Match a project name against a glob pattern.
 * Supports: * (any chars), ? (single char)
 * Examples: 'test-*' matches 'test-api', 'test-web'
 */
export function matchesGlob(projectName: string, pattern: string): boolean {
  // Convert glob pattern to regex
  // Escape special regex chars except * and ?
  let regexPattern = pattern
    .replace(/[.+^${}()|[\]\\]/g, "\\$&") // Escape special chars
    .replace(/\*/g, ".*") // * matches any chars
    .replace(/\?/g, "."); // ? matches single char

  // Anchor the pattern to match the entire string
  regexPattern = `^${regexPattern}$`;

  try {
    const regex = new RegExp(regexPattern);
    return regex.test(projectName);
  } catch {
    // Invalid pattern, return false
    return false;
  }
}

// =============================================================================
// Project Filtering
// =============================================================================

/**
 * Filter a list of projects based on include/exclude patterns.
 * Logic:
 * 1. If includes are specified, only keep projects matching at least one include pattern
 * 2. Then remove any projects matching any exclude pattern
 * 3. Return sorted list
 */
export function filterProjects(
  projects: string[],
  filter: ProjectFilter
): string[] {
  let result = [...projects];

  // Step 1: Apply includes (if any specified)
  if (filter.includes.length > 0) {
    result = result.filter((project) =>
      filter.includes.some((pattern) => matchesGlob(project, pattern))
    );
  }

  // Step 2: Apply excludes
  if (filter.excludes.length > 0) {
    result = result.filter(
      (project) =>
        !filter.excludes.some((pattern) => matchesGlob(project, pattern))
    );
  }

  // Step 3: Sort alphabetically
  return result.sort((a, b) => a.localeCompare(b));
}

// =============================================================================
// API Integration
// =============================================================================

/**
 * Fetch all projects from the API and apply filters.
 * @param apiUrl - Brain API URL
 * @param filter - Include/exclude patterns
 * @returns Filtered, sorted list of project IDs
 */
export async function resolveProjects(
  apiUrl: string,
  filter: ProjectFilter
): Promise<string[]> {
  const config = getRunnerConfig();
  const url = `${apiUrl}/api/v1/tasks`;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), config.apiTimeout);

  try {
    const response = await fetch(url, {
      signal: controller.signal,
      headers: {
        Accept: "application/json",
      },
    });

    if (!response.ok) {
      throw new Error(`Failed to fetch projects: ${response.status}`);
    }

    const data = (await response.json()) as { projects: string[] };
    const projects = data.projects || [];

    return filterProjects(projects, filter);
  } catch (error) {
    if (error instanceof Error && error.name === "AbortError") {
      throw new Error(`Request timeout after ${config.apiTimeout}ms`);
    }
    throw error;
  } finally {
    clearTimeout(timeout);
  }
}
