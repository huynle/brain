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
import { closeDatabase, initDatabase } from "../core/db";
import {
  createApiToken,
  validateApiToken,
  listApiTokens,
  revokeApiToken,
  getApiTokenByName,
} from "./token-store";

const TEST_DIR = join(tmpdir(), `brain-token-store-test-${Date.now()}`);

describe("API token store", () => {
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
  });

  test("createApiToken creates a valid token and returns it", () => {
    const result = createApiToken("my-cli-token");

    expect(result.token).toBeDefined();
    expect(result.token.length).toBe(64); // 32 bytes = 64 hex chars
    expect(result.name).toBe("my-cli-token");
    expect(result.createdAt).toBeGreaterThan(0);
  });

  test("createApiToken with duplicate name throws", () => {
    createApiToken("duplicate-name");

    expect(() => createApiToken("duplicate-name")).toThrow();
  });

  test("validateApiToken returns valid for a good token", () => {
    const { token } = createApiToken("valid-token");

    const result = validateApiToken(token);

    expect(result.valid).toBe(true);
    expect(result.name).toBe("valid-token");
  });

  test("validateApiToken returns invalid for a revoked token", () => {
    const { token } = createApiToken("revoked-token");
    revokeApiToken("revoked-token");

    const result = validateApiToken(token);

    expect(result.valid).toBe(false);
    expect(result.name).toBeUndefined();
  });

  test("validateApiToken returns invalid for a non-existent token", () => {
    const result = validateApiToken("nonexistent-token-value");

    expect(result.valid).toBe(false);
    expect(result.name).toBeUndefined();
  });

  test("validateApiToken updates last_used_at", () => {
    const { token } = createApiToken("usage-tracked");

    // Before validation, last_used_at should be null
    const before = getApiTokenByName("usage-tracked");
    expect(before?.lastUsedAt).toBeNull();

    validateApiToken(token);

    const after = getApiTokenByName("usage-tracked");
    expect(after?.lastUsedAt).toBeGreaterThan(0);
  });

  test("listApiTokens returns tokens with truncated prefix", () => {
    const { token: token1 } = createApiToken("token-a");
    createApiToken("token-b");

    const list = listApiTokens();

    expect(list.length).toBe(2);

    const tokenA = list.find((t) => t.name === "token-a");
    expect(tokenA).toBeDefined();
    expect(tokenA!.tokenPrefix).toBe(token1.slice(0, 8));
    expect(tokenA!.tokenPrefix.length).toBe(8);
    expect(tokenA!.createdAt).toBeGreaterThan(0);
    expect(tokenA!.revokedAt).toBeNull();
  });

  test("revokeApiToken soft-deletes by name", () => {
    createApiToken("to-revoke");

    const revoked = revokeApiToken("to-revoke");
    expect(revoked).toBe(true);

    const info = getApiTokenByName("to-revoke");
    expect(info?.revokedAt).toBeGreaterThan(0);
  });

  test("revokeApiToken by prefix", () => {
    const { token } = createApiToken("prefix-revoke");
    const prefix = token.slice(0, 8);

    const revoked = revokeApiToken(prefix);
    expect(revoked).toBe(true);

    const info = getApiTokenByName("prefix-revoke");
    expect(info?.revokedAt).toBeGreaterThan(0);
  });

  test("revokeApiToken returns false for unknown name", () => {
    const revoked = revokeApiToken("nonexistent");
    expect(revoked).toBe(false);
  });

  test("getApiTokenByName returns token metadata", () => {
    createApiToken("metadata-token");

    const info = getApiTokenByName("metadata-token");

    expect(info).not.toBeNull();
    expect(info!.name).toBe("metadata-token");
    expect(info!.createdAt).toBeGreaterThan(0);
    expect(info!.lastUsedAt).toBeNull();
    expect(info!.revokedAt).toBeNull();
    expect(info!.tokenPrefix.length).toBe(8);
  });

  test("getApiTokenByName returns null for unknown name", () => {
    const info = getApiTokenByName("nonexistent");
    expect(info).toBeNull();
  });
});
