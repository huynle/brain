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
