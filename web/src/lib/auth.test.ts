import { test, beforeEach, afterEach } from "node:test";
import assert from "node:assert/strict";
import { useAuth } from "./auth";
const originalFetch = globalThis.fetch;
const originalStorage = Object.getOwnPropertyDescriptor(
  globalThis,
  "localStorage",
);
beforeEach(() => {
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (k: string) => values.get(k) ?? null,
      setItem: (k: string, v: string) => values.set(k, String(v)),
      removeItem: (k: string) => values.delete(k),
    },
  });
  localStorage.setItem("brain.access_token", "old");
  localStorage.setItem("brain.refresh_token", "refresh-old");
  localStorage.setItem("brain.auth_mode", "password");
  localStorage.setItem("brain.offline.scope", "a".repeat(64));
  useAuth.setState({
    status: "authenticated",
    token: "old",
    mode: "password",
    error: null,
  });
});
afterEach(() => {
  globalThis.fetch = originalFetch;
  if (originalStorage)
    Object.defineProperty(globalThis, "localStorage", originalStorage);
  else Reflect.deleteProperty(globalThis, "localStorage");
});
const tokens = () =>
  Response.json({
    access_token: "new",
    refresh_token: "refresh-new",
    expires_in: 3600,
  });
test("concurrent unauthorized requests share one rotating refresh", async () => {
  let calls = 0;
  globalThis.fetch = async () => {
    calls++;
    await new Promise((r) => setTimeout(r, 5));
    return tokens();
  };
  assert.deepEqual(
    await Promise.all(
      Array.from({ length: 8 }, () => useAuth.getState().onUnauthorized("old")),
    ),
    Array(8).fill(true),
  );
  assert.equal(calls, 1);
  assert.equal(useAuth.getState().token, "new");
  assert.equal(localStorage.getItem("brain.refresh_token"), "refresh-new");
});
test("late anonymous or old-token rejection cannot refresh or clear a newer login", async () => {
  let calls = 0;
  globalThis.fetch = async () => {
    calls++;
    return tokens();
  };
  assert.equal(await useAuth.getState().onUnauthorized(null), true);
  assert.equal(await useAuth.getState().onUnauthorized("previous-login"), true);
  assert.equal(calls, 0);
  assert.equal(useAuth.getState().token, "old");
});
test("refresh completion cannot restore a logged-out session", async () => {
  let release!: (r: Response) => void;
  globalThis.fetch = async (input) =>
    String(input).includes("logout")
      ? new Response(null, { status: 204 })
      : await new Promise<Response>((r) => (release = r));
  const pending = useAuth.getState().onUnauthorized("old");
  useAuth.getState().logout();
  release(tokens());
  await pending;
  assert.equal(useAuth.getState().status, "needs-login");
  assert.equal(localStorage.getItem("brain.access_token"), null);
});
test("refresh rejection still clears the current invalid session", async () => {
  globalThis.fetch = async () => new Response(null, { status: 401 });
  assert.equal(await useAuth.getState().onUnauthorized("old"), false);
  assert.equal(useAuth.getState().status, "needs-login");
  assert.equal(localStorage.getItem("brain.refresh_token"), null);
});
test("initialization and unauthorized handling share the same refresh", async () => {
  let calls = 0;
  globalThis.fetch = async () => {
    calls++;
    await new Promise((r) => setTimeout(r, 5));
    return tokens();
  };
  await Promise.all([
    useAuth.getState().init(),
    useAuth.getState().onUnauthorized("old"),
    useAuth.getState().init(),
  ]);
  assert.equal(calls, 1);
  assert.equal(useAuth.getState().token, "new");
  assert.equal(useAuth.getState().status, "authenticated");
});
test("an old startup auth probe cannot undo a completed password login", async () => {
  localStorage.removeItem("brain.access_token");
  useAuth.setState({ status: "loading", token: null, mode: null });
  let release!: (r: Response) => void;
  globalThis.fetch = async (input, init) => {
    if (String(input).endsWith("/auth/login")) return tokens();
    if ((init?.headers as Record<string, string>)?.Authorization)
      return Response.json({ scope: "b".repeat(64) });
    return await new Promise<Response>((r) => (release = r));
  };
  const pending = useAuth.getState().init();
  await useAuth.getState().loginPassword("admin", "fixture");
  release(new Response(null, { status: 401 }));
  await pending;
  assert.equal(useAuth.getState().status, "authenticated");
  assert.equal(localStorage.getItem("brain.offline.scope"), "b".repeat(64));
});
test("API retries a stale anonymous 401 using the new login without refreshing it", async () => {
  const { api } = await import("./api");
  useAuth.setState({ status: "needs-login", token: null, mode: null });
  let release!: (r: Response) => void;
  let calls = 0;
  globalThis.fetch = async (_input, init) => {
    calls++;
    if (calls === 1) return await new Promise<Response>((r) => (release = r));
    assert.equal(
      (init?.headers as Record<string, string>).Authorization,
      "Bearer new-login",
    );
    return Response.json({ ok: true });
  };
  const pending = api("/api/v1/reminders");
  localStorage.setItem("brain.access_token", "new-login");
  useAuth.setState({
    status: "authenticated",
    token: "new-login",
    mode: "password",
  });
  release(new Response(null, { status: 401 }));
  assert.deepEqual(await pending, { ok: true });
  assert.equal(calls, 2);
  assert.equal(useAuth.getState().token, "new-login");
});
test('an anonymous server that starts requiring auth returns to sign-in',async()=>{
 useAuth.setState({status:'anonymous',token:null,mode:null});
 localStorage.removeItem('brain.access_token');
 localStorage.setItem('brain.offline.anonymous','true');
 assert.equal(await useAuth.getState().onUnauthorized(null),false);
 assert.equal(useAuth.getState().status,'needs-login');
 assert.equal(localStorage.getItem('brain.offline.anonymous'),null);
});
