/**
 * Brain API - File Parser
 *
 * Parses markdown files with frontmatter into structured data.
 * Extracts links, computes checksums, and builds ParsedFile objects.
 */

import { readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { parseFrontmatter } from "./frontmatter";
import { extractIdFromPath } from "./zk-client";

// =============================================================================
// Types
// =============================================================================

export interface ExtractedLink {
  href: string; // raw link target (could be short_id, path, or URL)
  title: string; // link display text
  type: "markdown" | "wikilink" | "url";
  snippet: string; // surrounding context (±50 chars)
}

export interface ParsedFile {
  path: string;
  shortId: string; // extracted from filename (8-char alphanumeric)
  title: string; // from frontmatter
  lead: string; // first paragraph of body, stripped of markdown, truncated to 200 chars
  body: string; // content without frontmatter
  rawContent: string; // full file content
  wordCount: number;
  checksum: string; // SHA-256 of rawContent
  metadata: Record<string, any>; // all frontmatter fields
  type?: string;
  status?: string;
  priority?: string;
  projectId?: string;
  featureId?: string;
  tags: string[];
  created: string; // from frontmatter or file ctime
  modified: string; // file mtime
  links: ExtractedLink[]; // markdown links found in body
}

// =============================================================================
// Public Functions
// =============================================================================

/**
 * Parse a markdown file from disk into a ParsedFile structure.
 *
 * @param filePath - Relative path within the brain directory (e.g., "projects/test/task/abc12def.md")
 * @param brainDir - Absolute path to the brain root directory
 * @returns ParsedFile with all extracted data
 */
export function parseFile(filePath: string, brainDir: string): ParsedFile {
  const fullPath = join(brainDir, filePath);
  const rawContent = readFileSync(fullPath, "utf-8");
  const stat = statSync(fullPath);

  const { frontmatter, body } = parseFrontmatter(rawContent);
  const shortId = extractIdFromPath(filePath);
  const checksum = computeChecksum(rawContent);
  const links = extractLinks(body);
  const wordCount = countWords(body);
  const lead = extractLead(body);

  // Extract tags from frontmatter
  const tags: string[] = Array.isArray(frontmatter.tags)
    ? (frontmatter.tags as string[])
    : [];

  // Get timestamps
  const created =
    (frontmatter.created as string) || stat.birthtime.toISOString();
  const modified = stat.mtime.toISOString();

  return {
    path: filePath,
    shortId,
    title: (frontmatter.title as string) || shortId,
    lead,
    body,
    rawContent,
    wordCount,
    checksum,
    metadata: frontmatter as Record<string, any>,
    type: (frontmatter.type as string) || undefined,
    status: (frontmatter.status as string) || undefined,
    priority: (frontmatter.priority as string) || undefined,
    projectId: (frontmatter.projectId as string) || undefined,
    featureId: (frontmatter.feature_id as string) || undefined,
    tags,
    created,
    modified,
    links,
  };
}

/**
 * Extract markdown links from body text.
 *
 * Extracts standard markdown links [text](target) and classifies them:
 * - href starting with http/https → type: 'url'
 * - all others → type: 'markdown'
 *
 * Image links ![alt](src) are excluded.
 * Wikilinks are not extracted (reserved for future use).
 *
 * @param markdown - The markdown body text (without frontmatter)
 * @returns Array of extracted links with context snippets
 */
export function extractLinks(markdown: string): ExtractedLink[] {
  const links: ExtractedLink[] = [];

  // Match [text](target) but NOT ![alt](src)
  // Negative lookbehind (?<!!) ensures we skip image links.
  const linkRegex = /(?<!!)\[([^\]]*)\]\(([^)]+)\)/g;
  let match: RegExpExecArray | null;

  while ((match = linkRegex.exec(markdown)) !== null) {
    const title = match[1];
    const href = match[2];
    const matchStart = match.index;
    const matchEnd = matchStart + match[0].length;

    // Determine link type
    const type: ExtractedLink["type"] = /^https?:\/\//.test(href)
      ? "url"
      : "markdown";

    // Extract ±50 chars of surrounding context
    const snippetStart = Math.max(0, matchStart - 50);
    const snippetEnd = Math.min(markdown.length, matchEnd + 50);
    const snippet = markdown.slice(snippetStart, snippetEnd);

    links.push({ href, title, type, snippet });
  }

  return links;
}

/**
 * Compute SHA-256 checksum of content.
 *
 * @param content - The raw file content
 * @returns Hex-encoded SHA-256 hash
 */
export function computeChecksum(content: string): string {
  return new Bun.CryptoHasher("sha256").update(content).digest("hex");
}

// =============================================================================
// Internal Helpers
// =============================================================================

/**
 * Count words in body text.
 * Splits on whitespace and filters empty strings.
 */
function countWords(body: string): number {
  return body.split(/\s+/).filter((w) => w.length > 0).length;
}

/**
 * Extract lead text from body.
 *
 * Finds the first non-empty paragraph (block of text separated by blank lines),
 * strips markdown formatting, and truncates to 200 characters.
 */
function extractLead(body: string): string {
  // Split into paragraphs (separated by one or more blank lines)
  const paragraphs = body.split(/\n\s*\n/);

  // Find first non-empty paragraph
  const firstParagraph = paragraphs.find((p) => p.trim().length > 0);
  if (!firstParagraph) return "";

  // Strip markdown formatting
  let text = firstParagraph.trim();

  // Remove headings (# prefix)
  text = text.replace(/^#{1,6}\s+/gm, "");
  // Remove bold/italic markers
  text = text.replace(/\*{1,3}([^*]+)\*{1,3}/g, "$1");
  text = text.replace(/_{1,3}([^_]+)_{1,3}/g, "$1");
  // Remove inline code
  text = text.replace(/`([^`]+)`/g, "$1");
  // Remove images entirely: ![alt](src) → ""
  text = text.replace(/!\[([^\]]*)\]\([^)]+\)/g, "");
  // Remove links, keep text: [text](url) → text
  text = text.replace(/\[([^\]]*)\]\([^)]+\)/g, "$1");
  // Remove strikethrough
  text = text.replace(/~~([^~]+)~~/g, "$1");
  // Collapse whitespace
  text = text.replace(/\s+/g, " ").trim();

  // Truncate to 200 chars
  return text.slice(0, 200);
}
