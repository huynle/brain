/**
 * lib/actions/bulkBaton — finish a fan-out the server caps at 100.
 *
 * The bulk endpoints refuse to touch more than 100 entries per call and
 * report `truncated` + `matched_total` when a filter matched more. The old
 * client answer was a hard stop (TruncatedOperationError), which made any
 * feature past 100 tasks un-mutable from the UI. This loop repeats the
 * capped call until the server stops reporting truncation, aggregating the
 * page results.
 *
 * The loop only terminates if each page removes its entries from the match
 * set. That property is the CALLER's job:
 *
 *   - bulk-delete has it for free — deleted entries can't match again.
 *   - bulk-update does NOT with a bare feature filter: the server lists by
 *     `modified DESC`, so freshly-updated entries sort first and the same
 *     100 would be re-updated forever. Callers must pin the filter to a
 *     field the update changes (status), fanning out one baton per source
 *     status. See useFeatureActionContext.setStatusForAll.
 *
 * As a backstop against callers that get this wrong (or a server that
 * misreports), the loop also stops when a page makes no progress — a
 * truncated page where nothing succeeded cannot shrink the match set, so
 * repeating it would just burn 50 identical requests.
 */

/** The subset of BulkUpdateResponse / BulkDeleteResponse the loop reads. */
export interface BulkPage {
  failed: number;
  total: number;
  truncated?: boolean;
  matched_total?: number;
}

export interface BatonProgress {
  /** Entries processed so far across all pages (ok + failed). */
  processed: number;
  /** Best-known population size, from the latest page's matched_total. */
  matched: number;
  /** 1-based page count. */
  iteration: number;
}

export interface BatonOutcome {
  /** Aggregate successes across pages. */
  ok: number;
  /** Aggregate failures across pages. */
  failed: number;
  /** Aggregate entries attempted (ok + failed). */
  total: number;
  /** Pages executed. */
  iterations: number;
  /**
   * True when the loop stopped with the server still reporting truncation —
   * either the iteration cap was hit or a page made no progress. The
   * operation is incomplete and the summary must say so.
   */
  stopped: boolean;
}

/** Hard ceiling: 50 pages × 100 entries = 5 000 tasks per gesture. */
export const BULK_BATON_MAX_ITERATIONS = 50;

/**
 * Repeat one capped bulk call until the server stops reporting truncation.
 *
 * @param runPage one committed (non-dry-run) call, limit ≤ 100
 * @param okOf    extracts the success count (`updated` vs `deleted`)
 */
export async function runBulkBaton<T extends BulkPage>(
  runPage: (iteration: number) => Promise<T>,
  okOf: (page: T) => number,
  opts: {
    maxIterations?: number;
    onProgress?: (p: BatonProgress) => void;
  } = {},
): Promise<BatonOutcome> {
  const max = opts.maxIterations ?? BULK_BATON_MAX_ITERATIONS;
  let ok = 0;
  let failed = 0;
  let iterations = 0;

  for (;;) {
    if (iterations >= max) {
      return { ok, failed, total: ok + failed, iterations, stopped: true };
    }
    const page = await runPage(iterations);
    iterations++;
    const pageOk = okOf(page);
    ok += pageOk;
    failed += page.failed;

    opts.onProgress?.({
      processed: ok + failed,
      matched: page.matched_total ?? ok + failed,
      iteration: iterations,
    });

    if (!page.truncated) {
      return { ok, failed, total: ok + failed, iterations, stopped: false };
    }
    // No-progress backstop: a truncated page whose successes are zero
    // leaves the match set exactly as it was. See module docstring.
    if (pageOk === 0) {
      return { ok, failed, total: ok + failed, iterations, stopped: true };
    }
  }
}

/**
 * Toast copy for a finished baton. Distinct from summarizeBulkResult
 * because a baton has an extra failure mode — stopping early with work
 * remaining — that a single-page result cannot have.
 */
export function summarizeBatonOutcome(
  r: BatonOutcome,
  verb: "updated" | "deleted",
): { message: string; kind: "success" | "warning" | "error" } {
  const noun = r.ok === 1 ? "task" : "tasks";
  if (r.stopped) {
    return {
      message: `Stopped after ${r.ok} ${noun} ${verb} (${r.failed} failed) — more remain, run again to continue`,
      kind: "warning",
    };
  }
  if (r.failed === 0) {
    if (r.ok === 0) {
      return { message: `Nothing matched — no tasks ${verb}`, kind: "warning" };
    }
    return { message: `${r.ok} ${noun} ${verb}`, kind: "success" };
  }
  if (r.ok === 0) {
    return { message: `All ${r.failed} failed`, kind: "error" };
  }
  return {
    message: `${r.ok} of ${r.total} ${verb}; ${r.failed} failed`,
    kind: "warning",
  };
}
