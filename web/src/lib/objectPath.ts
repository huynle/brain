/**
 * Get/set nested object values by dotted path, e.g. "server.port"
 * or "runner.opencode.bin".
 *
 * Both operate on plain-object trees (not classes). Setters return a
 * new tree; original inputs are never mutated so React can diff
 * cleanly.
 *
 * Exported as pure functions so they're unit-testable without React.
 */

export function getByPath(obj: unknown, path: string): unknown {
  if (obj === null || obj === undefined) return undefined;
  const parts = path.split(".");
  let cur: unknown = obj;
  for (const p of parts) {
    if (cur === null || cur === undefined) return undefined;
    if (typeof cur !== "object") return undefined;
    cur = (cur as Record<string, unknown>)[p];
  }
  return cur;
}

export function setByPath<T>(obj: T, path: string, value: unknown): T {
  const parts = path.split(".");
  if (parts.length === 0) return obj;

  // Non-destructive clone: only clone the ancestors on the path.
  const rootClone = { ...(obj as unknown as Record<string, unknown>) };
  let cur: Record<string, unknown> = rootClone;
  for (let i = 0; i < parts.length - 1; i++) {
    const key = parts[i];
    const child = cur[key];
    const clone =
      child !== null && typeof child === "object" && !Array.isArray(child)
        ? { ...(child as Record<string, unknown>) }
        : {};
    cur[key] = clone;
    cur = clone;
  }
  cur[parts[parts.length - 1]] = value;
  return rootClone as unknown as T;
}

/**
 * Deep structural equality suitable for a shallow config diff. We
 * intentionally don't handle Map/Set/Date because config values are
 * always plain JSON primitives / objects / arrays. undefined and
 * null are considered equal (server yaml sometimes emits either for
 * empty pointer fields).
 */
export function deepEqual(a: unknown, b: unknown): boolean {
  if (a === b) return true;
  if (a === null || a === undefined) return b === null || b === undefined;
  if (b === null || b === undefined) return false;
  if (typeof a !== typeof b) return false;
  if (typeof a !== "object") return a === b;
  if (Array.isArray(a) !== Array.isArray(b)) return false;
  if (Array.isArray(a) && Array.isArray(b)) {
    if (a.length !== b.length) return false;
    for (let i = 0; i < a.length; i++) {
      if (!deepEqual(a[i], b[i])) return false;
    }
    return true;
  }
  const ao = a as Record<string, unknown>;
  const bo = b as Record<string, unknown>;
  const keys = new Set([...Object.keys(ao), ...Object.keys(bo)]);
  for (const k of keys) {
    if (!deepEqual(ao[k], bo[k])) return false;
  }
  return true;
}
