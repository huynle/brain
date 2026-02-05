/**
 * OpenCode Port Discovery and Status Checking
 *
 * Utilities for discovering OpenCode HTTP API ports and checking
 * their idle/busy status for blocked task detection.
 */

import { isDebugEnabled } from "./config";

/**
 * OpenCode status as reported by its HTTP API.
 */
export type OpencodeStatus = "idle" | "busy" | "unavailable";

/**
 * Discover the HTTP API port for a running OpenCode process.
 * Uses lsof: `lsof -i -P -n -p <pid> 2>/dev/null | grep LISTEN`
 * Parse output to extract the port number.
 * Returns null if discovery fails.
 */
export async function discoverOpencodePort(pid: number): Promise<number | null> {
  // First check if the PID is valid and alive
  if (pid <= 0 || !isPidAlive(pid)) {
    if (isDebugEnabled()) {
      console.log(`[OpenCodePort] PID ${pid} is not alive, skipping port discovery`);
    }
    return null;
  }

  try {
    // Use lsof to find listening ports for the given PID
    // Note: We need to also grep for the PID to filter correctly,
    // because lsof may return all processes if the -p filter fails
    const result = await Bun.$`lsof -i -P -n -p ${pid} 2>/dev/null | grep -E "^[^ ]+ +${pid} " | grep LISTEN`.text();

    if (!result || result.trim() === "") {
      if (isDebugEnabled()) {
        console.log(`[OpenCodePort] No listening ports found for PID ${pid}`);
      }
      return null;
    }

    // Parse lsof output to extract port number
    // Example line: "opencode 12345 user 10u IPv4 0x123 0t0 TCP *:54321 (LISTEN)"
    // or: "opencode 12345 user 10u IPv6 0x123 0t0 TCP [::1]:54321 (LISTEN)"
    const port = parsePortFromLsof(result);

    if (port !== null && isDebugEnabled()) {
      console.log(`[OpenCodePort] Discovered port ${port} for PID ${pid}`);
    }

    return port;
  } catch (error) {
    // lsof command failed or no LISTEN lines found
    if (isDebugEnabled()) {
      console.log(
        `[OpenCodePort] Failed to discover port for PID ${pid}:`,
        error instanceof Error ? error.message : String(error)
      );
    }
    return null;
  }
}

/**
 * Parse port number from lsof output line.
 * Handles both IPv4 (*:port) and IPv6 ([::]:port) formats.
 */
export function parsePortFromLsof(lsofOutput: string): number | null {
  // Split into lines in case there are multiple
  const lines = lsofOutput.trim().split("\n");

  for (const line of lines) {
    // Look for TCP pattern with port: "*:12345", "[::1]:12345", "127.0.0.1:12345"
    // The port is right before "(LISTEN)"
    const match = line.match(/[:\*](\d+)\s+\(LISTEN\)/);
    if (match && match[1]) {
      const port = parseInt(match[1], 10);
      if (!isNaN(port) && port > 0 && port < 65536) {
        return port;
      }
    }
  }

  return null;
}

/**
 * Check the status of an OpenCode instance via its HTTP API.
 * GET http://localhost:<port>/api/status
 * Response: { status: { type: "idle" | "busy" } }
 * Returns 'unavailable' on error/timeout.
 */
export async function checkOpencodeStatus(port: number): Promise<OpencodeStatus> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 5000); // 5 second timeout

  try {
    const response = await fetch(`http://localhost:${port}/api/status`, {
      signal: controller.signal,
      headers: {
        Accept: "application/json",
      },
    });

    if (!response.ok) {
      if (isDebugEnabled()) {
        console.log(
          `[OpenCodePort] Status check failed for port ${port}: HTTP ${response.status}`
        );
      }
      return "unavailable";
    }

    const data = await response.json() as { status?: { type?: string } };

    // Parse the status response
    const statusType = data?.status?.type;
    if (statusType === "idle" || statusType === "busy") {
      if (isDebugEnabled()) {
        console.log(`[OpenCodePort] Status for port ${port}: ${statusType}`);
      }
      return statusType;
    }

    // Unknown status format
    if (isDebugEnabled()) {
      console.log(
        `[OpenCodePort] Unknown status format for port ${port}:`,
        JSON.stringify(data)
      );
    }
    return "unavailable";
  } catch (error) {
    if (isDebugEnabled()) {
      console.log(
        `[OpenCodePort] Status check error for port ${port}:`,
        error instanceof Error ? error.message : String(error)
      );
    }
    return "unavailable";
  } finally {
    clearTimeout(timeout);
  }
}

/**
 * Check if a PID is still alive using signal 0.
 */
export function isPidAlive(pid: number): boolean {
  try {
    // Sending signal 0 checks if process exists without actually signaling
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}
