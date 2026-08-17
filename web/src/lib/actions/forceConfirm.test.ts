/**
 * Tests for lib/actions/forceConfirm — the promise bridge behind the force
 * dialog. What matters: a request parks until settled, settles with the
 * user's answer, and a second request auto-declines the first instead of
 * queueing (single-dialog rule).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { forceConfirmFor, useForceConfirm } from "./forceConfirm";

function reset() {
  useForceConfirm.setState({ pending: null, _resolve: null });
}

const SPEC = {
  title: "Runner online — force?",
  body: "Force does the thing anyway.",
  confirmLabel: "Force",
};

test("request exposes the spec and resolves with settle(true)", async () => {
  reset();
  const p = useForceConfirm.getState().request(SPEC);
  assert.equal(useForceConfirm.getState().pending?.title, SPEC.title);

  useForceConfirm.getState().settle(true);
  assert.equal(await p, true);
  assert.equal(useForceConfirm.getState().pending, null);
});

test("settle(false) resolves the request as declined", async () => {
  reset();
  const p = useForceConfirm.getState().request(SPEC);
  useForceConfirm.getState().settle(false);
  assert.equal(await p, false);
});

test("a second request auto-declines the first rather than queueing", async () => {
  reset();
  const first = useForceConfirm.getState().request(SPEC);
  const second = useForceConfirm
    .getState()
    .request({ ...SPEC, title: "Second" });

  assert.equal(await first, false);
  assert.equal(useForceConfirm.getState().pending?.title, "Second");

  useForceConfirm.getState().settle(true);
  assert.equal(await second, true);
});

test("settle with nothing pending is a no-op", () => {
  reset();
  useForceConfirm.getState().settle(true);
  assert.equal(useForceConfirm.getState().pending, null);
});

test("forceConfirmFor prepends the server message to the body", async () => {
  reset();
  const confirm = forceConfirmFor(SPEC);
  const p = confirm("task claimed by runner amos-1");
  const pending = useForceConfirm.getState().pending;
  assert.ok(pending);
  assert.match(pending.body, /^task claimed by runner amos-1\. /);
  assert.match(pending.body, /Force does the thing anyway\./);
  useForceConfirm.getState().settle(true);
  assert.equal(await p, true);
});

test("forceConfirmFor keeps the plain body when the server message is empty", () => {
  reset();
  const confirm = forceConfirmFor(SPEC);
  void confirm("");
  assert.equal(useForceConfirm.getState().pending?.body, SPEC.body);
  useForceConfirm.getState().settle(false);
});
