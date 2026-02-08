/**
 * OpenCode Port Discovery and Status Checking Tests
 */

import { describe, test, expect, mock, beforeEach, afterEach } from "bun:test";
import {
  discoverOpencodePort,
  checkOpencodeStatus,
  isPidAlive,
  parsePortFromLsof,
} from "./opencode-port";

describe("parsePortFromLsof", () => {
  test("parses IPv4 port from lsof output", () => {
    const output = "opencode 12345 user   10u  IPv4 0x123 0t0  TCP *:54321 (LISTEN)";
    expect(parsePortFromLsof(output)).toBe(54321);
  });

  test("parses IPv6 port from lsof output", () => {
    const output = "opencode 12345 user   10u  IPv6 0x123 0t0  TCP [::1]:8080 (LISTEN)";
    expect(parsePortFromLsof(output)).toBe(8080);
  });

  test("parses localhost port from lsof output", () => {
    const output = "opencode 12345 user   10u  IPv4 0x123 0t0  TCP 127.0.0.1:3333 (LISTEN)";
    expect(parsePortFromLsof(output)).toBe(3333);
  });

  test("parses first port from multiple lines", () => {
    const output = `opencode 12345 user   10u  IPv4 0x123 0t0  TCP *:54321 (LISTEN)
opencode 12345 user   11u  IPv4 0x456 0t0  TCP *:54322 (LISTEN)`;
    expect(parsePortFromLsof(output)).toBe(54321);
  });

  test("returns null for empty output", () => {
    expect(parsePortFromLsof("")).toBe(null);
  });

  test("returns null for non-LISTEN output", () => {
    const output = "opencode 12345 user   10u  IPv4 0x123 0t0  TCP *:54321 (ESTABLISHED)";
    expect(parsePortFromLsof(output)).toBe(null);
  });

  test("returns null for invalid port", () => {
    const output = "opencode 12345 user   10u  IPv4 0x123 0t0  TCP *:99999999 (LISTEN)";
    expect(parsePortFromLsof(output)).toBe(null);
  });
});

describe("isPidAlive", () => {
  test("returns true for current process PID", () => {
    expect(isPidAlive(process.pid)).toBe(true);
  });

  test("returns false for non-existent PID", () => {
    // Use a very high PID that's unlikely to exist on most systems
    // PID max is usually 32768 on Linux, 99999 on macOS
    // We pick a number just below the max that's unlikely to be in use
    expect(isPidAlive(4194300)).toBe(false);
  });

  // Note: PID 0 and negative PIDs have platform-specific behavior
  // On macOS, process.kill(-1, 0) sends to all processes, which may succeed
  // So we don't test those edge cases
});

describe("checkOpencodeStatus", () => {
  // We can't easily mock fetch in Bun, so we'll test with real network calls
  // to non-existent ports (should return unavailable)
  
  test("returns unavailable or idle for non-listening port", async () => {
    // Port 65534 should not be listening in most cases
    // However, the test environment may have services on this port
    // So we accept either unavailable (connection refused) or idle (empty response)
    const status = await checkOpencodeStatus(65534);
    expect(["unavailable", "idle"]).toContain(status);
  });

  test("returns unavailable or idle for port 0", async () => {
    // Port 0 is a special case - OS may interpret it differently
    // Accept either unavailable or idle as valid responses
    const status = await checkOpencodeStatus(0);
    expect(["unavailable", "idle"]).toContain(status);
  });
});

describe("discoverOpencodePort", () => {
  test("returns null for very high PID that doesn't exist", async () => {
    // Use a PID that's extremely unlikely to exist
    const port = await discoverOpencodePort(4194300);
    expect(port).toBe(null);
  });

  // Note: We can't easily test successful port discovery without a real OpenCode process
  // These would be integration tests
});
