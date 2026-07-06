// Field-query filter syntax for "/" filters, k9s-style:
//
//   filter ::= term (ws term)*
//   term   ::= field ":" value | value
//   field  ::= status | feature | tag | priority | executor | project | id
//   value  ::= bare-token | '"' quoted '"'
//
// Semantics:
//   - plain terms AND together as case-insensitive substring matches over
//     title/id/feature/status/tags (mirrors the legacy filterTasks fields);
//   - repeated occurrences of the SAME field OR together, different fields
//     AND together;
//   - field values prefix-match ("status:comp" hits "completed");
//   - "status:ready" is a pseudo-value: pending/active with nothing to wait on;
//   - unknown "field:" prefixes degrade to plain substring terms so pasting
//     "http://host" into a filter never explodes.
//
// Pure module: no store/react/api imports (node:test-able). Tasks are matched
// structurally via TaskLike.

export const FILTER_FIELDS = [
  "status",
  "feature",
  "tag",
  "priority",
  "executor",
  "project",
  "id",
] as const;

export type FilterField = (typeof FILTER_FIELDS)[number];

export interface ParsedFilter {
  raw: string;
  /** Plain substring terms (lowercased). */
  text: string[];
  /** Field terms (values lowercased), same-field values OR together. */
  fields: Partial<Record<FilterField, string[]>>;
}

export interface TaskLike {
  id?: string;
  title?: string;
  status?: string;
  feature_id?: string;
  tags?: string[];
  priority?: string;
  executor?: string;
  projectId?: string;
  waiting_on?: string[];
  blocked_by?: string[];
}

function isFilterField(s: string): s is FilterField {
  return (FILTER_FIELDS as readonly string[]).includes(s);
}

/** Split on whitespace, honoring double-quoted values ("a b" and field:"a b"). */
export function tokenizeFilter(input: string): string[] {
  const tokens: string[] = [];
  let cur = "";
  let inQuote = false;
  for (const ch of input) {
    if (ch === '"') {
      inQuote = !inQuote;
      continue;
    }
    if (!inQuote && /\s/.test(ch)) {
      if (cur) tokens.push(cur);
      cur = "";
      continue;
    }
    cur += ch;
  }
  if (cur) tokens.push(cur);
  return tokens;
}

export function parseFilter(input: string): ParsedFilter {
  const parsed: ParsedFilter = { raw: input, text: [], fields: {} };
  for (const token of tokenizeFilter(input)) {
    const colon = token.indexOf(":");
    if (colon > 0) {
      const field = token.slice(0, colon).toLowerCase();
      const value = token.slice(colon + 1).toLowerCase();
      if (isFilterField(field) && value) {
        (parsed.fields[field] ??= []).push(value);
        continue;
      }
    }
    if (token) parsed.text.push(token.toLowerCase());
  }
  return parsed;
}

export function isEmptyFilter(f: ParsedFilter): boolean {
  return f.text.length === 0 && Object.keys(f.fields).length === 0;
}

function taskIsReady(t: TaskLike): boolean {
  const runnable = t.status === "pending" || t.status === "active";
  return runnable && !(t.waiting_on?.length || t.blocked_by?.length);
}

function fieldValues(t: TaskLike, field: FilterField): string[] {
  switch (field) {
    case "status":
      return t.status ? [t.status] : [];
    case "feature":
      return t.feature_id ? [t.feature_id] : [];
    case "tag":
      return t.tags ?? [];
    case "priority":
      return t.priority ? [t.priority] : [];
    case "executor":
      return t.executor ? [t.executor] : [];
    case "project":
      return t.projectId ? [t.projectId] : [];
    case "id":
      return t.id ? [t.id] : [];
  }
}

function matchesField(t: TaskLike, field: FilterField, wanted: string[]): boolean {
  // Values within one field OR together.
  return wanted.some((w) => {
    if (field === "status" && w === "ready") return taskIsReady(t);
    return fieldValues(t, field).some((v) => v.toLowerCase().startsWith(w));
  });
}

function matchesText(t: TaskLike, term: string): boolean {
  const hay = [t.title, t.id, t.feature_id, t.status, ...(t.tags ?? [])];
  return hay.some((h) => h && h.toLowerCase().includes(term));
}

export function matchTask(t: TaskLike, f: ParsedFilter): boolean {
  for (const [field, wanted] of Object.entries(f.fields) as [FilterField, string[]][]) {
    if (!matchesField(t, field, wanted)) return false;
  }
  for (const term of f.text) {
    if (!matchesText(t, term)) return false;
  }
  return true;
}

/** Short human-readable form for the context header chip. */
export function describeFilter(f: ParsedFilter): string {
  const parts: string[] = [];
  for (const [field, wanted] of Object.entries(f.fields)) {
    parts.push(`${field}:${(wanted as string[]).join("|")}`);
  }
  parts.push(...f.text);
  return parts.join(" ");
}
