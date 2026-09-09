/**
 * Wire-format tests for resumeTaskWithContext.
 *
 * Same hand-rolled fetch capture as apiGoals.test.ts: assert the exact URL,
 * method, and JSON body the client emits — that shape is the contract with the
 * Go handler HandleResumeWithContext (internal/api/tasks.go). The key
 * behaviors: the endpoint is /resume-with-context, project+task are
 * url-encoded, and prefer_same_session is ALWAYS sent (the Go decode does not
 * default an absent field — ADR 7ihrqpi4).
 */
import { strict as assert } from "node:assert";
import { test, beforeEach, afterEach } from "node:test";

import { resumeTaskWithContext } from "./api";
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

test("resumeTaskWithContext POSTs the encoded resume-with-context URL", async () => {
  nextBody = { task_id: "abc", resumed: true, resume_mode: "rehydrate" };
  const r = await resumeTaskWithContext("brain-api", "abc12def", {
    injected_context: "keep going",
  });
  assert.equal(r.resume_mode, "rehydrate");
  assert.equal(captured[0].method, "POST");
  assert.equal(
    captured[0].url,
    "/api/v1/tasks/brain-api/abc12def/resume-with-context",
  );
});

test("resumeTaskWithContext always sends prefer_same_session:true by default", async () => {
  nextBody = { task_id: "abc", resumed: true, resume_mode: "same_session" };
  await resumeTaskWithContext("p", "t", { injected_context: "hi" });
  assert.deepEqual(captured[0].body, {
    prefer_same_session: true,
    injected_context: "hi",
  });
});

test("resumeTaskWithContext lets the caller override prefer_same_session", async () => {
  nextBody = { task_id: "abc", resumed: true, resume_mode: "rehydrate" };
  await resumeTaskWithContext("p", "t", {
    injected_context: "hi",
    prefer_same_session: false,
    executor_override: "pi",
    force: true,
  });
  assert.deepEqual(captured[0].body, {
    prefer_same_session: false,
    injected_context: "hi",
    executor_override: "pi",
    force: true,
  });
});

test("resumeTaskWithContext url-encodes project and task ids", async () => {
  nextBody = { task_id: "a/b", resumed: true, resume_mode: "rehydrate" };
  await resumeTaskWithContext("proj/x", "a b", { injected_context: "hi" });
  assert.equal(
    captured[0].url,
    "/api/v1/tasks/proj%2Fx/a%20b/resume-with-context",
  );
});
