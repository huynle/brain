import test from "node:test";
import assert from "node:assert/strict";
import { BrainClient, BrainError } from "../dist/index.js";

// Parity with Go Error(): only the stable machine code and HTTP status; never
// the server message, request ID or field details.
test("BrainError message includes stable code only", () => {
  const cases = [
    [new BrainError("invalid_configuration"), "brain: invalid_configuration"],
    [new BrainError("redirect_refused"), "brain: redirect_refused"],
    [new BrainError("not_found", 404, "req-SECRET", "SECRET-BODY", false, [{field: "f", message: "SECRET-FIELD"}]), "brain: not_found (HTTP 404)"],
    [new BrainError("Bad Code SECRET\n", 500), "brain: request failed (HTTP 500)"],
    [new BrainError("", 502), "brain: request failed (HTTP 502)"],
    [new BrainError(""), "brain: request failed"],
  ];
  for (const [error, want] of cases) {
    assert.equal(error.message, want);
    assert.ok(!String(error).includes("SECRET") && !JSON.stringify(error).includes("SECRET"));
  }
});

test("bad base URL reports invalid_configuration", () => {
  assert.throws(() => new BrainClient({baseUrl: "ftp://example.com"}), (e) => e instanceof BrainError && e.message === "brain: invalid_configuration");
});
