let worker: Worker | undefined;
let next = 0;
const waiting = new Map<
  number,
  { resolve: (v: unknown) => void; reject: (e: Error) => void }
>();
export function cacheScope(): string {
  let scope = localStorage.getItem("brain.offline.scope");
  if (!scope) {
    scope = crypto.randomUUID();
    localStorage.setItem("brain.offline.scope", scope);
  }
  return scope;
}
export function database<T>(method: string, ...args: unknown[]): Promise<T> {
  return databaseFor<T>(cacheScope(), method, ...args);
}
export function databaseFor<T>(
  scope: string,
  method: string,
  ...args: unknown[]
): Promise<T> {
  if (!worker) {
    worker = new Worker(new URL("./worker.ts", import.meta.url), {
      type: "module",
    });
    worker.onmessage = ({ data }) => {
      const p = waiting.get(data.id);
      if (!p) return;
      waiting.delete(data.id);
      if (data.error) p.reject(new Error(data.error));
      else p.resolve(data.result);
    };
    worker.onerror = () => {
      for (const p of waiting.values())
        p.reject(new Error("Offline database worker failed"));
      waiting.clear();
      worker?.terminate();
      worker = undefined;
    };
  }
  const id = ++next;
  return new Promise<T>((resolve, reject) => {
    waiting.set(id, { resolve: resolve as (v: unknown) => void, reject });
    worker!.postMessage({ id, scope, method, args });
  });
}
