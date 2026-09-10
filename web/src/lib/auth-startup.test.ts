import test from "node:test";
import assert from "node:assert/strict";
import { useAuth } from "./auth";

test("anonymous startup probes identity without fetching tasks", async (t) => {
  const originalFetch = globalThis.fetch;
  const originalStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (k: string) => values.get(k) ?? null,
      setItem: (k: string, v: string) => values.set(k, v),
      removeItem: (k: string) => values.delete(k),
    },
  });
  t.after(() => {
    globalThis.fetch = originalFetch;
    if (originalStorage)
      Object.defineProperty(globalThis, "localStorage", originalStorage);
    else Reflect.deleteProperty(globalThis, "localStorage");
  });
  const paths: string[] = [];
  globalThis.fetch = async (url) => {
    paths.push(String(url));
    return new Response("{}");
  };
  await useAuth.getState().init();
  assert.deepEqual(paths, ["/api/v1/sync/identity"]);
  assert.equal(useAuth.getState().status, "anonymous");
  // A server requiring login must still gate even an existing anonymous cache.
  globalThis.fetch = async () => new Response("", { status: 401 });
  await useAuth.getState().init();
  assert.equal(useAuth.getState().status, "needs-login");
});
