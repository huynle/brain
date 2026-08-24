/**
 * Contract between <Toasts/> and the stylesheet that has to render it.
 *
 * There is no DOM test harness in this package, and the bug these tests pin
 * needed none: `.toast` was declared `opacity: 0` with a `.toast.show` rule
 * to reveal it, while nothing in the React app ever added `show` (it was a
 * leftover from the pre-React wireframe host, which toggled the class
 * imperatively). Every toast this app raised painted invisibly for its whole
 * 4s life — which is how "Run feature now" came to look like a dead menu
 * item: the server's explanation arrived, was rendered, and could not be
 * read. Nothing failed, so nothing caught it.
 *
 * So these assert the two properties a unit test CAN see: the base rule
 * leaves the toast visible and clickable, and no `.toast.<state>` rule
 * depends on a class the component never applies.
 */
import { strict as assert } from "node:assert";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const read = (rel: string) =>
  readFileSync(fileURLToPath(new URL(rel, import.meta.url)), "utf8");

// Comments are stripped first: this file's own rules carry a comment that
// quotes the offending `.toast.show` declaration, and a scan that counted it
// would fail on the explanation rather than on the code.
const css = read("../../styles/global.css").replace(/\/\*[\s\S]*?\*\//g, "");
const component = read("./Toasts.tsx");

/** The body of the first rule whose selector list is exactly `selector`. */
function ruleBody(selector: string): string {
  const match = new RegExp(
    `(^|\\})\\s*${selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}\\s*\\{([^}]*)\\}`,
    "m",
  ).exec(css);
  assert.ok(match, `expected a \`${selector}\` rule in global.css`);
  return match![2];
}

test("the base .toast rule leaves the toast visible", () => {
  const body = ruleBody(".toast");
  for (const hidden of [
    /opacity:\s*0(\D|$)/,
    /display:\s*none/,
    /visibility:\s*hidden/,
  ]) {
    assert.ok(
      !hidden.test(body),
      `.toast must not hide itself (matched ${hidden}) — nothing adds a class to reveal it`,
    );
  }
});

test("a toast can be clicked to dismiss", () => {
  // The component's onClick dismisses, and the action button lives inside
  // the same box; `pointer-events: none` would swallow both silently.
  assert.match(ruleBody(".toast"), /pointer-events:\s*auto/);
});

test("no .toast state rule depends on a class the component never applies", () => {
  // Only the kind comes from the component: `toast ${t.kind}`.
  const allowed = new Set(["info", "success", "error", "warning"]);
  const compound = [...css.matchAll(/\.toast\.([\w-]+)/g)].map((m) => m[1]);
  const orphans = compound.filter((cls) => !allowed.has(cls));
  assert.deepEqual(
    orphans,
    [],
    `global.css styles .toast.${orphans[0]} but <Toasts/> only ever adds a kind class`,
  );
  // Guard the premise: if the component starts adding state classes, this
  // test's allow-list has to be revisited rather than silently passing.
  assert.match(component, /className=\{`toast \$\{t\.kind\}`\}/);
});

test("every class the component renders is styled", () => {
  for (const cls of ["toast-wrap", "toast", "toast-message", "toast-action"]) {
    assert.ok(
      new RegExp(`\\.${cls}[\\s,{.:]`).test(css),
      `.${cls} is rendered by <Toasts/> but has no rule in global.css`,
    );
  }
});
