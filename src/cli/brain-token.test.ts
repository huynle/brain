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
import { createApiToken, revokeApiToken } from "../auth";
import { execTokenCommand } from "./brain-token";

const TEST_DIR = join(tmpdir(), `brain-token-cli-test-${Date.now()}`);

describe("brain token CLI", () => {
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

  // =========================================================================
  // token create
  // =========================================================================

  describe("create", () => {
    test("creates token with --name flag", () => {
      const result = execTokenCommand(["create", "--name", "my-token"]);

      expect(result.exitCode).toBe(0);
      expect(result.output).toContain("API Token created:");
      expect(result.output).toContain("Name:  my-token");
      expect(result.output).toContain("Token:");
      expect(result.output).toContain("Save this token");
      expect(result.output).toContain("BRAIN_API_TOKEN=");
    });

    test("creates token with positional name", () => {
      const result = execTokenCommand(["create", "pos-token"]);

      expect(result.exitCode).toBe(0);
      expect(result.output).toContain("Name:  pos-token");
      expect(result.output).toContain("Token:");
    });

    test("returns full 64-char token in output", () => {
      const result = execTokenCommand(["create", "--name", "full-token"]);

      expect(result.exitCode).toBe(0);
      // Extract token from output - it's a 64-char hex string
      const tokenMatch = result.output.match(/Token: ([a-f0-9]{64})/);
      expect(tokenMatch).not.toBeNull();
      expect(tokenMatch![1].length).toBe(64);
    });

    test("fails without name", () => {
      const result = execTokenCommand(["create"]);

      expect(result.exitCode).toBe(1);
      expect(result.output).toContain("name is required");
    });

    test("fails on duplicate name", () => {
      execTokenCommand(["create", "--name", "dup-name"]);
      const result = execTokenCommand(["create", "--name", "dup-name"]);

      expect(result.exitCode).toBe(1);
      expect(result.output.toLowerCase()).toContain("error");
    });
  });

  // =========================================================================
  // token list
  // =========================================================================

  describe("list", () => {
    test("shows empty message when no tokens", () => {
      const result = execTokenCommand(["list"]);

      expect(result.exitCode).toBe(0);
      expect(result.output).toContain("No API tokens found.");
    });

    test("lists tokens with name and prefix", () => {
      createApiToken("list-token-a");
      createApiToken("list-token-b");

      const result = execTokenCommand(["list"]);

      expect(result.exitCode).toBe(0);
      expect(result.output).toContain("list-token-a");
      expect(result.output).toContain("list-token-b");
      expect(result.output).toContain("active");
      expect(result.output).toContain("2 tokens");
    });

    test("shows revoked status for revoked tokens", () => {
      createApiToken("active-tok");
      createApiToken("revoked-tok");
      revokeApiToken("revoked-tok");

      const result = execTokenCommand(["list"]);

      expect(result.exitCode).toBe(0);
      expect(result.output).toContain("active");
      expect(result.output).toContain("revoked");
      expect(result.output).toContain("1 active");
      expect(result.output).toContain("1 revoked");
    });

    test("shows token prefix with ellipsis", () => {
      const token = createApiToken("prefix-tok");
      const prefix = token.token.slice(0, 8);

      const result = execTokenCommand(["list"]);

      expect(result.exitCode).toBe(0);
      expect(result.output).toContain(`${prefix}...`);
    });
  });

  // =========================================================================
  // token revoke
  // =========================================================================

  describe("revoke", () => {
    test("revokes token by name", () => {
      createApiToken("to-revoke");

      const result = execTokenCommand(["revoke", "to-revoke"]);

      expect(result.exitCode).toBe(0);
      expect(result.output).toContain("Revoked");
      expect(result.output).toContain("to-revoke");
    });

    test("fails for unknown token", () => {
      const result = execTokenCommand(["revoke", "nonexistent"]);

      expect(result.exitCode).toBe(1);
      expect(result.output).toContain("Token not found");
      expect(result.output).toContain("nonexistent");
    });

    test("fails without name argument", () => {
      const result = execTokenCommand(["revoke"]);

      expect(result.exitCode).toBe(1);
      expect(result.output).toContain("name is required");
    });
  });

  // =========================================================================
  // help / unknown subcommand
  // =========================================================================

  describe("help", () => {
    test("shows help for unknown subcommand", () => {
      const result = execTokenCommand(["unknown"]);

      expect(result.exitCode).toBe(1);
      expect(result.output).toContain("token create");
      expect(result.output).toContain("token list");
      expect(result.output).toContain("token revoke");
    });

    test("shows help when no subcommand given", () => {
      const result = execTokenCommand([]);

      expect(result.exitCode).toBe(1);
      expect(result.output).toContain("token create");
      expect(result.output).toContain("token list");
      expect(result.output).toContain("token revoke");
    });
  });
});
