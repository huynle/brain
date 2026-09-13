import test from "node:test";
import assert from "node:assert/strict";
import { onLoopbackHost } from "./sync";

// onLoopbackHost gates offlineAvailable(): on a loopback origin the server is
// always reachable, so the dashboard must NOT route reads through the OPFS
// first-sync (which otherwise blocks on "Loading projects…"). See the comment
// on offlineAvailable() in sync.ts.

function withHostname<T>(hostname: string | undefined, fn: () => T): T {
  const original = (globalThis as { window?: unknown }).window;
  if (hostname === undefined) {
    delete (globalThis as { window?: unknown }).window;
  } else {
    (globalThis as { window?: unknown }).window = {
      location: { hostname },
    };
  }
  try {
    return fn();
  } finally {
    if (original === undefined) delete (globalThis as { window?: unknown }).window;
    else (globalThis as { window?: unknown }).window = original;
  }
}

test("onLoopbackHost: loopback hostnames are treated as always-online", () => {
  for (const h of ["localhost", "127.0.0.1", "::1", "[::1]", "brain.localhost"]) {
    assert.equal(withHostname(h, onLoopbackHost), true, `${h} should be loopback`);
  }
});

test("onLoopbackHost: remote hostnames keep offline sync enabled", () => {
  for (const h of ["ai.orion.us.lmco.com", "192.168.1.10", "example.com"]) {
    assert.equal(withHostname(h, onLoopbackHost), false, `${h} should not be loopback`);
  }
});

test("onLoopbackHost: no window (SSR/worker) is not loopback", () => {
  assert.equal(withHostname(undefined, onLoopbackHost), false);
});
