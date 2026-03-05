import {
  afterAll,
  beforeAll,
  beforeEach,
  describe,
  expect,
  test,
} from "bun:test";
import { existsSync, rmSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { Hono } from "hono";
import { closeDatabase, initDatabase } from "../core/db";
import { createApiToken } from "./token-store";
import { accessTokenStore, clientStore } from "../mcp/auth/stores";
import { apiAuth } from "./middleware";

const TEST_DIR = join(tmpdir(), `brain-api-auth-mw-test-${Date.now()}`);

describe("apiAuth middleware", () => {
  beforeAll(() => {
    process.env.BRAIN_DIR = TEST_DIR;
    closeDatabase();
  });

  afterAll(() => {
    closeDatabase();
    if (existsSync(TEST_DIR)) {
      rmSync(TEST_DIR, { recursive: true, force: true });
    }
  });

  beforeEach(() => {
    const db = initDatabase();
    db.run("DELETE FROM api_tokens");
    db.run("DELETE FROM oauth_access_tokens");
    db.run("DELETE FROM oauth_clients");
  });

  /**
   * Helper: create a small Hono app with the middleware applied.
   * Health is registered BEFORE the middleware so it's not protected.
   */
  function createTestApp(enabled: boolean) {
    const app = new Hono();
    app.get("/health", (c) => c.json({ status: "ok" }));
    app.use("*", apiAuth(enabled));
    app.get("/protected", (c) => {
      return c.json({
        authType: c.get("authType"),
        authName: c.get("authName"),
        oauthClientId: c.get("oauthClientId"),
        oauthScope: c.get("oauthScope"),
      });
    });
    return app;
  }

  // =========================================================================
  // Auth disabled
  // =========================================================================

  test("auth disabled: requests pass through without token", async () => {
    const app = createTestApp(false);
    const res = await app.request("/protected");
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.authType).toBeUndefined();
  });

  // =========================================================================
  // Auth enabled, no token
  // =========================================================================

  test("auth enabled, no token: returns 401", async () => {
    const app = createTestApp(true);
    const res = await app.request("/protected");
    expect(res.status).toBe(401);
    const body = await res.json();
    expect(body.error).toBe("unauthorized");
    expect(body.error_description).toContain("Missing");
    expect(res.headers.get("WWW-Authenticate")).toBe(
      'Bearer realm="brain-api"'
    );
  });

  // =========================================================================
  // Auth enabled, valid API token via Bearer header
  // =========================================================================

  test("auth enabled, valid API token via Bearer header: 200 with context", async () => {
    const app = createTestApp(true);
    const { token } = createApiToken("test-cli");

    const res = await app.request("/protected", {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.authType).toBe("api_token");
    expect(body.authName).toBe("test-cli");
  });

  // =========================================================================
  // Auth enabled, valid OAuth token via Bearer header
  // =========================================================================

  test("auth enabled, valid OAuth token via Bearer header: 200 with context", async () => {
    const app = createTestApp(true);

    // Create an OAuth client + access token
    const client = clientStore.create(["http://localhost/callback"], {
      clientName: "test-oauth-client",
      scope: "mcp",
    });
    const oauthToken = accessTokenStore.create(client.client_id, {
      scope: "mcp:read mcp:write",
      ttlSeconds: 3600,
    });

    const res = await app.request("/protected", {
      headers: { Authorization: `Bearer ${oauthToken.token}` },
    });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.authType).toBe("oauth");
    expect(body.oauthClientId).toBe(client.client_id);
    expect(body.oauthScope).toBe("mcp:read mcp:write");
  });

  // =========================================================================
  // Auth enabled, invalid token
  // =========================================================================

  test("auth enabled, invalid token: returns 401", async () => {
    const app = createTestApp(true);
    const res = await app.request("/protected", {
      headers: { Authorization: "Bearer totally-bogus-token" },
    });
    expect(res.status).toBe(401);
    const body = await res.json();
    expect(body.error).toBe("invalid_token");
    expect(body.error_description).toContain("invalid or revoked");
    expect(res.headers.get("WWW-Authenticate")).toContain("Bearer");
  });

  // =========================================================================
  // Token from query parameter
  // =========================================================================

  test("auth enabled, token via query param: 200", async () => {
    const app = createTestApp(true);
    const { token } = createApiToken("query-token");

    const res = await app.request(`/protected?token=${token}`);
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.authType).toBe("api_token");
    expect(body.authName).toBe("query-token");
  });

  // =========================================================================
  // Header takes precedence over query param
  // =========================================================================

  test("auth enabled, header takes precedence over query param", async () => {
    const app = createTestApp(true);
    const { token: headerToken } = createApiToken("header-token");
    const { token: queryToken } = createApiToken("query-token-2");

    const res = await app.request(`/protected?token=${queryToken}`, {
      headers: { Authorization: `Bearer ${headerToken}` },
    });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.authName).toBe("header-token");
  });

  // =========================================================================
  // Health endpoint bypasses auth (registered before middleware)
  // =========================================================================

  test("health endpoint bypasses auth when enabled", async () => {
    const app = createTestApp(true);
    const res = await app.request("/health");
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.status).toBe("ok");
  });

  // =========================================================================
  // Revoked API token is rejected
  // =========================================================================

  test("auth enabled, revoked API token: returns 401", async () => {
    const app = createTestApp(true);
    const { token } = createApiToken("revoked-mw");

    // Revoke it
    const db = initDatabase();
    db.run("UPDATE api_tokens SET revoked_at = ? WHERE name = ?", [
      Date.now(),
      "revoked-mw",
    ]);

    const res = await app.request("/protected", {
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(res.status).toBe(401);
  });

  // =========================================================================
  // Expired OAuth token is rejected
  // =========================================================================

  test("auth enabled, expired OAuth token: returns 401", async () => {
    const app = createTestApp(true);

    const client = clientStore.create(["http://localhost/callback"]);
    const oauthToken = accessTokenStore.create(client.client_id, {
      ttlSeconds: -1, // Already expired
    });

    const res = await app.request("/protected", {
      headers: { Authorization: `Bearer ${oauthToken.token}` },
    });
    expect(res.status).toBe(401);
  });

  // =========================================================================
  // Case-insensitive "bearer" prefix
  // =========================================================================

  test("auth enabled, lowercase 'bearer' prefix works", async () => {
    const app = createTestApp(true);
    const { token } = createApiToken("lowercase-bearer");

    const res = await app.request("/protected", {
      headers: { Authorization: `bearer ${token}` },
    });
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.authType).toBe("api_token");
  });

  // =========================================================================
  // Skip paths configuration
  // =========================================================================

  test("custom skip paths are respected", async () => {
    const app = new Hono();
    app.use("*", apiAuth(true, ["/custom-public"]));
    app.get("/custom-public", (c) => c.json({ public: true }));
    app.get("/protected", (c) => c.json({ ok: true }));

    const res = await app.request("/custom-public");
    expect(res.status).toBe(200);
    const body = await res.json();
    expect(body.public).toBe(true);
  });
});
