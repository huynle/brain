// Shared fuzzy subsequence matching. Extracted from ProjectSheet so the
// command bar, project picker, and any future palette rank candidates the
// same way.

/**
 * fuzzyScore scores `query` against `text` as a subsequence match, or returns
 * null if it doesn't match at all. Higher is better: contiguous runs,
 * word-boundary hits, and prefix matches score more, shorter names break ties.
 */
export function fuzzyScore(text: string, query: string): number | null {
  if (!query) return 0;
  const t = text.toLowerCase();
  const q = query.toLowerCase();
  let ti = 0;
  let score = 0;
  let run = 0;
  let firstIdx = -1;
  for (const ch of q) {
    const idx = t.indexOf(ch, ti);
    if (idx === -1) return null;
    if (firstIdx === -1) firstIdx = idx;
    if (idx === ti) {
      run += 1;
      score += 3 + run; // reward contiguous matches
    } else {
      run = 0;
      score += 1;
      const prev = t[idx - 1];
      if (prev === "-" || prev === "_" || prev === "/" || prev === " ") score += 2; // word boundary
    }
    ti = idx + 1;
  }
  if (firstIdx === 0) score += 5; // prefix bonus
  score -= t.length * 0.05; // prefer shorter names
  return score;
}

export interface FuzzyMatch<T> {
  item: T;
  score: number;
}

/**
 * fuzzyBest ranks `items` against `query`, dropping non-matches. Stable for
 * equal scores (preserves input order), so deterministic suggestion lists.
 */
export function fuzzyBest<T>(items: readonly T[], key: (item: T) => string, query: string): FuzzyMatch<T>[] {
  const out: FuzzyMatch<T>[] = [];
  for (const item of items) {
    const score = fuzzyScore(key(item), query);
    if (score !== null) out.push({ item, score });
  }
  out.sort((a, b) => b.score - a.score);
  return out;
}

/**
 * fuzzyResolve picks a single unambiguous winner: exact (case-insensitive)
 * match wins outright; otherwise the top fuzzy match wins only when it beats
 * the runner-up by `epsilon` — ties return null so callers suggest instead of
 * guessing.
 */
export function fuzzyResolve<T>(
  items: readonly T[],
  key: (item: T) => string,
  query: string,
  epsilon = 0.5,
): T | null {
  const q = query.toLowerCase();
  for (const item of items) {
    if (key(item).toLowerCase() === q) return item;
  }
  const ranked = fuzzyBest(items, key, query);
  if (ranked.length === 0) return null;
  if (ranked.length === 1) return ranked[0].item;
  return ranked[0].score - ranked[1].score > epsilon ? ranked[0].item : null;
}
