export function filterEntriesByHiddenTypes<T extends { type: string }>(
  entries: T[],
  hiddenTypes: ReadonlySet<string>,
): T[] {
  if (hiddenTypes.size === 0) return entries;
  return entries.filter((entry) => !hiddenTypes.has(entry.type));
}

export function toggleHiddenEntryType(
  hiddenTypes: ReadonlySet<string>,
  type: string,
): Set<string> {
  const next = new Set(hiddenTypes);
  if (next.has(type)) next.delete(type);
  else next.add(type);
  return next;
}


export function serializeHiddenEntryTypes(hiddenTypes: ReadonlySet<string>): string {
  return JSON.stringify([...hiddenTypes].sort());
}

export function deserializeHiddenEntryTypes(value: string | null): Set<string> {
  if (!value) return new Set();
  try {
    const parsed = JSON.parse(value);
    if (!Array.isArray(parsed)) return new Set();
    return new Set(parsed.filter((type): type is string => typeof type === "string" && type !== ""));
  } catch {
    return new Set();
  }
}
