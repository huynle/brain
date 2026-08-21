/**
 * ansi — pure terminal-text helpers for the log surfaces.
 *
 * Agent stdout is *terminal* output: it carries SGR colour escapes,
 * cursor/erase sequences, OSC title writes, and carriage returns used to
 * redraw progress in place. Dropped into a `<div>` unprocessed, an SGR
 * reset shows up as a literal `[0m` and a spinner collapses into one
 * endless line — exactly the two complaints against the Processes tab.
 *
 * Two ways out are offered, both dependency-free:
 *   • `parseAnsi`  — keep the colour, as styled spans (preferred)
 *   • `stripAnsi`  — throw the colour away, keep the text
 *
 * Anchoring matters. The strip pass this replaced also ran an
 * *unanchored* `/\[(?:\d{1,3};)*\d{1,3}[A-Za-z]/` over the text, which
 * happily eats real content: `sleep[5s]` → `sleep]`, `v[1beta]` →
 * `v eta]`. Every pattern here starts at a real ESC (0x1B) or its 8-bit
 * CSI equivalent (0x9B), so ordinary bracketed text is never touched.
 *
 * Everything is pure — no DOM, no react — so it unit-tests under
 * node:test like the rest of lib/.
 *
 * NOTE: `applyCarriageReturns` intentionally mirrors the private helper
 * in lib/shell.ts rather than importing from it. shell.ts belongs to the
 * runner-shell feature and is off-limits to this change; behaviour is
 * identical (plus a trailing-CR fix) and covered by tests here.
 */

// ─── style model ─────────────────────────────────────────────────

export interface AnsiStyle {
  /** CSS colour, already resolved (basic / 256 / truecolor). */
  fg?: string;
  bg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
  strike?: boolean;
}

export interface AnsiSpan {
  text: string;
  style: AnsiStyle;
}

/**
 * Palette. Tuned against the app's near-black log surface (#0a0c0e) —
 * the canonical xterm colours (notably blue #0000ee) are unreadable
 * there, so these lean on the same hues the rest of the UI uses.
 */
const BASIC = [
  "#4b545c", // black  → visible grey, not invisible
  "#e06c5f", // red
  "#6fca7d", // green
  "#f4b23a", // yellow
  "#6a8bff", // blue
  "#c678dd", // magenta
  "#4fb6c4", // cyan
  "#d9dbde", // white
];

const BRIGHT = [
  "#6b757e",
  "#ff8478",
  "#8ee39b",
  "#ffc250",
  "#8aa5ff",
  "#d79bec",
  "#6fd4e2",
  "#ffffff",
];

/** Fallbacks used only to resolve `inverse` (SGR 7). */
const DEFAULT_FG = "#d9dbde";
const DEFAULT_BG = "#0a0c0e";

function hex2(n: number): string {
  return Math.max(0, Math.min(255, Math.round(n))).toString(16).padStart(2, "0");
}

function rgb(r: number, g: number, b: number): string {
  return `#${hex2(r)}${hex2(g)}${hex2(b)}`;
}

/** xterm-256 index → CSS colour. Out-of-range indices fall back to default fg. */
export function ansi256(n: number): string {
  if (!Number.isFinite(n) || n < 0 || n > 255) return DEFAULT_FG;
  const i = Math.floor(n);
  if (i < 8) return BASIC[i];
  if (i < 16) return BRIGHT[i - 8];
  if (i < 232) {
    const c = i - 16;
    const steps = [0, 95, 135, 175, 215, 255];
    return rgb(
      steps[Math.floor(c / 36) % 6],
      steps[Math.floor(c / 6) % 6],
      steps[c % 6],
    );
  }
  const v = 8 + (i - 232) * 10;
  return rgb(v, v, v);
}

// ─── escape-sequence recognition ─────────────────────────────────

/*
 * Alternatives, in priority order:
 *   1. CSI    ESC [ …params… …intermediates… final      (captured: SGR lives here)
 *   2. CSI    0x9B form of the same
 *   3. OSC    ESC ] … BEL | ST                          (window titles, hyperlinks)
 *   4. DCS/PM/APC/SOS  ESC P|X|^|_ … ST
 *   5. any other two-byte escape                        (ESC =, ESC 7, …)
 * Every branch is anchored at ESC/0x9B, so plain "[200 OK]" is data.
 */
const ESCAPE_SOURCE =
  "(?:\\x1B\\[|\\x9B)([0-9;:?<>=]*)([ -/]*)([@-~])" +
  "|\\x1B\\][\\s\\S]*?(?:\\x07|\\x1B\\\\|\\x9C)" +
  "|\\x1B[P^_X][\\s\\S]*?(?:\\x1B\\\\|\\x9C)" +
  "|\\x1B[\\s\\S]";

const ESCAPE_PARSE = new RegExp(ESCAPE_SOURCE, "g");
const ESCAPE_STRIP = new RegExp(ESCAPE_SOURCE, "g");

/**
 * C0/C1 control characters that must never reach the DOM. Tab and
 * newline are kept (the log pane renders them via `white-space:
 * pre-wrap`); CR is kept here because `normalizeLogText` resolves it
 * first, and a stray survivor is harmless.
 */
const CONTROL_CHARS = /[\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/g;

function scrub(text: string): string {
  // `String.replace` with a /g regex resets lastIndex itself; `test`
  // would not, so never reach for it on a shared global regex.
  return text.replace(CONTROL_CHARS, "");
}

/** Remove every escape sequence and control character, keep the text. */
export function stripAnsi(input?: string): string {
  if (!input) return "";
  ESCAPE_STRIP.lastIndex = 0;
  return input.replace(ESCAPE_STRIP, "").replace(CONTROL_CHARS, "");
}

/** True when the string carries anything the raw renderer would mangle. */
export function hasAnsi(input?: string): boolean {
  if (!input) return false;
  return input.includes("\x1B") || input.includes("\x9B");
}

// ─── SGR state machine ───────────────────────────────────────────

interface SgrState {
  fg?: string;
  bg?: string;
  bold: boolean;
  dim: boolean;
  italic: boolean;
  underline: boolean;
  strike: boolean;
  inverse: boolean;
}

function newState(): SgrState {
  // fg/bg are listed explicitly so `Object.assign(state, newState())`
  // actually clears a colour set earlier — omitting the keys would leave
  // the old colour standing through an SGR 0 reset.
  return {
    fg: undefined,
    bg: undefined,
    bold: false,
    dim: false,
    italic: false,
    underline: false,
    strike: false,
    inverse: false,
  };
}

/**
 * Read the extended-colour tail of a `38`/`48` selector.
 *
 * Handles both the `;`-separated form (`38;5;196`, `38;2;255;0;0`) and
 * the ITU `:`-separated form, whose empty colour-space slot
 * (`38:2::255:0:0`) is skipped rather than read as a zero.
 * Returns the colour plus the index of the last token consumed.
 */
function readExtendedColor(
  parts: string[],
  start: number,
): { color?: string; next: number } {
  const nums: number[] = [];
  let j = start;
  while (j < parts.length) {
    const tok = parts[j];
    j++;
    if (tok === "") continue; // empty colour-space id
    const v = Number(tok);
    if (!Number.isFinite(v)) break;
    nums.push(v);
    if (nums[0] === 5 && nums.length === 2) break;
    if (nums[0] === 2 && nums.length === 4) break;
    if (nums[0] !== 5 && nums[0] !== 2) break; // unknown selector
  }
  if (nums[0] === 5 && nums.length >= 2) {
    return { color: ansi256(nums[1]), next: j - 1 };
  }
  if (nums[0] === 2 && nums.length >= 4) {
    return { color: rgb(nums[1], nums[2], nums[3]), next: j - 1 };
  }
  return { next: j - 1 };
}

function applySgr(state: SgrState, params: string): SgrState {
  const s: SgrState = { ...state };
  /*
   * Split on `;` ONLY. A `:` introduces ECMA-48 *sub*-parameters, which
   * refine the parameter they follow — they are not parameters of their
   * own. Flattening both separators made `ESC[4:3m` (curly underline,
   * emitted by rustc/clang/gcc diagnostics) read as 4 then 3 → underline
   * AND italic, and made `ESC[4:0m` (underline off) read as 4 then 0 →
   * a full SGR reset that silently dropped the colour for the rest of
   * the line. Only 38/48/58 legitimately carry sub-parameters we read.
   */
  const parts = (params === "" ? "0" : params).split(";");
  for (let i = 0; i < parts.length; i++) {
    const tok = parts[i];
    const colon = tok.indexOf(":");
    const head = colon === -1 ? tok : tok.slice(0, colon);
    // An omitted parameter is a zero (reset), per ECMA-48.
    const n = head === "" ? 0 : Number(head);
    if (!Number.isFinite(n)) continue;
    if (n === 38 || n === 48 || n === 58) {
      // Colour selector: sub-parameters when colon-formed
      // (`38:2::255:0:0`), the following `;` tokens otherwise
      // (`38;2;255;0;0`). Either way they must be CONSUMED, never read
      // back as attributes.
      let color: string | undefined;
      if (colon === -1) {
        const ext = readExtendedColor(parts, i + 1);
        color = ext.color;
        i = ext.next;
      } else {
        color = readExtendedColor(tok.split(":"), 1).color;
      }
      // 58 selects the underline colour, which this renderer does not
      // draw — its parameters are consumed and dropped.
      if (color && n === 38) s.fg = color;
      else if (color && n === 48) s.bg = color;
      continue;
    }
    if (n === 4 && colon !== -1) {
      // 4:0 is "underline off"; 4:1…4:5 pick a line style (single,
      // double, curly, dotted, dashed) — all drawn as one underline.
      s.underline = tok.slice(colon + 1).split(":")[0] !== "0";
      continue;
    }
    if (n === 0) {
      Object.assign(s, newState());
    } else if (n === 1) s.bold = true;
    else if (n === 2) s.dim = true;
    else if (n === 3) s.italic = true;
    else if (n === 4) s.underline = true;
    else if (n === 7) s.inverse = true;
    else if (n === 9) s.strike = true;
    else if (n === 21 || n === 22) {
      s.bold = false;
      s.dim = false;
    } else if (n === 23) s.italic = false;
    else if (n === 24) s.underline = false;
    else if (n === 27) s.inverse = false;
    else if (n === 29) s.strike = false;
    else if (n >= 30 && n <= 37) s.fg = BASIC[n - 30];
    else if (n === 39) s.fg = undefined;
    else if (n >= 40 && n <= 47) s.bg = BASIC[n - 40];
    else if (n === 49) s.bg = undefined;
    else if (n >= 90 && n <= 97) s.fg = BRIGHT[n - 90];
    else if (n >= 100 && n <= 107) s.bg = BRIGHT[n - 100];
  }
  return s;
}

/** Freeze the mutable machine state into the public style shape. */
function resolve(st: SgrState): AnsiStyle {
  let fg = st.fg;
  let bg = st.bg;
  if (st.inverse) {
    const f = fg ?? DEFAULT_FG;
    const b = bg ?? DEFAULT_BG;
    fg = b;
    bg = f;
  }
  const out: AnsiStyle = {};
  if (fg) out.fg = fg;
  if (bg) out.bg = bg;
  if (st.bold) out.bold = true;
  if (st.dim) out.dim = true;
  if (st.italic) out.italic = true;
  if (st.underline) out.underline = true;
  if (st.strike) out.strike = true;
  return out;
}

function sameStyle(a: AnsiStyle, b: AnsiStyle): boolean {
  return (
    a.fg === b.fg &&
    a.bg === b.bg &&
    !!a.bold === !!b.bold &&
    !!a.dim === !!b.dim &&
    !!a.italic === !!b.italic &&
    !!a.underline === !!b.underline &&
    !!a.strike === !!b.strike
  );
}

/** True when the style would change how the text renders at all. */
export function hasAnsiStyle(style: AnsiStyle): boolean {
  return (
    !!style.fg ||
    !!style.bg ||
    !!style.bold ||
    !!style.dim ||
    !!style.italic ||
    !!style.underline ||
    !!style.strike
  );
}

/**
 * Split terminal text into styled spans.
 *
 * SGR sequences drive the style; every other escape (cursor moves, erase,
 * OSC titles) is dropped, as is any C0 control character. Adjacent runs
 * sharing a style are coalesced, so unstyled text yields exactly one
 * span and the caller can skip wrapping it.
 *
 * Never throws; a string of pure escapes yields an empty array.
 */
export function parseAnsi(input?: string): AnsiSpan[] {
  const text = input ?? "";
  if (text === "") return [];
  if (!hasAnsi(text)) {
    const clean = scrub(text);
    return clean === "" ? [] : [{ text: clean, style: {} }];
  }

  const spans: AnsiSpan[] = [];
  let state = newState();
  let last = 0;

  const push = (chunk: string) => {
    if (chunk === "") return;
    const clean = scrub(chunk);
    if (clean === "") return;
    const style = resolve(state);
    const prev = spans[spans.length - 1];
    if (prev && sameStyle(prev.style, style)) prev.text += clean;
    else spans.push({ text: clean, style });
  };

  ESCAPE_PARSE.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = ESCAPE_PARSE.exec(text)) !== null) {
    push(text.slice(last, m.index));
    last = m.index + m[0].length;
    // Only a CSI with no intermediate bytes and final byte `m` is SGR.
    if (m[1] !== undefined && m[3] === "m" && !m[2]) {
      state = applySgr(state, m[1]);
    }
    // Zero-length match guard (cannot happen with this pattern, but a
    // runaway lastIndex would hang the render thread).
    if (m[0] === "") ESCAPE_PARSE.lastIndex++;
  }
  push(text.slice(last));
  return spans;
}

/** Style → inline CSS. Kept here so the React layer stays declarative. */
export function ansiStyleToCss(style: AnsiStyle): Record<string, string | number> {
  const css: Record<string, string | number> = {};
  if (style.fg) css.color = style.fg;
  if (style.bg) css.background = style.bg;
  if (style.bold) css.fontWeight = 600;
  if (style.dim) css.opacity = 0.65;
  if (style.italic) css.fontStyle = "italic";
  const deco: string[] = [];
  if (style.underline) deco.push("underline");
  if (style.strike) deco.push("line-through");
  if (deco.length > 0) css.textDecoration = deco.join(" ");
  return css;
}

// ─── carriage returns ────────────────────────────────────────────

/**
 * Collapse carriage-return overwrites the way a terminal would: only the
 * text drawn after the last CR is visible. This is what turns a 400-frame
 * progress bar back into one line.
 *
 * A *trailing* CR is not an overwrite — it parks the cursor at column 0
 * and erases nothing — so trailing empty segments are ignored rather than
 * blanking the line.
 */
export function applyCarriageReturns(line: string): string {
  if (!line.includes("\r")) return line;
  const parts = line.split("\r");
  while (parts.length > 1 && parts[parts.length - 1] === "") parts.pop();
  return parts[parts.length - 1];
}

/**
 * Prepare a raw log payload for display: CRLF normalised to LF, then CR
 * overwrite applied per physical line. Escape sequences are left intact
 * so the caller can still choose colour (`parseAnsi`) over stripping.
 */
export function normalizeLogText(content?: string): string {
  if (!content) return "";
  const lf = content.replace(/\r\n/g, "\n");
  if (!lf.includes("\r")) return lf;
  return lf.split("\n").map(applyCarriageReturns).join("\n");
}

/**
 * One-shot plain-text cleanup: CR overwrite + escape stripping.
 * Use when styled spans are not an option (titles, tooltips, copy).
 */
export function toPlainText(content?: string): string {
  return stripAnsi(normalizeLogText(content));
}

/**
 * The whole display pipeline in one call: CR overwrite, then styled
 * spans. Every surface that shows captured terminal output goes through
 * this (via <TerminalText>), so no caller can accidentally apply half of
 * it — running `parseAnsi` alone leaves spinner frames un-collapsed.
 */
export function terminalSpans(content?: string): AnsiSpan[] {
  return parseAnsi(normalizeLogText(content));
}
