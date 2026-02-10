/**
 * Brain Plugin - Shared API Client
 *
 * Platform-agnostic API client for the Brain API.
 * This module is designed to be embedded into target-specific plugins.
 */

import { execSync } from "child_process";
import { homedir } from "os";
import type {
  ApiError,
  ExecutionContext,
  SaveResponse,
  RecallResponse,
  SearchResponse,
  ListResponse,
  InjectResponse,
  GraphResponse,
  StaleResponse,
  UpdateResponse,
  StatsResponse,
  LinkResponse,
  SectionResponse,
  SectionsResponse,
} from "./types";

// ============================================================================
// Configuration
// ============================================================================

const DEFAULT_API_URL = "http://localhost:3333";

export function getApiUrl(): string {
  return process.env.BRAIN_API_URL || DEFAULT_API_URL;
}

// ============================================================================
// Execution Context
// ============================================================================

export function getExecutionContext(directory: string): ExecutionContext {
  const home = homedir();

  // Get main repo path (resolves worktrees to their main repo)
  let mainRepoPath = directory;
  let gitRemote: string | undefined;
  let gitBranch: string | undefined;

  try {
    // Check if we're in a worktree and get the main repo path
    const worktreeList = execSync("git worktree list --porcelain", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();

    const lines = worktreeList.split("\n");
    const firstWorktreeLine = lines.find((l) => l.startsWith("worktree "));
    if (firstWorktreeLine) {
      mainRepoPath = firstWorktreeLine.replace("worktree ", "");
    }

    // Get git remote
    gitRemote = execSync("git remote get-url origin", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();

    // Get current branch (used to derive worktree path later)
    gitBranch = execSync("git branch --show-current", {
      cwd: directory,
      encoding: "utf-8",
    }).trim();
  } catch {
    // Not a git repo or git not available
  }

  // Make paths relative to $HOME
  const makeHomeRelative = (path: string): string => {
    if (path.startsWith(home)) {
      return path.slice(home.length + 1); // +1 for the slash
    }
    return path;
  };

  const projectId = makeHomeRelative(mainRepoPath);
  const workdir = makeHomeRelative(mainRepoPath);

  return {
    projectId,
    workdir,
    gitRemote,
    gitBranch,
  };
}

// ============================================================================
// API Client
// ============================================================================

export async function apiRequest<T>(
  method: string,
  path: string,
  body?: unknown,
  queryParams?: Record<string, string | number | boolean | undefined>
): Promise<T> {
  const baseUrl = getApiUrl();
  let url = `${baseUrl}/api/v1${path}`;

  // Add query parameters
  if (queryParams) {
    const params = new URLSearchParams();
    for (const [key, value] of Object.entries(queryParams)) {
      if (value !== undefined) {
        params.append(key, String(value));
      }
    }
    const queryString = params.toString();
    if (queryString) {
      url += `?${queryString}`;
    }
  }

  const options: RequestInit = {
    method,
    headers: {
      "Content-Type": "application/json",
    },
  };

  if (body && (method === "POST" || method === "PATCH" || method === "PUT")) {
    options.body = JSON.stringify(body);
  }

  const response = await fetch(url, options);

  if (!response.ok) {
    let errorData: ApiError;
    try {
      errorData = await response.json();
    } catch {
      errorData = {
        error: "API Error",
        message: `HTTP ${response.status}: ${response.statusText}`,
      };
    }
    throw new Error(errorData.message || `API error: ${response.status}`);
  }

  return response.json();
}

// ============================================================================
// API Methods
// ============================================================================

export const brainApi = {
  // Entries
  async save(data: {
    type: string;
    title: string;
    content: string;
    tags?: string[];
    status?: string;
    priority?: string;
    depends_on?: string[];
    global?: boolean;
    project?: string;
    relatedEntries?: string[];

    workdir?: string;
    git_remote?: string;
    git_branch?: string;
  }): Promise<SaveResponse> {
    return apiRequest<SaveResponse>("POST", "/entries", data);
  },

  async recall(path: string): Promise<RecallResponse> {
    return apiRequest<RecallResponse>("GET", `/entries/${path}`);
  },

  async search(data: {
    query: string;
    type?: string;
    status?: string;
    limit?: number;
    global?: boolean;
  }): Promise<SearchResponse> {
    return apiRequest<SearchResponse>("POST", "/search", data);
  },

  async list(params: {
    type?: string;
    status?: string;
    filename?: string;
    limit?: number;
    global?: boolean;
    sortBy?: string;
  }): Promise<ListResponse> {
    return apiRequest<ListResponse>("GET", "/entries", undefined, params);
  },

  async inject(data: {
    query: string;
    maxEntries?: number;
    type?: string;
  }): Promise<InjectResponse> {
    return apiRequest<InjectResponse>("POST", "/inject", data);
  },

  async backlinks(path: string): Promise<GraphResponse> {
    return apiRequest<GraphResponse>("GET", `/entries/${path}/backlinks`);
  },

  async outlinks(path: string): Promise<GraphResponse> {
    return apiRequest<GraphResponse>("GET", `/entries/${path}/outlinks`);
  },

  async related(path: string, limit?: number): Promise<GraphResponse> {
    return apiRequest<GraphResponse>("GET", `/entries/${path}/related`, undefined, { limit });
  },

  async orphans(params: { type?: string; limit?: number }): Promise<GraphResponse> {
    return apiRequest<GraphResponse>("GET", "/orphans", undefined, params);
  },

  async stale(params: { days?: number; type?: string; limit?: number }): Promise<StaleResponse> {
    return apiRequest<StaleResponse>("GET", "/stale", undefined, params);
  },

  async verify(path: string): Promise<{ message: string; path: string }> {
    return apiRequest<{ message: string; path: string }>("POST", `/entries/${path}/verify`);
  },

  async update(
    path: string,
    data: {
      status?: string;
      title?: string;
      append?: string;
      note?: string;
    }
  ): Promise<UpdateResponse> {
    return apiRequest<UpdateResponse>("PATCH", `/entries/${path}`, data);
  },

  async stats(params: { global?: boolean }): Promise<StatsResponse> {
    return apiRequest<StatsResponse>("GET", "/stats", undefined, params);
  },

  async delete(path: string): Promise<{ message: string; path: string }> {
    return apiRequest<{ message: string; path: string }>("DELETE", `/entries/${path}`, undefined, {
      confirm: "true",
    });
  },

  async link(data: {
    title?: string;
    path?: string;
    withTitle?: boolean;
  }): Promise<LinkResponse> {
    return apiRequest<LinkResponse>("POST", "/link", data);
  },

  async section(
    planId: string,
    sectionTitle: string,
    includeSubsections?: boolean
  ): Promise<SectionResponse> {
    const encodedTitle = encodeURIComponent(sectionTitle);
    return apiRequest<SectionResponse>(
      "GET",
      `/entries/${planId}/sections/${encodedTitle}`,
      undefined,
      { includeSubsections: includeSubsections !== false ? "true" : "false" }
    );
  },

  async sections(path: string): Promise<SectionsResponse> {
    return apiRequest<SectionsResponse>("GET", `/entries/${path}/sections`);
  },
};
