/**
 * Wire-format tests for the goal API wrappers.
 *
 * Same hand-rolled fetch capture as apiAssignment.test.ts: assert the
 * exact URL, method, and JSON body the client emits — that shape is the
 * contract with the Go handlers in internal/api/goals.go.
 */
import { strict as assert } from "node:assert";
import { test, beforeEach, afterEach } from "node:test";

import {
  archiveGoal,
  deleteGoal,
  listGoals,
  pauseGoal,
  resumeGoal,
  runGoal,
} from "./api";
import { useAuth } from "./auth";

type Captured = { url: string; method: string; body: unknown };

let captured: Captured[] = [];
let nextBody: unknown = {};
const originalFetch = globalThis.fetch;

beforeEach(() => {
  captured = [];
  nextBody = {};
  useAuth.setState({ token: "test-token" });

  globalThis.fetch = (async (
    input: RequestInfo | URL,
    init?: RequestInit,
  ): Promise<Response> => {
    const url =
      typeof input === "string"
        ? input
        : input instanceof URL
          ? input.toString()
          : (input as Request).url;
    let bodyJson: unknown = undefined;
    if (init?.body && typeof init.body === "string") {
      try {
        bodyJson = JSON.parse(init.body);
      } catch {
        bodyJson = init.body;
      }
    }
    captured.push({
      url,
      method: (init?.method ?? "GET").toUpperCase(),
      body: bodyJson,
    });
    return new Response(JSON.stringify(nextBody), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  }) as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

test("listGoals without params sends no query string", async () => {
  nextBody = { goals: [], count: 0 };
  const goals = await listGoals();
  assert.deepEqual(goals, []);
  assert.equal(captured[0].url, "/api/v1/goals");
  assert.equal(captured[0].method, "GET");
});

test("listGoals passes project/feature_id/status as query params", async () => {
  nextBody = { goals: null, count: 0 };
  const goals = await listGoals({
    project: "brain-api",
    feature_id: "auth",
    status: "archived",
  });
  // `goals: null` from the server still yields an array to callers.
  assert.deepEqual(goals, []);
  const url = new URL(captured[0].url, "http://x");
  assert.equal(url.pathname, "/api/v1/goals");
  assert.equal(url.searchParams.get("project"), "brain-api");
  assert.equal(url.searchParams.get("feature_id"), "auth");
  assert.equal(url.searchParams.get("status"), "archived");
});

test("deleteGoal DELETEs the id-encoded URL", async () => {
  nextBody = { success: true, goal_id: "a/b" };
  const r = await deleteGoal("a/b");
  assert.equal(r.success, true);
  assert.equal(captured[0].method, "DELETE");
  assert.equal(captured[0].url, "/api/v1/goals/a%2Fb");
});

test("runGoal POSTs to /run with no body", async () => {
  nextBody = { decision: "noop", reason: "in progress" };
  await runGoal("g-1");
  assert.equal(captured[0].method, "POST");
  assert.equal(captured[0].url, "/api/v1/goals/g-1/run");
  assert.equal(captured[0].body, undefined);
});

test("lifecycle wrappers PATCH the matching status", async () => {
  nextBody = { goal_id: "g-1" };
  await pauseGoal("g-1");
  await resumeGoal("g-1");
  await archiveGoal("g-1");
  assert.deepEqual(
    captured.map((c) => [c.method, c.url, c.body]),
    [
      ["PATCH", "/api/v1/goals/g-1", { status: "blocked" }],
      ["PATCH", "/api/v1/goals/g-1", { status: "active" }],
      ["PATCH", "/api/v1/goals/g-1", { status: "archived" }],
    ],
  );
});
