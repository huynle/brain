/**
 * Brain API - Zod Schemas for OpenAPI
 *
 * Shared Zod schemas used across all API endpoints.
 * These schemas provide both runtime validation and OpenAPI documentation.
 */

import { z } from "@hono/zod-openapi";
import { ENTRY_TYPES, ENTRY_STATUSES, PRIORITIES, TASK_CLASSIFICATIONS } from "../core/types";

// =============================================================================
// Base Enums as Zod Schemas
// =============================================================================

export const EntryTypeSchema = z.enum(ENTRY_TYPES).openapi({
  description: "Type of brain entry",
  example: "plan",
});

export const EntryStatusSchema = z.enum(ENTRY_STATUSES).openapi({
  description: "Status of the entry",
  example: "active",
});

export const PrioritySchema = z.enum(PRIORITIES).openapi({
  description: "Priority level",
  example: "high",
});

export const TaskClassificationSchema = z.enum(TASK_CLASSIFICATIONS).openapi({
  description: "Task classification based on dependency resolution",
  example: "ready",
});

// =============================================================================
// Common Schemas
// =============================================================================

export const EntryIdSchema = z.string().regex(/^[a-z0-9]{8}$/).openapi({
  description: "8-character alphanumeric entry ID",
  example: "abc12def",
});

export const EntryIdOrPathSchema = z.string().min(1).openapi({
  description: "Entry ID (8-char alphanumeric) or full path",
  example: "abc12def",
});

export const ProjectIdSchema = z.string().regex(/^[a-zA-Z0-9_-]+$/).openapi({
  description: "Project identifier (alphanumeric, hyphens, underscores)",
  example: "my-project",
});

// =============================================================================
// Error Response Schema
// =============================================================================

export const ErrorResponseSchema = z.object({
  error: z.string().openapi({ example: "Validation Error" }),
  message: z.string().openapi({ example: "Invalid request body" }),
  details: z.array(z.object({
    field: z.string(),
    message: z.string(),
  })).optional(),
}).openapi("ErrorResponse");

export const NotFoundResponseSchema = z.object({
  error: z.string().openapi({ example: "Not Found" }),
  message: z.string().openapi({ example: "Entry not found" }),
}).openapi("NotFoundResponse");

export const ServiceUnavailableResponseSchema = z.object({
  error: z.string().openapi({ example: "Service Unavailable" }),
  message: z.string().openapi({ example: "zk CLI is required" }),
}).openapi("ServiceUnavailableResponse");

// =============================================================================
// Entry Schemas
// =============================================================================

export const BrainEntrySchema = z.object({
  id: EntryIdSchema,
  path: z.string().openapi({ example: "projects/my-project/plan/feature.md" }),
  title: z.string().openapi({ example: "Feature Implementation Plan" }),
  type: EntryTypeSchema,
  status: EntryStatusSchema,
  content: z.string().openapi({ example: "# Plan\n\nThis is the plan content..." }),
  tags: z.array(z.string()).openapi({ example: ["feature", "v2"] }),
  priority: PrioritySchema.optional(),
  depends_on: z.array(z.string()).optional().openapi({ description: "Array of entry IDs this depends on" }),
  project_id: z.string().optional(),
  created: z.string().optional().openapi({ example: "2024-01-15T10:30:00Z" }),
  modified: z.string().optional().openapi({ example: "2024-01-15T12:00:00Z" }),
  access_count: z.number().optional(),
  last_verified: z.string().optional(),
  workdir: z.string().optional().openapi({ description: "$HOME-relative path to main worktree" }),
  worktree: z.string().optional().openapi({ description: "Specific worktree if different from main" }),
  git_remote: z.string().optional().openapi({ description: "Git remote URL for verification" }),
  git_branch: z.string().optional().openapi({ description: "Branch context when entry was created" }),
  user_original_request: z.string().optional().openapi({ 
    description: "Verbatim user request for validation during task completion",
    example: "Add a dark mode toggle to the settings page" 
  }),
}).openapi("BrainEntry");

export const BrainEntrySummarySchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  type: EntryTypeSchema,
  status: EntryStatusSchema,
}).openapi("BrainEntrySummary");

export const BacklinkSchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  type: EntryTypeSchema,
}).openapi("Backlink");

// =============================================================================
// Create Entry Request/Response
// =============================================================================

export const CreateEntryRequestSchema = z.object({
  type: EntryTypeSchema,
  title: z.string().min(1).openapi({ example: "My New Entry" }),
  content: z.string().openapi({ example: "# Content\n\nEntry content here..." }),
  tags: z.array(z.string()).optional().openapi({ example: ["tag1", "tag2"] }),
  status: EntryStatusSchema.optional(),
  priority: PrioritySchema.optional(),
  depends_on: z.array(z.string()).optional(),
  global: z.boolean().optional().openapi({ description: "Create as global entry" }),
  project: z.string().optional().openapi({ description: "Project name" }),
  relatedEntries: z.array(z.string()).optional().openapi({ description: "Related entry paths/IDs to link" }),
  workdir: z.string().optional(),
  worktree: z.string().optional(),
  git_remote: z.string().optional(),
  git_branch: z.string().optional(),
  user_original_request: z.string().optional().openapi({
    description: "Verbatim user request for validation during task completion. Highly recommended for tasks to enable intent verification. Supports multiline content, code blocks, and special characters.",
    example: "Add a dark mode toggle to the settings page with the following requirements:\n- Toggle should persist across sessions\n- Use CSS variables for theming"
  }),
}).openapi("CreateEntryRequest");

export const CreateEntryResponseSchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  type: EntryTypeSchema,
  status: EntryStatusSchema,
  link: z.string().openapi({ example: "[My Entry](abc12def)" }),
}).openapi("CreateEntryResponse");

// =============================================================================
// Update Entry Request
// =============================================================================

export const UpdateEntryRequestSchema = z.object({
  status: EntryStatusSchema.optional(),
  title: z.string().optional(),
  append: z.string().optional().openapi({ description: "Content to append to the entry" }),
  note: z.string().optional().openapi({ description: "Note to add to the entry" }),
}).refine(
  (data) => data.status !== undefined || data.title !== undefined || data.append !== undefined || data.note !== undefined,
  { message: "At least one of status, title, append, or note must be provided" }
).openapi("UpdateEntryRequest");

// =============================================================================
// List Entries Request/Response
// =============================================================================

export const ListEntriesQuerySchema = z.object({
  type: EntryTypeSchema.optional(),
  status: EntryStatusSchema.optional(),
  filename: z.string().optional().openapi({ description: "Filter by filename pattern" }),
  limit: z.string().transform(Number).pipe(z.number().int().positive()).optional(),
  offset: z.string().transform(Number).pipe(z.number().int().nonnegative()).optional(),
  global: z.string().transform((v) => v === "true").optional(),
  sortBy: z.enum(["created", "modified", "priority"]).optional(),
}).openapi("ListEntriesQuery");

export const ListEntriesResponseSchema = z.object({
  entries: z.array(BrainEntrySchema),
  total: z.number(),
  limit: z.number(),
  offset: z.number(),
}).openapi("ListEntriesResponse");

// =============================================================================
// Get Entry Response (with backlinks)
// =============================================================================

export const GetEntryResponseSchema = BrainEntrySchema.extend({
  backlinks: z.array(BacklinkSchema),
}).openapi("GetEntryResponse");

// =============================================================================
// Delete Entry Response
// =============================================================================

export const DeleteEntryResponseSchema = z.object({
  message: z.string().openapi({ example: "Entry deleted successfully" }),
  path: z.string(),
}).openapi("DeleteEntryResponse");

// =============================================================================
// Search Schemas
// =============================================================================

export const SearchRequestSchema = z.object({
  query: z.string({ message: "query is required and must be a string" })
    .min(1, { message: "query cannot be empty" })
    .refine((val) => val.trim().length > 0, { message: "query cannot be empty" })
    .openapi({ example: "authentication" }),
  type: EntryTypeSchema.optional(),
  status: EntryStatusSchema.optional(),
  limit: z.number({ message: "limit must be a positive integer" })
    .int({ message: "limit must be a positive integer" })
    .positive({ message: "limit must be a positive integer" })
    .optional(),
  global: z.boolean({ message: "global must be a boolean" }).optional(),
}).openapi("SearchRequest");

export const SearchResultSchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  type: EntryTypeSchema,
  status: EntryStatusSchema,
  snippet: z.string().openapi({ description: "First 150 characters of content" }),
}).openapi("SearchResult");

export const SearchResponseSchema = z.object({
  results: z.array(SearchResultSchema),
  total: z.number(),
}).openapi("SearchResponse");

// =============================================================================
// Inject Schemas
// =============================================================================

export const InjectRequestSchema = z.object({
  query: z.string({ message: "query is required and must be a string" })
    .min(1, { message: "query cannot be empty" })
    .refine((val) => val.trim().length > 0, { message: "query cannot be empty" })
    .openapi({ example: "user authentication flow" }),
  maxEntries: z.number({ message: "maxEntries must be a positive integer" })
    .int({ message: "maxEntries must be a positive integer" })
    .positive({ message: "maxEntries must be a positive integer" })
    .optional(),
  type: EntryTypeSchema.optional(),
}).openapi("InjectRequest");

export const InjectResponseSchema = z.object({
  context: z.string().openapi({ description: "Formatted context text for AI consumption" }),
  entries: z.array(BrainEntrySummarySchema),
}).openapi("InjectResponse");

// =============================================================================
// Health & Stats Schemas
// =============================================================================

export const HealthResponseSchema = z.object({
  status: z.enum(["healthy", "degraded", "unhealthy"]),
  zkAvailable: z.boolean(),
  dbAvailable: z.boolean(),
  timestamp: z.string(),
  version: z.string().openapi({ example: "0.1.0" }),
}).openapi("HealthResponse");

export const StatsResponseSchema = z.object({
  zkAvailable: z.boolean(),
  zkVersion: z.string().nullable(),
  notebookExists: z.boolean(),
  brainDir: z.string(),
  dbPath: z.string(),
  totalEntries: z.number(),
  globalEntries: z.number(),
  projectEntries: z.number(),
  byType: z.record(z.string(), z.number()),
  orphanCount: z.number(),
  trackedEntries: z.number(),
  staleCount: z.number(),
}).openapi("StatsResponse");

export const OrphanEntrySchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  type: EntryTypeSchema,
  status: EntryStatusSchema,
  created: z.string().optional(),
}).openapi("OrphanEntry");

export const OrphansResponseSchema = z.object({
  entries: z.array(OrphanEntrySchema),
  total: z.number(),
  message: z.string(),
}).openapi("OrphansResponse");

export const StaleEntrySchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  type: EntryTypeSchema,
  status: EntryStatusSchema,
  daysSinceVerified: z.number().nullable(),
  lastVerified: z.string().nullable(),
}).openapi("StaleEntry");

export const StaleResponseSchema = z.object({
  entries: z.array(StaleEntrySchema),
  total: z.number(),
}).openapi("StaleResponse");

export const VerifyResponseSchema = z.object({
  message: z.string().openapi({ example: "Entry verified" }),
  path: z.string(),
  verifiedAt: z.string(),
}).openapi("VerifyResponse");

export const LinkRequestSchema = z.object({
  title: z.string().optional(),
  path: z.string().optional(),
  withTitle: z.boolean().optional(),
}).refine(
  (data) => data.title !== undefined || data.path !== undefined,
  { message: "Either title or path must be provided" }
).openapi("LinkRequest");

export const LinkResponseSchema = z.object({
  link: z.string().openapi({ example: "[My Entry](abc12def)" }),
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
}).openapi("LinkResponse");

// =============================================================================
// Graph Schemas
// =============================================================================

export const GraphEntrySchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  type: EntryTypeSchema,
}).openapi("GraphEntry");

export const GraphResponseSchema = z.object({
  entries: z.array(GraphEntrySchema),
  total: z.number(),
}).openapi("GraphResponse");

// =============================================================================
// Section Schemas
// =============================================================================

export const SectionSchema = z.object({
  title: z.string(),
  level: z.number().int().min(2).max(3),
  line: z.number().int().positive().openapi({ description: "1-based line number" }),
}).openapi("Section");

export const SectionsResponseSchema = z.object({
  sections: z.array(SectionSchema),
  total: z.number(),
}).openapi("SectionsResponse");

export const SectionContentSchema = z.object({
  title: z.string(),
  content: z.string(),
  level: z.number().int(),
  line: z.number().int(),
}).openapi("SectionContent");

// =============================================================================
// Task Schemas
// =============================================================================

export const TaskSchema = z.object({
  id: EntryIdSchema,
  path: z.string(),
  title: z.string(),
  priority: PrioritySchema,
  status: EntryStatusSchema,
  depends_on: z.array(z.string()),
  created: z.string(),
  workdir: z.string().nullable(),
  worktree: z.string().nullable(),
  git_remote: z.string().nullable(),
  git_branch: z.string().nullable(),
}).openapi("Task");

export const ResolvedTaskSchema = TaskSchema.extend({
  resolved_deps: z.array(EntryIdSchema).openapi({ description: "IDs of resolved dependencies" }),
  unresolved_deps: z.array(z.string()).openapi({ description: "References that couldn't be resolved" }),
  classification: TaskClassificationSchema,
  blocked_by: z.array(EntryIdSchema).openapi({ description: "IDs of blocking dependencies" }),
  blocked_by_reason: z.string().optional().openapi({ description: "Reason for being blocked" }),
  waiting_on: z.array(EntryIdSchema).openapi({ description: "IDs of incomplete dependencies" }),
  in_cycle: z.boolean(),
  resolved_workdir: z.string().nullable().openapi({ description: "Absolute path after resolution" }),
}).openapi("ResolvedTask");

export const TaskStatsSchema = z.object({
  total: z.number(),
  ready: z.number(),
  waiting: z.number(),
  blocked: z.number(),
  not_pending: z.number(),
}).openapi("TaskStats");

export const ProjectListResponseSchema = z.object({
  projects: z.array(z.string()),
  count: z.number(),
}).openapi("ProjectListResponse");

export const TaskListResponseSchema = z.object({
  tasks: z.array(ResolvedTaskSchema),
  count: z.number(),
  stats: TaskStatsSchema.optional(),
  cycles: z.array(z.array(z.string())).optional().openapi({ description: "Groups of task IDs in cycles" }),
}).openapi("TaskListResponse");

export const TaskNextResponseSchema = z.object({
  task: ResolvedTaskSchema.nullable(),
  message: z.string().optional(),
}).openapi("TaskNextResponse");

// =============================================================================
// Task Claiming Schemas
// =============================================================================

export const ClaimRequestSchema = z.object({
  runnerId: z.string().min(1).openapi({ example: "runner-001" }),
}).openapi("ClaimRequest");

export const ClaimResponseSchema = z.object({
  success: z.literal(true),
  taskId: z.string(),
  runnerId: z.string(),
  claimedAt: z.string().optional(),
}).openapi("ClaimResponse");

export const ClaimConflictResponseSchema = z.object({
  success: z.literal(false),
  error: z.literal("conflict"),
  message: z.string(),
  taskId: z.string(),
  claimedBy: z.string(),
  claimedAt: z.string(),
  isStale: z.boolean(),
}).openapi("ClaimConflictResponse");

export const ReleaseResponseSchema = z.object({
  success: z.boolean(),
  taskId: z.string().optional(),
  message: z.string().optional(),
}).openapi("ReleaseResponse");

export const ClaimStatusResponseSchema = z.object({
  claimed: z.boolean(),
  claimedBy: z.string().optional(),
  claimedAt: z.string().optional(),
  isStale: z.boolean().optional(),
}).openapi("ClaimStatusResponse");
