/**
 * Search API - Integration Tests
 *
 * Tests for the search and inject REST API endpoints.
 *
 * NOTE: These tests validate HTTP request/response handling and validation logic.
 * Full search functionality requires zk CLI to be available.
 */

import { describe, test, expect } from "bun:test";
import { Hono } from "hono";
import { createSearchRoutes } from "./search";

// =============================================================================
// Test Setup
// =============================================================================

// Create test app
const app = new Hono();
app.route("/", createSearchRoutes());

// =============================================================================
// Tests: POST /search
// =============================================================================

describe("Search API", () => {
  describe("POST /search - Full-text search", () => {
    test("should reject empty body", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toContainEqual({
        field: "query",
        message: "query is required and must be a string",
      });
    });

    test("should reject missing query", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          type: "plan",
          limit: 10,
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject empty query string", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "   ",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toContainEqual({
        field: "query",
        message: "query cannot be empty",
      });
    });

    test("should reject invalid type", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "authentication",
          type: "invalid-type",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toBeInstanceOf(Array);
    });

    test("should reject invalid status", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "authentication",
          status: "invalid-status",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject non-integer limit", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "authentication",
          limit: 5.5,
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toContainEqual({
        field: "limit",
        message: "limit must be a positive integer",
      });
    });

    test("should reject negative limit", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "authentication",
          limit: -1,
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject non-boolean global", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "authentication",
          global: "yes",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toContainEqual({
        field: "global",
        message: "global must be a boolean",
      });
    });

    test("should reject invalid JSON", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "not valid json",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Bad Request");
      expect(json.message).toBe("Invalid JSON in request body");
    });

    test("should accept valid search request", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "authentication",
          type: "plan",
          status: "active",
          limit: 10,
          global: false,
        }),
      });

      // Might be 200 (success) or 503 (zk not available)
      expect([200, 503]).toContain(res.status);

      const json = await res.json();
      if (res.status === 200) {
        expect(json).toHaveProperty("results");
        expect(json).toHaveProperty("total");
        expect(Array.isArray(json.results)).toBe(true);
      } else {
        expect(json.error).toBe("Service Unavailable");
      }
    });

    test("should trim whitespace from query", async () => {
      const res = await app.request("/search", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "  authentication  ",
        }),
      });

      // Query is trimmed, so should process (200 or 503)
      expect([200, 503]).toContain(res.status);
    });
  });

  // =============================================================================
  // Tests: POST /inject
  // =============================================================================

  describe("POST /inject - Get relevant context", () => {
    test("should reject empty body", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toContainEqual({
        field: "query",
        message: "query is required and must be a string",
      });
    });

    test("should reject missing query", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          maxEntries: 5,
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject empty query string", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toContainEqual({
        field: "query",
        message: "query cannot be empty",
      });
    });

    test("should reject non-integer maxEntries", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "how does authentication work",
          maxEntries: 2.5,
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toContainEqual({
        field: "maxEntries",
        message: "maxEntries must be a positive integer",
      });
    });

    test("should reject zero maxEntries", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "how does authentication work",
          maxEntries: 0,
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject invalid type", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "how does authentication work",
          type: "not-a-valid-type",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject invalid JSON", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{invalid json",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Bad Request");
      expect(json.message).toBe("Invalid JSON in request body");
    });

    test("should accept valid inject request", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "how does authentication work",
          maxEntries: 5,
          type: "plan",
        }),
      });

      // Always 200 - inject handles zk unavailable gracefully
      expect(res.status).toBe(200);

      const json = await res.json();
      expect(json).toHaveProperty("context");
      expect(json).toHaveProperty("entries");
      expect(typeof json.context).toBe("string");
      expect(Array.isArray(json.entries)).toBe(true);
    });

    test("should accept minimal request with just query", async () => {
      const res = await app.request("/inject", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          query: "authentication",
        }),
      });

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json).toHaveProperty("context");
      expect(json).toHaveProperty("entries");
    });
  });
});
