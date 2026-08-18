import test from "node:test";
import assert from "node:assert/strict";
import {
  taskMatchesStatusFilter,
  projectMatchesStatusFilter,
} from "./statusFilter";

test("taskMatchesStatusFilter: all matches everything (including missing status)", () => {
  assert.equal(taskMatchesStatusFilter({ status: "in_progress" }, "all"), true);
  assert.equal(taskMatchesStatusFilter({ status: "cancelled" }, "all"), true);
  assert.equal(taskMatchesStatusFilter({}, "all"), true);
});

test("taskMatchesStatusFilter: active matches in_progress only", () => {
  assert.equal(taskMatchesStatusFilter({ status: "in_progress" }, "active"), true);
  assert.equal(taskMatchesStatusFilter({ status: "pending" }, "active"), false);
  assert.equal(taskMatchesStatusFilter({}, "active"), false);
});

test("taskMatchesStatusFilter: ready matches pending only", () => {
  assert.equal(taskMatchesStatusFilter({ status: "pending" }, "ready"), true);
  assert.equal(taskMatchesStatusFilter({ status: "in_progress" }, "ready"), false);
});

test("taskMatchesStatusFilter: blocked matches blocked", () => {
  assert.equal(taskMatchesStatusFilter({ status: "blocked" }, "blocked"), true);
  assert.equal(taskMatchesStatusFilter({ status: "pending" }, "blocked"), false);
});

test("taskMatchesStatusFilter: done matches completed AND validated", () => {
  assert.equal(taskMatchesStatusFilter({ status: "completed" }, "done"), true);
  assert.equal(taskMatchesStatusFilter({ status: "validated" }, "done"), true);
  assert.equal(taskMatchesStatusFilter({ status: "in_progress" }, "done"), false);
});

test("taskMatchesStatusFilter: archived matches archived only", () => {
  assert.equal(taskMatchesStatusFilter({ status: "archived" }, "archived"), true);
  assert.equal(taskMatchesStatusFilter({ status: "completed" }, "archived"), false);
  assert.equal(taskMatchesStatusFilter({ status: "validated" }, "archived"), false);
  assert.equal(taskMatchesStatusFilter({}, "archived"), false);
});

test("taskMatchesStatusFilter: done does NOT match archived (Done stays completed+validated)", () => {
  assert.equal(taskMatchesStatusFilter({ status: "archived" }, "done"), false);
});

test("taskMatchesStatusFilter: all matches archived", () => {
  assert.equal(taskMatchesStatusFilter({ status: "archived" }, "all"), true);
});

test("projectMatchesStatusFilter: all matches every project including empty", () => {
  assert.equal(projectMatchesStatusFilter([], "all"), true);
  assert.equal(projectMatchesStatusFilter([{ status: "in_progress" }], "all"), true);
});

test("projectMatchesStatusFilter: empty project never matches specific filter", () => {
  assert.equal(projectMatchesStatusFilter([], "active"), false);
  assert.equal(projectMatchesStatusFilter([], "blocked"), false);
  assert.equal(projectMatchesStatusFilter([], "ready"), false);
  assert.equal(projectMatchesStatusFilter([], "done"), false);
  assert.equal(projectMatchesStatusFilter([], "archived"), false);
});

test("projectMatchesStatusFilter: archived matches iff a task is archived", () => {
  assert.equal(
    projectMatchesStatusFilter(
      [{ status: "pending" }, { status: "archived" }],
      "archived",
    ),
    true,
  );
  assert.equal(
    projectMatchesStatusFilter([{ status: "completed" }], "archived"),
    false,
  );
});

test("projectMatchesStatusFilter: matches iff at least one task has the status", () => {
  const tasks = [
    { status: "pending" },
    { status: "completed" },
    { status: "blocked" },
  ];
  assert.equal(projectMatchesStatusFilter(tasks, "ready"), true);
  assert.equal(projectMatchesStatusFilter(tasks, "done"), true);
  assert.equal(projectMatchesStatusFilter(tasks, "blocked"), true);
  assert.equal(projectMatchesStatusFilter(tasks, "active"), false);
});
