import { describe, it } from "node:test";
import assert from "node:assert/strict";
import { getByPath, setByPath, deepEqual } from "./objectPath";

describe("getByPath", () => {
  it("reads top-level", () => {
    assert.equal(getByPath({ a: 1 }, "a"), 1);
  });
  it("reads nested", () => {
    assert.equal(getByPath({ a: { b: { c: 42 } } }, "a.b.c"), 42);
  });
  it("returns undefined for missing", () => {
    assert.equal(getByPath({ a: 1 }, "b"), undefined);
    assert.equal(getByPath({ a: { b: 1 } }, "a.c"), undefined);
    assert.equal(getByPath({ a: null }, "a.b"), undefined);
  });
  it("handles empty input", () => {
    assert.equal(getByPath(null, "x"), undefined);
    assert.equal(getByPath(undefined, "x"), undefined);
  });
});

describe("setByPath", () => {
  it("sets top-level and returns new object", () => {
    const orig = { a: 1, b: 2 };
    const out = setByPath(orig, "a", 99);
    assert.equal(out.a, 99);
    assert.equal(out.b, 2);
    assert.equal(orig.a, 1); // unchanged
  });
  it("sets nested and clones only ancestors", () => {
    const orig = { a: { b: { c: 1 } }, other: { x: 1 } };
    const out = setByPath(orig, "a.b.c", 99);
    assert.equal(out.a.b.c, 99);
    assert.equal(orig.a.b.c, 1); // original untouched
    assert.equal(out.other, orig.other); // sibling not cloned
  });
  it("creates missing intermediates as objects", () => {
    const out = setByPath({}, "a.b.c", 1);
    assert.deepEqual(out, { a: { b: { c: 1 } } });
  });
});

describe("deepEqual", () => {
  it("primitives", () => {
    assert.ok(deepEqual(1, 1));
    assert.ok(!deepEqual(1, 2));
    assert.ok(deepEqual("a", "a"));
  });
  it("treats null and undefined as equal", () => {
    assert.ok(deepEqual(null, undefined));
    assert.ok(deepEqual(undefined, null));
  });
  it("objects", () => {
    assert.ok(deepEqual({ a: 1, b: 2 }, { b: 2, a: 1 }));
    assert.ok(!deepEqual({ a: 1 }, { a: 2 }));
    assert.ok(!deepEqual({ a: 1 }, { a: 1, b: 2 }));
  });
  it("arrays", () => {
    assert.ok(deepEqual([1, 2, 3], [1, 2, 3]));
    assert.ok(!deepEqual([1, 2, 3], [3, 2, 1]));
    assert.ok(!deepEqual([1, 2], { 0: 1, 1: 2 }));
  });
  it("nested", () => {
    assert.ok(
      deepEqual(
        { a: [{ b: 1 }, { c: 2 }] },
        { a: [{ b: 1 }, { c: 2 }] },
      ),
    );
    assert.ok(
      !deepEqual({ a: [{ b: 1 }] }, { a: [{ b: 2 }] }),
    );
  });
});
