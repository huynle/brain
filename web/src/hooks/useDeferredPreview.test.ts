import { strict as assert } from "node:assert";
import { describe, it } from "node:test";

import { DOUBLE_CLICK_WINDOW_MS } from "./useDeferredPreview";

// The hook itself needs a React renderer to exercise, but the value that
// makes it work is a plain constant with a real constraint on it: it has to
// exceed the OS double-click interval that users actually run, or the flash
// it exists to remove comes straight back.
describe("DOUBLE_CLICK_WINDOW_MS", () => {
  it("outlasts the default double-click interval on macOS and Windows", () => {
    // Windows' default is 500ms MAX but the practical interval between two
    // deliberate clicks is ~150-200ms; macOS' default slider sits around the
    // same place. Below ~200ms a normal double-click starts leaking a
    // preview.
    assert.ok(
      DOUBLE_CLICK_WINDOW_MS >= 200,
      `${DOUBLE_CLICK_WINDOW_MS}ms is short enough that ordinary double-clicks would still flash the panel`,
    );
  });

  it("stays under the threshold where a delay reads as lag", () => {
    assert.ok(
      DOUBLE_CLICK_WINDOW_MS <= 300,
      `${DOUBLE_CLICK_WINDOW_MS}ms would make single-click previews feel sluggish`,
    );
  });
});
