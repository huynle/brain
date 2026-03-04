/**
 * Integration Tests: Health API Endpoint
 *
 * Tests the GET /api/v1/health endpoint through the full HTTP stack
 * using createApp() and app.request().
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
// Health Endpoint Tests
// =============================================================================

describe("GET /api/v1/health", () => {
  test("returns 200 status code", async () => {
    const res = await app.request("/api/v1/health");
    expect(res.status).toBe(200);
  });

  test("returns JSON content type", async () => {
    const res = await app.request("/api/v1/health");
    expect(res.headers.get("content-type")).toContain("application/json");
  });

  test("response includes status field with valid value", async () => {
    const res = await app.request("/api/v1/health");
    const data = await res.json();
    expect(data).toHaveProperty("status");
    expect(["healthy", "degraded", "unhealthy"]).toContain(data.status);
  });

  test("response includes version field", async () => {
    const res = await app.request("/api/v1/health");
    const data = await res.json();
    expect(data).toHaveProperty("version");
    expect(typeof data.version).toBe("string");
  });

  test("response includes timestamp as valid ISO string", async () => {
    const res = await app.request("/api/v1/health");
    const data = await res.json();
    expect(data).toHaveProperty("timestamp");
    expect(new Date(data.timestamp).toISOString()).toBe(data.timestamp);
  });

  test("response includes dbAvailable boolean", async () => {
    const res = await app.request("/api/v1/health");
    const data = await res.json();
    expect(data).toHaveProperty("dbAvailable");
    expect(typeof data.dbAvailable).toBe("boolean");
  });

  test("response includes storageLayerAvailable boolean", async () => {
    const res = await app.request("/api/v1/health");
    const data = await res.json();
    expect(data).toHaveProperty("storageLayerAvailable");
    expect(typeof data.storageLayerAvailable).toBe("boolean");
  });

  test("health endpoint is accessible without authentication", async () => {
    // Health is registered before auth middleware, so it should always be accessible
    const res = await app.request("/api/v1/health");
    expect(res.status).toBe(200);
  });
});
