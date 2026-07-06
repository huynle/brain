import { strict as assert } from "node:assert";
import { test } from "node:test";
import { fuzzyBest, fuzzyResolve, fuzzyScore } from "./fuzzy";

test("fuzzyScore rejects non-subsequences and accepts empty query", () => {
  assert.equal(fuzzyScore("brain-api", "xyz"), null);
  assert.equal(fuzzyScore("brain-api", ""), 0);
  assert.notEqual(fuzzyScore("brain-api", "bapi"), null);
});

test("fuzzyScore prefers prefix over word-boundary over scattered", () => {
  const prefix = fuzzyScore("demo-project", "demo")!;
  const boundary = fuzzyScore("my-demo-thing", "demo")!;
  const scattered = fuzzyScore("dxexmxo", "demo")!;
  assert.ok(prefix > boundary, `prefix ${prefix} should beat boundary ${boundary}`);
  assert.ok(boundary > scattered, `boundary ${boundary} should beat scattered ${scattered}`);
});

test("fuzzyScore prefers shorter names on equal structure", () => {
  const short = fuzzyScore("demo", "demo")!;
  const long = fuzzyScore("demo-with-a-much-longer-suffix", "demo")!;
  assert.ok(short > long);
});

test("fuzzyBest ranks matches and drops non-matches, stable on ties", () => {
  const items = ["alpha", "beta-proj", "demo", "beta-prod"];
  const ranked = fuzzyBest(items, (s) => s, "beta");
  assert.deepEqual(
    ranked.map((r) => r.item),
    ["beta-proj", "beta-prod"],
  );
});

test("fuzzyResolve: exact match wins outright even when fuzzy prefers another", () => {
  assert.equal(fuzzyResolve(["demo", "demo-2"], (s) => s, "DEMO"), "demo");
});

test("fuzzyResolve: ambiguity returns null instead of guessing", () => {
  // Two equally-good candidates for the query.
  assert.equal(fuzzyResolve(["proj-a", "proj-b"], (s) => s, "proj"), null);
  // A clearly better candidate resolves.
  assert.equal(fuzzyResolve(["brain-api", "unrelated"], (s) => s, "brain"), "brain-api");
  // No match at all.
  assert.equal(fuzzyResolve(["alpha", "beta"], (s) => s, "zzz"), null);
});
