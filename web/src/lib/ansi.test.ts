import test from "node:test";
import assert from "node:assert/strict";
import {
  ansi256,
  ansiStyleToCss,
  applyCarriageReturns,
  hasAnsi,
  hasAnsiStyle,
  normalizeLogText,
  parseAnsi,
  stripAnsi,
  terminalSpans,
  toPlainText,
} from "./ansi";

const ESC = "\x1B";

// ─── stripAnsi ───────────────────────────────────────────────────

test("stripAnsi: removes SGR colour sequences", () => {
  assert.equal(stripAnsi(`${ESC}[31mred${ESC}[0m`), "red");
  assert.equal(stripAnsi(`${ESC}[1;38;5;196mbright${ESC}[m`), "bright");
  assert.equal(stripAnsi(`plain`), "plain");
  assert.equal(stripAnsi(""), "");
  assert.equal(stripAnsi(undefined), "");
});

test("stripAnsi: removes cursor/erase sequences and OSC titles", () => {
  assert.equal(stripAnsi(`${ESC}[2J${ESC}[Hcleared`), "cleared");
  assert.equal(stripAnsi(`a${ESC}[1Ab`), "ab");
  assert.equal(stripAnsi(`${ESC}]0;window title\x07done`), "done");
  assert.equal(stripAnsi(`${ESC}]8;;http://x\x07link${ESC}]8;;\x07`), "link");
  // 8-bit CSI
  assert.equal(stripAnsi(`\x9B32mgreen\x9B0m`), "green");
  // Two-byte escapes (charset select, save cursor)
  assert.equal(stripAnsi(`${ESC}7saved${ESC}8`), "saved");
});

test("stripAnsi: strips control characters but keeps tabs and newlines", () => {
  assert.equal(stripAnsi("a\x07b"), "ab");
  assert.equal(stripAnsi("a\x00b\x7Fc"), "abc");
  assert.equal(stripAnsi("col\tumn\nnext"), "col\tumn\nnext");
});

/**
 * Regression: the pass this replaced ran an unanchored
 * /\[(?:\d{1,3};)*\d{1,3}[A-Za-z]/ over log text, which shreds ordinary
 * bracketed content. Nothing without a real ESC may be touched.
 */
test("stripAnsi: never eats ordinary bracketed text", () => {
  const samples = [
    "[200 OK] GET /api/v1/tasks",
    "array[3]d",
    "sleep[5s] then retry",
    "v[1beta] released",
    "[404m] not a colour",
    "matrix[12][34]",
    "budget [100USD]",
    "[0m",
    "ESC[0m written as literal text",
  ];
  for (const s of samples) {
    assert.equal(stripAnsi(s), s, `mangled: ${s}`);
  }
});

test("hasAnsi: detects 7-bit and 8-bit escapes only", () => {
  assert.equal(hasAnsi(`${ESC}[0m`), true);
  assert.equal(hasAnsi("\x9B0m"), true);
  assert.equal(hasAnsi("[0m"), false);
  assert.equal(hasAnsi(""), false);
  assert.equal(hasAnsi(undefined), false);
});

// ─── parseAnsi ───────────────────────────────────────────────────

test("parseAnsi: plain text is one unstyled span", () => {
  const spans = parseAnsi("hello [200 OK]");
  assert.equal(spans.length, 1);
  assert.equal(spans[0].text, "hello [200 OK]");
  assert.equal(hasAnsiStyle(spans[0].style), false);
});

test("parseAnsi: empty and escape-only input yields no spans", () => {
  assert.deepEqual(parseAnsi(""), []);
  assert.deepEqual(parseAnsi(undefined), []);
  assert.deepEqual(parseAnsi(`${ESC}[0m${ESC}[2J`), []);
});

test("parseAnsi: basic colours split into styled spans", () => {
  const spans = parseAnsi(`ok ${ESC}[31mfail${ESC}[0m done`);
  assert.equal(spans.length, 3);
  assert.equal(spans[0].text, "ok ");
  assert.equal(spans[0].style.fg, undefined);
  assert.equal(spans[1].text, "fail");
  assert.equal(spans[1].style.fg, "#e06c5f");
  assert.equal(spans[2].text, " done");
  assert.equal(spans[2].style.fg, undefined);
});

test("parseAnsi: attributes accumulate and reset", () => {
  const spans = parseAnsi(`${ESC}[1m${ESC}[4mA${ESC}[24mB${ESC}[0mC`);
  assert.deepEqual(
    spans.map((s) => [s.text, !!s.style.bold, !!s.style.underline]),
    [
      ["A", true, true],
      ["B", true, false],
      ["C", false, false],
    ],
  );
});

test("parseAnsi: bare ESC[m is a reset", () => {
  const spans = parseAnsi(`${ESC}[32mgreen${ESC}[mplain`);
  assert.equal(spans[0].style.fg, "#6fca7d");
  assert.equal(spans[1].style.fg, undefined);
});

test("parseAnsi: 256-colour and truecolor selectors", () => {
  const c256 = parseAnsi(`${ESC}[38;5;196mx`);
  assert.equal(c256[0].style.fg, ansi256(196));
  const truecolor = parseAnsi(`${ESC}[38;2;18;34;51mx`);
  assert.equal(truecolor[0].style.fg, "#122233");
  const bg = parseAnsi(`${ESC}[48;5;21mx`);
  assert.equal(bg[0].style.bg, ansi256(21));
  // ITU colon form with an empty colour-space slot must not read 0 as red
  const itu = parseAnsi(`${ESC}[38:2::255:0:0mx`);
  assert.equal(itu[0].style.fg, "#ff0000");
});

/*
 * ECMA-48 sub-parameters. `:` binds to the parameter it follows, so
 * splitting on /[;:]/ turned rustc/clang's `ESC[4:3m` (curly underline)
 * into 4 then 3 — underline AND italic — and `ESC[4:0m` (underline off)
 * into 4 then 0, a full reset that dropped the colour mid-line.
 */
test("parseAnsi: colon sub-parameters refine, they do not become parameters", () => {
  const curly = parseAnsi(`${ESC}[4:3mx`);
  assert.equal(curly[0].style.underline, true);
  assert.equal(curly[0].style.italic, undefined);

  const off = parseAnsi(`${ESC}[31;4mred${ESC}[4:0mstill red`);
  assert.equal(off[0].style.underline, true);
  assert.equal(off[0].style.fg, "#e06c5f");
  assert.equal(off[1].text, "still red");
  assert.equal(off[1].style.underline, undefined);
  assert.equal(off[1].style.fg, "#e06c5f", "4:0 must not reset the colour");

  // Other line styles all render as a single underline.
  for (const sub of ["1", "2", "4", "5"]) {
    assert.equal(parseAnsi(`${ESC}[4:${sub}mx`)[0].style.underline, true);
  }
});

test("parseAnsi: 58 (underline colour) consumes its parameters", () => {
  // Not rendered, but its 5;196 must not be read back as attributes.
  const spans = parseAnsi(`${ESC}[31;58;5;196mx`);
  assert.equal(spans[0].style.fg, "#e06c5f");
  assert.equal(spans[0].style.bg, undefined);
  assert.equal(parseAnsi(`${ESC}[58:2::0:0:255mx`)[0].style.fg, undefined);
});

test("parseAnsi: bright colours", () => {
  assert.equal(parseAnsi(`${ESC}[91mx`)[0].style.fg, "#ff8478");
  assert.equal(parseAnsi(`${ESC}[102mx`)[0].style.bg, "#8ee39b");
});

test("parseAnsi: inverse swaps foreground and background", () => {
  const spans = parseAnsi(`${ESC}[31;7mx`);
  assert.equal(spans[0].style.bg, "#e06c5f");
  assert.ok(spans[0].style.fg); // default fg moved into the background slot
  const off = parseAnsi(`${ESC}[31;7m${ESC}[27my`);
  assert.equal(off[0].style.fg, "#e06c5f");
  assert.equal(off[0].style.bg, undefined);
});

test("parseAnsi: non-SGR CSI sequences are dropped, not styled", () => {
  const spans = parseAnsi(`${ESC}[2K${ESC}[1;5Hplain`);
  assert.equal(spans.length, 1);
  assert.equal(spans[0].text, "plain");
  assert.equal(hasAnsiStyle(spans[0].style), false);
});

test("parseAnsi: adjacent same-style runs coalesce", () => {
  const spans = parseAnsi(`${ESC}[31ma${ESC}[31mb`);
  assert.equal(spans.length, 1);
  assert.equal(spans[0].text, "ab");
});

test("parseAnsi: strips control characters inside spans", () => {
  const spans = parseAnsi(`${ESC}[31ma\x07b`);
  assert.equal(spans[0].text, "ab");
});

test("parseAnsi: text is preserved exactly across a real-world line", () => {
  const line = `${ESC}[90m12:04:31${ESC}[0m ${ESC}[32mINFO${ESC}[0m built 3 targets [200 OK]`;
  assert.equal(
    parseAnsi(line)
      .map((s) => s.text)
      .join(""),
    "12:04:31 INFO built 3 targets [200 OK]",
  );
});

// ─── ansi256 ─────────────────────────────────────────────────────

test("ansi256: bands", () => {
  assert.equal(ansi256(1), "#e06c5f"); // basic
  assert.equal(ansi256(9), "#ff8478"); // bright
  assert.equal(ansi256(16), "#000000"); // cube origin
  assert.equal(ansi256(231), "#ffffff"); // cube max
  assert.equal(ansi256(232), "#080808"); // greyscale start
  assert.equal(ansi256(255), "#eeeeee"); // greyscale end
  assert.equal(ansi256(-1), "#d9dbde"); // out of range → default fg
  assert.equal(ansi256(999), "#d9dbde");
});

// ─── ansiStyleToCss ──────────────────────────────────────────────

test("ansiStyleToCss: maps only the set attributes", () => {
  assert.deepEqual(ansiStyleToCss({}), {});
  assert.deepEqual(ansiStyleToCss({ fg: "#fff", bold: true }), {
    color: "#fff",
    fontWeight: 600,
  });
  assert.deepEqual(ansiStyleToCss({ underline: true, strike: true }), {
    textDecoration: "underline line-through",
  });
  assert.deepEqual(ansiStyleToCss({ dim: true, italic: true }), {
    opacity: 0.65,
    fontStyle: "italic",
  });
});

// ─── carriage returns ────────────────────────────────────────────

test("applyCarriageReturns: only text after the last CR survives", () => {
  assert.equal(applyCarriageReturns("no cr here"), "no cr here");
  assert.equal(applyCarriageReturns("10%\r50%\r100%"), "100%");
  assert.equal(applyCarriageReturns("\rstart"), "start");
});

test("applyCarriageReturns: a trailing CR erases nothing", () => {
  assert.equal(applyCarriageReturns("done\r"), "done");
  assert.equal(applyCarriageReturns("a\rb\r\r"), "b");
  assert.equal(applyCarriageReturns("\r"), "");
});

test("normalizeLogText: CRLF becomes LF, CR overwrites per line", () => {
  assert.equal(normalizeLogText("a\r\nb"), "a\nb");
  assert.equal(normalizeLogText("one\ntwo"), "one\ntwo");
  assert.equal(
    normalizeLogText("build 10%\rbuild 90%\rbuild 100%\ndone"),
    "build 100%\ndone",
  );
  assert.equal(normalizeLogText(""), "");
  assert.equal(normalizeLogText(undefined), "");
});

test("normalizeLogText: leaves escape sequences for the colour pass", () => {
  const s = `${ESC}[32mok${ESC}[0m`;
  assert.equal(normalizeLogText(s), s);
});

test("normalizeLogText: a spinner collapses to its final frame", () => {
  const spinner = ["|", "/", "-", "\\", "done"].join("\r");
  assert.equal(normalizeLogText(spinner), "done");
});

test("toPlainText: CR overwrite then escape stripping", () => {
  assert.equal(
    toPlainText(`${ESC}[32m10%\r${ESC}[32m100%${ESC}[0m`),
    "100%",
  );
  assert.equal(toPlainText("[200 OK]"), "[200 OK]");
  assert.equal(toPlainText(undefined), "");
});

// ─── terminalSpans (the whole display pipeline) ──────────────────

test("terminalSpans: collapses CR frames AND keeps colour", () => {
  const spinner = `${ESC}[36m|\r${ESC}[36m/\r${ESC}[32mdone${ESC}[0m`;
  const spans = terminalSpans(spinner);
  assert.equal(
    spans.map((s) => s.text).join(""),
    "done",
    "only the final frame survives",
  );
  assert.equal(spans[0].style.fg, "#6fca7d");
});

test("terminalSpans: prose without escapes or CR passes through unchanged", () => {
  // The chat transcript renders markdown text parts through this; blank
  // lines, indentation and bracketed text must survive verbatim.
  const prose = "Fixed it.\n\n  - ran `npm test` [200 OK]\n  - see array[3]\n";
  const spans = terminalSpans(prose);
  assert.equal(spans.length, 1);
  assert.equal(spans[0].text, prose);
  assert.equal(hasAnsiStyle(spans[0].style), false);
});

test("terminalSpans: empty input yields no spans", () => {
  assert.deepEqual(terminalSpans(""), []);
  assert.deepEqual(terminalSpans(undefined), []);
});
