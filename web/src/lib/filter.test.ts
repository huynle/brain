import { strict as assert } from "node:assert";
import { test } from "node:test";
import { describeFilter, isEmptyFilter, matchTask, parseFilter, type TaskLike } from "./filter";

const task = (over: Partial<TaskLike> = {}): TaskLike => ({
  id: "t1",
  title: "Fix auth login flow",
  status: "pending",
  feature_id: "auth-feature",
  tags: ["backend", "urgent"],
  priority: "high",
  executor: "script",
  projectId: "demo",
  ...over,
});

test("parseFilter splits plain terms and field terms", () => {
  const f = parseFilter('auth status:blocked feature:login tag:"a b"');
  assert.deepEqual(f.text, ["auth"]);
  assert.deepEqual(f.fields.status, ["blocked"]);
  assert.deepEqual(f.fields.feature, ["login"]);
  assert.deepEqual(f.fields.tag, ["a b"]);
});

test("unknown field prefixes degrade to plain substring terms", () => {
  const f = parseFilter("http://example.com/x notafield:value");
  assert.deepEqual(f.fields, {});
  assert.deepEqual(f.text, ["http://example.com/x", "notafield:value"]);
});

test("plain terms AND together over title/id/feature/status/tags", () => {
  assert.ok(matchTask(task(), parseFilter("auth urgent")));
  assert.ok(!matchTask(task(), parseFilter("auth missing-term")));
  assert.ok(matchTask(task(), parseFilter("AUTH-FEATURE")));
});

test("same field ORs, different fields AND", () => {
  const t = task({ status: "blocked" });
  assert.ok(matchTask(t, parseFilter("status:blocked status:completed")));
  assert.ok(matchTask(t, parseFilter("status:blocked feature:auth")));
  assert.ok(!matchTask(t, parseFilter("status:blocked feature:other")));
  assert.ok(!matchTask(t, parseFilter("status:completed")));
});

test("field values prefix-match", () => {
  assert.ok(matchTask(task({ status: "completed" }), parseFilter("status:comp")));
  assert.ok(matchTask(task(), parseFilter("priority:hi")));
  assert.ok(!matchTask(task(), parseFilter("priority:lo")));
});

test("status:ready pseudo-value checks runnability", () => {
  assert.ok(matchTask(task(), parseFilter("status:ready")));
  assert.ok(!matchTask(task({ waiting_on: ["t0"] }), parseFilter("status:ready")));
  assert.ok(!matchTask(task({ status: "blocked" }), parseFilter("status:ready")));
  assert.ok(matchTask(task({ status: "active" }), parseFilter("status:ready")));
});

test("empty filter matches everything and reports empty", () => {
  const f = parseFilter("   ");
  assert.ok(isEmptyFilter(f));
  assert.ok(matchTask(task(), f));
});

test("describeFilter renders header chip text", () => {
  const f = parseFilter("auth status:blocked status:completed");
  assert.equal(describeFilter(f), "status:blocked|completed auth");
});
