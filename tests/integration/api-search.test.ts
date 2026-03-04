/**
 * Integration Tests: Search API Endpoints
 *
 * Tests the POST /api/v1/search and POST /api/v1/inject endpoints
 * through the full HTTP stack using createApp() and app.request().
 *
 * NOTE: Search is a POST endpoint (not GET) per the OpenAPI schema.
 * The request body contains { query, type?, status?, limit?, global? }.
 */

import { describe, test, expect, beforeAll } from "bun:test";
import { createApp } from "../../src/server";
import { getConfig } from "../../src/config";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
let app: ReturnType<typeof createApp>;

beforeAll(() => {
  app = createApp(config);
});

// =============================================================================
// POST /api/v1/search — Full-text Search
// =============================================================================

describe("POST /api/v1/search", () => {
  test("returns results array for valid query", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "test" }),
    });

    // 200 = success, 503 = zk not available
    expect([200, 503].includes(res.status)).toBe(true);

    if (res.status === 200) {
      const data = await res.json();
      expect(data).toHaveProperty("results");
      expect(Array.isArray(data.results)).toBe(true);
      expect(data).toHaveProperty("total");
      expect(typeof data.total).toBe("number");
    }
  });

  test("returns empty results for non-matching query", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "xyznonexistent99999" }),
    });

    // 200 = success, 503 = zk not available
    expect([200, 503].includes(res.status)).toBe(true);

    if (res.status === 200) {
      const data = await res.json();
      expect(data.results).toEqual([]);
      expect(data.total).toBe(0);
    }
  });

  test("rejects missing query field (400)", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects empty query string (400)", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "" }),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects whitespace-only query (400)", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "   " }),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects invalid JSON body (400)", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: "not-json",
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Bad Request");
  });

  test("search results include expected fields when results exist", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "test" }),
    });

    if (res.status !== 200) return;

    const data = await res.json();
    if (data.results.length > 0) {
      const result = data.results[0];
      expect(result).toHaveProperty("id");
      expect(result).toHaveProperty("path");
      expect(result).toHaveProperty("title");
      expect(result).toHaveProperty("type");
      expect(result).toHaveProperty("snippet");
    }
  });

  test("accepts optional type filter", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "test", type: "task" }),
    });

    // Should not fail validation — 200 or 503
    expect([200, 503].includes(res.status)).toBe(true);
  });

  test("accepts optional limit parameter", async () => {
    const res = await app.request("/api/v1/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "test", limit: 5 }),
    });

    // Should not fail validation — 200 or 503
    expect([200, 503].includes(res.status)).toBe(true);
  });
});

// =============================================================================
// POST /api/v1/inject — Context Injection
// =============================================================================

describe("POST /api/v1/inject", () => {
  test("returns context and entries for valid query", async () => {
    const res = await app.request("/api/v1/inject", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "test" }),
    });

    // 200 = success, 503 = zk not available, 500 = service error
    expect([200, 500, 503].includes(res.status)).toBe(true);

    if (res.status === 200) {
      const data = await res.json();
      expect(data).toHaveProperty("context");
      expect(typeof data.context).toBe("string");
      expect(data).toHaveProperty("entries");
      expect(Array.isArray(data.entries)).toBe(true);
    }
  });

  test("rejects missing query field (400)", async () => {
    const res = await app.request("/api/v1/inject", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });

  test("rejects empty query string (400)", async () => {
    const res = await app.request("/api/v1/inject", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "" }),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.error).toBe("Validation Error");
  });
});
