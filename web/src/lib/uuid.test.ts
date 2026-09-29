import { strict as assert } from "node:assert";
import { test } from "node:test";
import { createUUID } from "./uuid";

test("createUUID works when randomUUID is unavailable on plain HTTP", () => {
  const values = new Uint8Array(16).map((_, index) => index);
  const id = createUUID({ getRandomValues: <T extends ArrayBufferView>(target: T) => {
    new Uint8Array(target.buffer, target.byteOffset, target.byteLength).set(values);
    return target;
  } });
  assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
});

test("createUUID uses native randomUUID when available", () => {
  assert.equal(createUUID({ randomUUID: () => "native-id" }), "native-id");
});
