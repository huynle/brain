// Vim count-prefix state machine, resolving the conflict between count
// prefixes (5j) and the 1-9 project-tab jump:
//
//   - a digit buffers and starts a ~600ms timer (the keystroke is swallowed);
//   - a following countable chord flushes the buffer as its count ("5j");
//   - a following digit extends the buffer ("12j"), restarting the timer;
//   - on timeout, or when the next chord is NOT countable: a single digit
//     1-9 replays as the project jump; multi-digit buffers just drop.
//
// Bare "0" is never a count starter (matches vim); it only extends an
// existing buffer ("10j").
//
// Pure module modeled on makeGgSequence (lib/paneNav.ts): real timers,
// configurable timeout, dispose() for unmount.

export interface CountMachineOpts {
  timeoutMs?: number;
  /** Fired when a lone buffered digit 1-9 turns out to be a project jump. */
  onReplayDigit: (digit: number) => void;
}

export interface CountMachine {
  /** Feed a bare digit keypress. Returns true when consumed (buffered). */
  feedDigit(d: string): boolean;
  /**
   * Resolve the pending buffer for the next non-digit chord. Returns the
   * count to dispatch with (>= 1). When the chord is not countable, a lone
   * 1-9 buffer replays as a project jump and the count is 1.
   */
  resolveForChord(countable: boolean): number;
  /** Current buffer ("" when idle) — for tests and debugging. */
  pending(): string;
  dispose(): void;
}

export function makeCountMachine(opts: CountMachineOpts): CountMachine {
  const timeoutMs = opts.timeoutMs ?? 600;
  let buffer = "";
  let timer: ReturnType<typeof setTimeout> | null = null;

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function expire() {
    clearTimer();
    const b = buffer;
    buffer = "";
    if (b.length === 1 && b >= "1" && b <= "9") {
      opts.onReplayDigit(Number(b));
    }
    // Multi-digit buffers drop silently: the user abandoned a count.
  }

  function armTimer() {
    clearTimer();
    timer = setTimeout(expire, timeoutMs);
  }

  return {
    feedDigit(d: string): boolean {
      if (!/^[0-9]$/.test(d)) return false;
      if (buffer === "" && d === "0") return false; // "0" never starts a count
      buffer += d;
      armTimer();
      return true;
    },
    resolveForChord(countable: boolean): number {
      clearTimer();
      const b = buffer;
      buffer = "";
      if (b === "") return 1;
      if (countable) {
        const n = Number(b);
        return Number.isFinite(n) && n > 0 ? n : 1;
      }
      if (b.length === 1 && b >= "1" && b <= "9") {
        opts.onReplayDigit(Number(b));
      }
      return 1;
    },
    pending: () => buffer,
    dispose: clearTimer,
  };
}
