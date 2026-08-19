/**
 * Line-level diff for the entry Compare view.
 *
 * Dependency-free LCS diff with common prefix/suffix trimming. Output
 * is a flat row list suitable for a unified rendering: unchanged rows
 * carry both line numbers, del/add rows carry one side's.
 *
 * The DP table is O(n·m); after trimming, inputs whose remaining
 * product exceeds `MAX_DP_CELLS` degrade to one replace block
 * (all deletions then all additions) instead of hanging the tab.
 * Entry bodies are typically a few hundred lines, so the fallback is
 * a safety valve, not the common path.
 */

export interface DiffRow {
  kind: "same" | "del" | "add";
  text: string;
  /** 1-based line number in the left/old text (same + del rows). */
  aLine?: number;
  /** 1-based line number in the right/new text (same + add rows). */
  bLine?: number;
}

export interface DiffStats {
  added: number;
  removed: number;
  unchanged: number;
}

const MAX_DP_CELLS = 4_000_000;

export function diffLines(aText: string, bText: string): DiffRow[] {
  const a = aText.split("\n");
  const b = bText.split("\n");

  // Trim common prefix.
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) {
    start++;
  }
  // Trim common suffix (not overlapping the prefix).
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
  }

  const rows: DiffRow[] = [];
  for (let i = 0; i < start; i++) {
    rows.push({ kind: "same", text: a[i], aLine: i + 1, bLine: i + 1 });
  }

  const midA = a.slice(start, endA);
  const midB = b.slice(start, endB);
  const mid =
    midA.length * midB.length > MAX_DP_CELLS
      ? replaceBlock(midA, midB)
      : lcsDiff(midA, midB);
  for (const r of mid) {
    rows.push({
      ...r,
      aLine: r.aLine === undefined ? undefined : r.aLine + start,
      bLine: r.bLine === undefined ? undefined : r.bLine + start,
    });
  }

  for (let i = 0; i < a.length - endA; i++) {
    rows.push({
      kind: "same",
      text: a[endA + i],
      aLine: endA + i + 1,
      bLine: endB + i + 1,
    });
  }
  return rows;
}

export function diffStats(rows: DiffRow[]): DiffStats {
  const s: DiffStats = { added: 0, removed: 0, unchanged: 0 };
  for (const r of rows) {
    if (r.kind === "add") s.added++;
    else if (r.kind === "del") s.removed++;
    else s.unchanged++;
  }
  return s;
}

/** Classic LCS-table diff over the (already trimmed) middle sections. */
function lcsDiff(a: string[], b: string[]): DiffRow[] {
  const n = a.length;
  const m = b.length;
  // lcs[i][j] = LCS length of a[i:] vs b[j:].
  const width = m + 1;
  const lcs = new Uint32Array((n + 1) * width);
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i * width + j] =
        a[i] === b[j]
          ? lcs[(i + 1) * width + j + 1] + 1
          : Math.max(lcs[(i + 1) * width + j], lcs[i * width + j + 1]);
    }
  }
  const rows: DiffRow[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      rows.push({ kind: "same", text: a[i], aLine: i + 1, bLine: j + 1 });
      i++;
      j++;
    } else if (lcs[(i + 1) * width + j] >= lcs[i * width + j + 1]) {
      rows.push({ kind: "del", text: a[i], aLine: i + 1 });
      i++;
    } else {
      rows.push({ kind: "add", text: b[j], bLine: j + 1 });
      j++;
    }
  }
  for (; i < n; i++) rows.push({ kind: "del", text: a[i], aLine: i + 1 });
  for (; j < m; j++) rows.push({ kind: "add", text: b[j], bLine: j + 1 });
  return rows;
}

/** Oversize fallback: one deletion block followed by one addition block. */
function replaceBlock(a: string[], b: string[]): DiffRow[] {
  const rows: DiffRow[] = [];
  a.forEach((text, i) => rows.push({ kind: "del", text, aLine: i + 1 }));
  b.forEach((text, j) => rows.push({ kind: "add", text, bLine: j + 1 }));
  return rows;
}
