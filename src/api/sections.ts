/**
 * Brain API - Section Endpoints
 *
 * REST API endpoints for extracting sections from markdown entries.
 * Provides brain_plan_sections and brain_section functionality.
 * Uses OpenAPIHono with Zod schemas for automatic validation and documentation.
 */

import { OpenAPIHono, createRoute, z } from "@hono/zod-openapi";
import { getBrainService } from "../core/brain-service";
import {
  SectionsResponseSchema,
  SectionContentSchema,
  ErrorResponseSchema,
  NotFoundResponseSchema,
  EntryIdOrPathSchema,
} from "./schemas";

// =============================================================================
// Types
// =============================================================================

export interface Section {
  title: string;
  level: number;
  line: number;
}

export interface SectionContent {
  title: string;
  content: string;
  level: number;
  line: number;
}

// =============================================================================
// Section Parsing Logic
// =============================================================================

/**
 * Parse all section headers (h2, h3) from markdown content
 */
export function parseSections(content: string): Section[] {
  const lines = content.split("\n");
  const sections: Section[] = [];

  lines.forEach((line, index) => {
    const match = line.match(/^(#{2,3})\s+(.+)$/);
    if (match) {
      sections.push({
        title: match[2].trim(),
        level: match[1].length,
        line: index + 1, // 1-based line numbers
      });
    }
  });

  return sections;
}

/**
 * Extract a specific section's content by title
 *
 * @param content - Full markdown content
 * @param title - Section title to find (case-insensitive)
 * @param includeSubsections - Whether to include nested headers (default: true)
 * @returns Section content or null if not found
 */
export function extractSection(
  content: string,
  title: string,
  includeSubsections = true
): SectionContent | null {
  const lines = content.split("\n");
  let startLine = -1;
  let startLevel = 0;
  let endLine = lines.length;

  for (let i = 0; i < lines.length; i++) {
    const match = lines[i].match(/^(#{2,3})\s+(.+)$/);
    if (match) {
      const level = match[1].length;
      const sectionTitle = match[2].trim();

      if (startLine === -1 && sectionTitle.toLowerCase() === title.toLowerCase()) {
        startLine = i;
        startLevel = level;
      } else if (startLine !== -1) {
        // Found a subsequent header
        if (level <= startLevel) {
          // Same or higher level header - end of section
          endLine = i;
          break;
        } else if (!includeSubsections) {
          // Lower level header but we don't want subsections
          endLine = i;
          break;
        }
        // Otherwise, continue (include subsection)
      }
    }
  }

  if (startLine === -1) {
    return null;
  }

  // Trim trailing empty lines
  while (endLine > startLine && lines[endLine - 1].trim() === "") {
    endLine--;
  }

  return {
    title: title,
    content: lines.slice(startLine, endLine).join("\n"),
    level: startLevel,
    line: startLine + 1, // 1-based line numbers
  };
}

// =============================================================================
// Helper Functions
// =============================================================================

/**
 * Resolve entry path from ID or path string
 */
async function resolveEntryPath(id: string): Promise<string | null> {
  const service = getBrainService();

  // Check if it's an 8-character ID
  if (/^[a-z0-9]{8}$/.test(id)) {
    try {
      const entry = await service.recall(id);
      return entry.path;
    } catch {
      return null;
    }
  }

  // Otherwise treat as path
  return id;
}

/**
 * Extract ID from request path by removing the prefix and suffix
 * Path format: /api/v1/entries/{id}/sections or /api/v1/entries/{id}/sections/{title}
 */
function extractIdFromPath(path: string, suffix: string): string {
  // Remove the /api/v1/entries/ prefix and the suffix
  const entriesPrefix = "/api/v1/entries/";
  let cleanPath = path;

  if (cleanPath.startsWith(entriesPrefix)) {
    cleanPath = cleanPath.slice(entriesPrefix.length);
  } else if (cleanPath.startsWith("/")) {
    cleanPath = cleanPath.slice(1);
  }

  // Remove the suffix
  const suffixIndex = cleanPath.lastIndexOf(suffix);
  if (suffixIndex !== -1) {
    cleanPath = cleanPath.slice(0, suffixIndex);
  }

  return cleanPath;
}

/**
 * Route patterns to match paths with 1-6 segments before /sections
 */
const SECTIONS_PATTERNS = [
  "/*/sections",
  "/*/*/sections",
  "/*/*/*/sections",
  "/*/*/*/*/sections",
  "/*/*/*/*/*/sections",
  "/*/*/*/*/*/*/sections",
];

/**
 * Route patterns to match paths with 1-6 segments before /sections/:title
 * The title is captured as the last segment after /sections/
 */
const SECTION_BY_TITLE_PATTERNS = [
  "/*/sections/*",
  "/*/*/sections/*",
  "/*/*/*/sections/*",
  "/*/*/*/*/sections/*",
  "/*/*/*/*/*/sections/*",
  "/*/*/*/*/*/*/sections/*",
];

// =============================================================================
// OpenAPI Route Definitions (for documentation)
// =============================================================================

// GET /{id}/sections - List all section headers
const listSectionsRoute = createRoute({
  method: "get",
  path: "/{id}/sections",
  tags: ["Sections"],
  summary: "List section headers",
  description: "Lists all h2 and h3 section headers from a markdown entry.",
  request: {
    params: z.object({
      id: EntryIdOrPathSchema.openapi({
        param: { name: "id", in: "path" },
        description: "Entry ID (8-char alphanumeric) or full path",
      }),
    }),
  },
  responses: {
    200: {
      content: { "application/json": { schema: SectionsResponseSchema } },
      description: "List of section headers",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Entry not found",
    },
  },
});

// GET /{id}/sections/{title} - Get specific section content
const getSectionRoute = createRoute({
  method: "get",
  path: "/{id}/sections/{title}",
  tags: ["Sections"],
  summary: "Get section content",
  description: "Retrieves the full content of a specific section by title (case-insensitive).",
  request: {
    params: z.object({
      id: EntryIdOrPathSchema.openapi({
        param: { name: "id", in: "path" },
        description: "Entry ID (8-char alphanumeric) or full path",
      }),
      title: z.string().min(1).openapi({
        param: { name: "title", in: "path" },
        description: "Section title to find (case-insensitive)",
        example: "Implementation Details",
      }),
    }),
    query: z.object({
      includeSubsections: z
        .string()
        .optional()
        .transform((v) => v !== "false")
        .openapi({
          description: "Whether to include nested subsections (default: true)",
          example: "true",
        }),
    }),
  },
  responses: {
    200: {
      content: { "application/json": { schema: SectionContentSchema } },
      description: "Section content",
    },
    400: {
      content: { "application/json": { schema: ErrorResponseSchema } },
      description: "Invalid path format",
    },
    404: {
      content: { "application/json": { schema: NotFoundResponseSchema } },
      description: "Entry or section not found",
    },
  },
});

// =============================================================================
// Section Routes
// =============================================================================

export function createSectionRoutes(): OpenAPIHono {
  const sections = new OpenAPIHono({
    defaultHook: (result, c) => {
      if (!result.success) {
        const errors = result.error.issues.map((issue) => ({
          field: issue.path.join("."),
          message: issue.message,
        }));

        const fieldNames = errors.map((e) => e.field).filter(Boolean);
        const message = fieldNames.length > 0
          ? `Invalid request: ${fieldNames.join(", ")}`
          : "Invalid request";

        return c.json(
          {
            error: "Validation Error",
            message,
            details: errors,
          },
          400
        );
      }
    },
  });

  // Register OpenAPI routes for documentation
  // These use simpler paths for OpenAPI spec generation
  sections.openapi(listSectionsRoute, async (c) => {
    // This handler won't be called - actual handling is done by wildcard routes below
    // But we need to satisfy the OpenAPI type requirements
    const { id } = c.req.valid("param");
    return await handleListSections(c, id);
  });

  sections.openapi(getSectionRoute, async (c) => {
    // This handler won't be called - actual handling is done by wildcard routes below
    const { id, title } = c.req.valid("param");
    const query = c.req.valid("query");
    return await handleGetSection(c, id, title, query.includeSubsections);
  });

  // Actual wildcard route handlers for multi-segment paths
  // These handle the actual requests since paths like /projects/foo/plan/bar.md/sections
  // need multiple wildcard segments

  // GET /entries/:id/sections - List all section headers
  sections.on("GET", SECTIONS_PATTERNS, async (c) => {
    const id = extractIdFromPath(c.req.path, "/sections");
    return await handleListSections(c, id);
  });

  // GET /entries/:id/sections/:title - Get specific section content
  sections.on("GET", SECTION_BY_TITLE_PATTERNS, async (c) => {
    const fullPath = c.req.path;

    // Extract the entry ID and section title from the path
    // Path format: /api/v1/entries/{id}/sections/{title}
    const sectionsIndex = fullPath.lastIndexOf("/sections/");
    if (sectionsIndex === -1) {
      return c.json(
        {
          error: "Bad Request",
          message: "Invalid path format",
        },
        400
      );
    }

    // Remove the /api/v1/entries/ prefix
    const entriesPrefix = "/api/v1/entries/";
    let pathWithoutPrefix = fullPath;
    if (fullPath.startsWith(entriesPrefix)) {
      pathWithoutPrefix = fullPath.slice(entriesPrefix.length);
    }

    // Now extract id and title from the cleaned path
    const cleanSectionsIndex = pathWithoutPrefix.lastIndexOf("/sections/");
    const id = pathWithoutPrefix.slice(0, cleanSectionsIndex);
    const encodedTitle = pathWithoutPrefix.slice(cleanSectionsIndex + "/sections/".length);
    const title = decodeURIComponent(encodedTitle);

    // Get query parameter
    const includeSubsectionsParam = c.req.query("includeSubsections");
    const includeSubsections = includeSubsectionsParam !== "false"; // Default true

    return await handleGetSection(c, id, title, includeSubsections);
  });

  return sections;
}

// =============================================================================
// Shared Handler Functions
// =============================================================================

async function handleListSections(c: any, id: string) {
  try {
    const entryPath = await resolveEntryPath(id);

    if (!entryPath) {
      return c.json(
        {
          error: "Not Found",
          message: `Entry not found: ${id}`,
        },
        404
      );
    }

    const service = getBrainService();

    // Try to recall the entry to get its content
    let entry;
    try {
      entry = await service.recall(entryPath);
    } catch (error) {
      if (error instanceof Error && error.message.includes("No entry found")) {
        return c.json(
          {
            error: "Not Found",
            message: `Entry not found: ${id}`,
          },
          404
        );
      }
      throw error;
    }

    const sectionList = parseSections(entry.content);

    return c.json({
      sections: sectionList,
      total: sectionList.length,
    });
  } catch (error) {
    if (error instanceof Error) {
      if (error.message.includes("No entry found")) {
        return c.json(
          {
            error: "Not Found",
            message: `Entry not found: ${id}`,
          },
          404
        );
      }
      if (error.message.includes("No exact match")) {
        return c.json(
          {
            error: "Not Found",
            message: error.message,
          },
          404
        );
      }
    }
    throw error;
  }
}

async function handleGetSection(
  c: any,
  id: string,
  title: string,
  includeSubsections: boolean
) {
  try {
    const entryPath = await resolveEntryPath(id);

    if (!entryPath) {
      return c.json(
        {
          error: "Not Found",
          message: `Entry not found: ${id}`,
        },
        404
      );
    }

    const service = getBrainService();

    // Try to recall the entry to get its content
    let entry;
    try {
      entry = await service.recall(entryPath);
    } catch (error) {
      if (error instanceof Error && error.message.includes("No entry found")) {
        return c.json(
          {
            error: "Not Found",
            message: `Entry not found: ${id}`,
          },
          404
        );
      }
      throw error;
    }

    const section = extractSection(entry.content, title, includeSubsections);

    if (!section) {
      return c.json(
        {
          error: "Not Found",
          message: `Section not found: ${title}`,
        },
        404
      );
    }

    return c.json(section);
  } catch (error) {
    if (error instanceof Error) {
      if (error.message.includes("No entry found")) {
        return c.json(
          {
            error: "Not Found",
            message: `Entry not found: ${id}`,
          },
          404
        );
      }
      if (error.message.includes("No exact match")) {
        return c.json(
          {
            error: "Not Found",
            message: error.message,
          },
          404
        );
      }
    }
    throw error;
  }
}
