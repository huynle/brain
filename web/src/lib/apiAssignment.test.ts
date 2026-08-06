/**
 * Wire-format tests for the Phase-8 feature-assignment API wrappers.
 *
 * We install a temporary `globalThis.fetch` mock that captures every
 * request and returns a caller-supplied Response. The tests assert
 * the *exact* URL, method, headers, and JSON body the client emits —
 * that shape is the contract with the Go handlers in
 * internal/api/tasks.go (see the assignment tests in
 * internal/api/runners_test.go for the server side).
 *
 * Why not MSW: node --test doesn't come with jsdom and MSW is overkill
 * for two endpoints. A hand-rolled fetch capture is 30 lines and keeps
 * the test file self-contained.
 */
import { strict as assert } from "node:assert";
import { test, beforeEach, afterEach } from "node:test";
import {
  assignFeatureToRunner,
  clearFeatureAssignment,
  ApiError,
} from "./api";
import { useAuth } from "./auth";

type Captured = {
  url: string;
  method: string;
  headers: Record<string, string>;
  body: unknown;
};

let captured: Captured[] = [];
let nextResponse: {
  status: number;
  body: unknown;
  headers?: Record<string, string>;
} = { status: 200, body: {} };
const originalFetch = globalThis.fetch;

beforeEach(() => {
  captured = [];
  nextResponse = { status: 200, body: {} };
  // Static token so `useAuth.authHeader()` returns a stable header and
  // the 401 refresh branch is never taken during these tests.
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
    const method = (init?.method ?? "GET").toUpperCase();
    const headers: Record<string, string> = {};
    if (init?.headers) {
      const h = init.headers as Record<string, string>;
      for (const k of Object.keys(h)) headers[k] = h[k];
    }
    let bodyJson: unknown = undefined;
    if (init?.body && typeof init.body === "string") {
      try {
        bodyJson = JSON.parse(init.body);
      } catch {
        bodyJson = init.body;
      }
    }
    captured.push({ url, method, headers, body: bodyJson });
    const responseBody =
      typeof nextResponse.body === "string"
        ? nextResponse.body
        : JSON.stringify(nextResponse.body);
    return new Response(responseBody, {
      status: nextResponse.status,
      headers: {
        "Content-Type": "application/json",
        ...(nextResponse.headers ?? {}),
      },
    });
  }) as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// ─── assignFeatureToRunner ──────────────────────────────────────

test("assignFeatureToRunner PUTs the correct URL and default 'assign' intent", async () => {
  nextResponse = {
    status: 200,
    body: {
      project_id: "brain-api",
      feature_id: "auth",
      runner_id: "runner-1",
      source: "manual",
      status: "active",
      assigned_at: "2026-04-29T10:00:00Z",
      updated_at: "2026-04-29T10:00:00Z",
    },
  };

  const result = await assignFeatureToRunner("brain-api", "auth", "runner-1");

  assert.equal(captured.length, 1);
  const req = captured[0];
  assert.equal(req.method, "PUT");
  assert.match(
    req.url,
    /\/api\/v1\/tasks\/brain-api\/features\/auth\/assignment$/,
    "URL must target the assignment endpoint",
  );
  assert.equal(req.headers["Content-Type"], "application/json");
  assert.deepEqual(req.body, {
    runner_id: "runner-1",
    intent: "assign",
  });
  assert.equal(result.runner_id, "runner-1");
  assert.equal(result.status, "active");
});

test("assignFeatureToRunner sends 'reassign' intent when specified", async () => {
  nextResponse = {
    status: 200,
    body: {
      project_id: "brain-api",
      feature_id: "auth",
      runner_id: "runner-2",
      previous_runner: "runner-1",
      source: "manual",
      status: "active",
    },
  };

  await assignFeatureToRunner("brain-api", "auth", "runner-2", {
    intent: "reassign",
  });

  assert.equal(captured.length, 1);
  assert.deepEqual(captured[0].body, {
    runner_id: "runner-2",
    intent: "reassign",
  });
});

test("assignFeatureToRunner includes force flag when requested", async () => {
  nextResponse = {
    status: 200,
    body: {
      project_id: "p",
      feature_id: "f",
      runner_id: "r",
      source: "manual",
      status: "active",
    },
  };

  await assignFeatureToRunner("p", "f", "r", { intent: "assign", force: true });

  assert.deepEqual(captured[0].body, {
    runner_id: "r",
    intent: "assign",
    force: true,
  });
});

test("assignFeatureToRunner omits force when falsy (server default)", async () => {
  nextResponse = {
    status: 200,
    body: {
      project_id: "p",
      feature_id: "f",
      runner_id: "r",
      source: "manual",
      status: "active",
    },
  };

  await assignFeatureToRunner("p", "f", "r", { force: false });

  // force:false should NOT be transmitted — omitempty on the Go side
  // treats absence and false identically, but keeping the body minimal
  // makes wire logs easier to read.
  const body = captured[0].body as Record<string, unknown>;
  assert.equal("force" in body, false, "force:false must be omitted");
});

test("assignFeatureToRunner URL-encodes projectId and featureId segments", async () => {
  nextResponse = {
    status: 200,
    body: {
      project_id: "weird proj",
      feature_id: "sec/nested",
      runner_id: "r",
      source: "manual",
      status: "active",
    },
  };

  await assignFeatureToRunner("weird proj", "sec/nested", "r");

  assert.match(
    captured[0].url,
    /\/tasks\/weird%20proj\/features\/sec%2Fnested\/assignment$/,
    "path segments must be URL-encoded",
  );
});

test("assignFeatureToRunner throws ApiError on 409 conflict", async () => {
  nextResponse = {
    status: 409,
    body: { message: "feature assignment conflict" },
  };

  await assert.rejects(
    () => assignFeatureToRunner("brain-api", "auth", "runner-2"),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 409);
      return true;
    },
  );
});

// ─── clearFeatureAssignment ─────────────────────────────────────

test("clearFeatureAssignment POSTs with {intent: 'clear'} to the clear endpoint", async () => {
  nextResponse = {
    status: 200,
    body: {
      project_id: "brain-api",
      feature_id: "auth",
      previous_runner: "runner-2",
      source: "manual",
      status: "cleared",
      updated_at: "2026-04-29T10:01:00Z",
    },
  };

  const result = await clearFeatureAssignment("brain-api", "auth");

  assert.equal(captured.length, 1);
  const req = captured[0];
  assert.equal(req.method, "POST");
  assert.match(
    req.url,
    /\/api\/v1\/tasks\/brain-api\/features\/auth\/assignment\/clear$/,
  );
  assert.deepEqual(req.body, { intent: "clear" });
  assert.equal(result.status, "cleared");
  assert.equal(result.previous_runner, "runner-2");
});

test("clearFeatureAssignment throws ApiError on 404 (nothing to clear)", async () => {
  nextResponse = {
    status: 404,
    body: { message: "feature assignment not found" },
  };

  await assert.rejects(
    () => clearFeatureAssignment("p", "f"),
    (err: unknown) => {
      assert.ok(err instanceof ApiError);
      assert.equal(err.status, 404);
      return true;
    },
  );
});
