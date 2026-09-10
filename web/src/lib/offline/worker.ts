import sqlite3InitModule from "@sqlite.org/sqlite-wasm";
import { EntryDatabase } from "./database";

const sqlite = sqlite3InitModule();
type Pool = Awaited<
  ReturnType<Awaited<typeof sqlite>["installOpfsSAHPoolVfs"]>
>;
const pools = new Map<string, Pool>();
// Each tab releases all file handles between operations. A browser-wide Web
// Lock serializes pool reopen/use/pause, so a second tab cannot corrupt the DB.
self.onmessage = async (event: MessageEvent) => {
  const { id, scope, method, args } = event.data;
  try {
    const result = await navigator.locks.request(
      "brain-entry-db-" + scope,
      async () => {
        const s = await sqlite;
        let pool = pools.get(scope);
        if (!pool) {
          pool = await s.installOpfsSAHPoolVfs({
            name: "brain-" + scope,
            directory: "/brain-offline/" + scope,
            initialCapacity: 6,
          });
          pools.set(scope, pool);
        } else await pool.unpauseVfs();
        let db;
        try {
          db = new pool.OpfsSAHPoolDb("/entries.sqlite3");
          const storage = new EntryDatabase(db);
          const fn = storage[method as keyof EntryDatabase] as (
            ...a: unknown[]
          ) => unknown;
          if (typeof fn !== "function")
            throw new Error("Unknown database operation");
          return fn.apply(storage, args);
        } finally {
          db?.close();
          pool.pauseVfs();
        }
      },
    );
    self.postMessage({ id, result });
  } catch (error) {
    self.postMessage({
      id,
      error: error instanceof Error ? error.message : String(error),
    });
  }
};
