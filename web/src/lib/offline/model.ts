import { parse, stringify } from "yaml";
import type { BrainEntry } from "../types";

export type CachedEntry = BrainEntry & {
  revision: string;
  raw: string;
  local_revision?: string;
};
export interface Mutation {
  id: string;
  path: string;
  method: "PATCH" | "POST";
  revision: string;
  baseLocalID?: string;
  body?: Record<string, unknown>;
  raw?: string;
  draft: CachedEntry;
  sent?: boolean;
  error?: string;
  failure?: "conflict" | "rejected" | "uncertain";
}
export interface ChangePage {
  epoch: string;
  cursor: number;
  more: boolean;
  changes: {
    path: string;
    deleted?: boolean;
    entry?: BrainEntry;
    raw?: string;
  }[];
}
export interface SyncState {
  cacheMode?: "recent" | "full";
  cachedCount?: number;
  epoch: string;
  cursor: number;
  ready: boolean;
  pending: Mutation[];
}
export function rawParts(raw: string): {
  fields: Record<string, unknown>;
  content: string;
} {
  const match = raw.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)([\s\S]*)$/);
  if (!match)
    throw new Error(
      "Full-file edits require YAML frontmatter between --- lines.",
    );
  const fields: unknown = parse(match[1]);
  if (!fields || typeof fields !== "object" || Array.isArray(fields))
    throw new Error("Frontmatter must be a mapping.");
  return { fields: fields as Record<string, unknown>, content: match[2] };
}
export function makeDraft(
  base: CachedEntry,
  body?: Record<string, unknown>,
  raw?: string,
): CachedEntry {
  let fields: Record<string, unknown>;
  let content: string;
  if (raw !== undefined) ({ fields, content } = rawParts(raw));
  else {
    const previous = rawParts(base.raw);
    const patch = body ?? {};
    fields = { ...previous.fields, ...patch };
    delete fields.content;
    delete fields.append;
    delete fields.expected_revision;
    content =
      typeof patch.content === "string" ? patch.content : previous.content;
    if (typeof patch.append === "string") content += "\n\n" + patch.append;
    raw = "---\n" + stringify(fields) + "---\n" + content;
  }
  return {
    ...base,
    ...fields,
    content,
    raw,
    path: base.path,
    id: base.id,
    revision: base.revision,
  } as CachedEntry;
}
export function matches(e: CachedEntry, q: Record<string, unknown>): boolean {
  if (
    (q.type && e.type !== q.type) ||
    (q.status && e.status !== q.status) ||
    (q.priority && e.priority !== q.priority)
  )
    return false;
  if (q.feature_id && e.feature_id !== q.feature_id) return false;
  const projects = Array.isArray(q.projects)
    ? q.projects
    : typeof q.projects === "string"
      ? q.projects.split(",")
      : [];
  if (projects.length) {
    if (
      !projects.includes(e.project_id) &&
      !(projects.includes("global") && e.path.startsWith("global/"))
    )
      return false;
  } else {
    if (q.project && e.project_id !== q.project) return false;
    if (
      (q.global === true || q.global === "true") &&
      !e.path.startsWith("global/")
    )
      return false;
  }
  const tags = Array.isArray(q.tags)
    ? q.tags
    : typeof q.tags === "string"
      ? q.tags.split(",")
      : [];
  return tags.every((t) => e.tags?.includes(t));
}
