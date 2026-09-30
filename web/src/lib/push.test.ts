/** Pure urlBase64ToUint8Array tests (node --test, no DOM). */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { urlBase64ToUint8Array } from "./push";

test("decodes a padded standard base64 string", () => {
  // "hi" → base64 "aGk=".
  const out = urlBase64ToUint8Array("aGk=");
  assert.deepEqual(Array.from(out), [104, 105]);
});

test("restores missing padding", () => {
  // "hi" without the trailing "=" must still decode.
  const out = urlBase64ToUint8Array("aGk");
  assert.deepEqual(Array.from(out), [104, 105]);
});

test("restores URL-safe alphabet (- and _)", () => {
  // Bytes [251, 255] encode to standard "+/8=" and URL-safe "-_8".
  const standard = urlBase64ToUint8Array("+/8=");
  const urlSafe = urlBase64ToUint8Array("-_8");
  assert.deepEqual(Array.from(standard), [251, 255]);
  assert.deepEqual(Array.from(urlSafe), [251, 255]);
});

test("produces a Uint8Array of the right length for a VAPID-sized key", () => {
  // A typical VAPID public key is 65 bytes (uncompressed P-256 point).
  const key = "BLc4xRzKlKORKWlbdgFaBrEJvXhO"; // arbitrary URL-safe sample
  const out = urlBase64ToUint8Array(key);
  assert.ok(out instanceof Uint8Array);
  assert.equal(out.length, Math.floor((key.length * 3) / 4));
});

test("empty input yields an empty array", () => {
  assert.deepEqual(Array.from(urlBase64ToUint8Array("")), []);
});
