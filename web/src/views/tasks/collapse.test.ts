import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  isFeatureCollapsed,
  parseCollapseState,
  serializeCollapseState,
  setAllCollapsed,
  toggleFeatureCollapsed,
  type CollapseState,
} from "./collapse";

test("isFeatureCollapsed falls back to scope default when nothing is recorded", () => {
  assert.equal(isFeatureCollapsed({}, "__all__", "auth", true), true);
  assert.equal(isFeatureCollapsed({}, "my-project", "auth", false), false);
});

test("toggleFeatureCollapsed flips against the scope default", () => {
  let state: CollapseState = {};
  state = toggleFeatureCollapsed(state, "my-project", "auth", false);
  assert.equal(isFeatureCollapsed(state, "my-project", "auth", false), true);
  state = toggleFeatureCollapsed(state, "my-project", "auth", false);
  assert.equal(isFeatureCollapsed(state, "my-project", "auth", false), false);
});

test("collapse state is independent per scope", () => {
  let state: CollapseState = {};
  state = toggleFeatureCollapsed(state, "project-a", "auth", false);
  assert.equal(isFeatureCollapsed(state, "project-a", "auth", false), true);
  // Same feature name in another project tab is untouched.
  assert.equal(isFeatureCollapsed(state, "project-b", "auth", false), false);
  assert.equal(isFeatureCollapsed(state, "__all__", "auth", true), true);
});

test("setAllCollapsed sets the baseline and clears per-feature overrides", () => {
  let state: CollapseState = {};
  state = toggleFeatureCollapsed(state, "p", "auth", false); // auth collapsed
  state = setAllCollapsed(state, "p", false); // expand all
  assert.equal(isFeatureCollapsed(state, "p", "auth", false), false);
  // Features that appear later follow the new baseline too.
  state = setAllCollapsed(state, "p", true);
  assert.equal(isFeatureCollapsed(state, "p", "brand-new-feature", false), true);
});

test("serialize/parse round-trips", () => {
  let state: CollapseState = {};
  state = toggleFeatureCollapsed(state, "p", "auth", false);
  state = setAllCollapsed(state, "__all__", false);
  const back = parseCollapseState(serializeCollapseState(state));
  assert.deepEqual(back, state);
});

test("parseCollapseState degrades malformed input to empty state", () => {
  assert.deepEqual(parseCollapseState(null), {});
  assert.deepEqual(parseCollapseState(""), {});
  assert.deepEqual(parseCollapseState("not json"), {});
  assert.deepEqual(parseCollapseState("[1,2]"), {});
  assert.deepEqual(parseCollapseState('{"p": "bogus"}'), {});
  // Non-boolean overrides are dropped; well-formed siblings survive.
  const mixed = parseCollapseState(
    '{"p": {"default": true, "overrides": {"auth": false, "bad": "x"}}}',
  );
  assert.deepEqual(mixed, { p: { default: true, overrides: { auth: false } } });
});
