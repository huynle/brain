/**
 * Tests for lib/automationRuns — the client-side reader for the audit
 * body the server writes in `createRunAudit`.
 *
 * The fixtures below are verbatim bodies from a live store, so a change
 * to the server's audit format breaks these rather than silently
 * degrading every run row to blanks.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  durationLabel,
  latestRunByAutomation,
  outcomeLabel,
  parseAutomationRun,
  parseAutomationRuns,
  runCountByAutomation,
  runOutcome,
  runTime,
  triggerLabel,
} from "./automationRuns";
import type { BrainEntry } from "./types";

/** A cron run that generated one task — the common case. */
const GENERATED = [
  "## Automation Run Audit",
  "",
  "automation_id: wvnbqz7w",
  "automation_path: projects/demo/automation/wvnbqz7w.md",
  "project: demo",
  "trigger_type: cron",
  "trigger_event: * * * * *",
  "dedup_key: automation:cron:wvnbqz7w:202608211049",
  "started_at: 2026-08-21T10:49:19Z",
  "completed_at: 2026-08-21T10:49:21Z",
  "duration_ms: 2400",
  "",
  "### Trigger Payload Summary",
  "- project_id: demo",
  "",
  "### Generated Tasks",
  "- f9eskoor",
].join("\n");

function mkEntry(over: Partial<BrainEntry> = {}): BrainEntry {
  return {
    id: over.id ?? "run1",
    path: over.path ?? "projects/demo/automation_run/run1.md",
    title: "Automation Run: wvnbqz7w",
    type: "automation_run",
    status: over.status ?? "queued",
    content: over.content ?? GENERATED,
    created: over.created ?? "2026-08-21T10:49:19Z",
    ...over,
  } as BrainEntry;
}

test("parses every audit field", () => {
  const run = parseAutomationRun(mkEntry());
  assert.equal(run.id, "run1");
  assert.equal(run.automationId, "wvnbqz7w");
  assert.equal(run.automationPath, "projects/demo/automation/wvnbqz7w.md");
  assert.equal(run.project, "demo");
  assert.equal(run.triggerType, "cron");
  assert.equal(run.triggerEvent, "* * * * *");
  assert.equal(run.dedupKey, "automation:cron:wvnbqz7w:202608211049");
  assert.equal(run.startedAt, "2026-08-21T10:49:19Z");
  assert.equal(run.durationMs, 2400);
  assert.deepEqual(run.taskIds, ["f9eskoor"]);
  assert.deepEqual(run.payload, [{ key: "project_id", value: "demo" }]);
});

test("a cron expression's own colons never split the field", () => {
  const run = parseAutomationRun(
    mkEntry({ content: GENERATED.replace("* * * * *", "0 3 * * *  # 03:00") }),
  );
  assert.equal(run.triggerEvent, "0 3 * * *  # 03:00");
});

test("an error message keeps its colons", () => {
  const run = parseAutomationRun(
    mkEntry({ content: GENERATED + "\nerror: exec: workdir not found" }),
  );
  // Trailing lines land after the last section heading, so the error has
  // to be read from the preamble to be seen at all.
  assert.equal(run.error, "");
  const inHead = parseAutomationRun(
    mkEntry({
      content: GENERATED.replace(
        "duration_ms: 2400",
        "duration_ms: 2400\nerror: exec: workdir not found",
      ),
    }),
  );
  assert.equal(inHead.error, "exec: workdir not found");
  assert.equal(runOutcome(inHead), "error");
});

test("\"- none\" is not a task id", () => {
  const run = parseAutomationRun(
    mkEntry({ content: GENERATED.replace("- f9eskoor", "- none") }),
  );
  assert.deepEqual(run.taskIds, []);
  assert.equal(runOutcome(run), "noop");
});

test("a payload line is never read as an audit field", () => {
  const run = parseAutomationRun(
    mkEntry({
      content: GENERATED.replace(
        "- project_id: demo",
        "- project_id: demo\n- trigger_type: not-the-field",
      ),
    }),
  );
  assert.equal(run.triggerType, "cron");
  assert.equal(run.payload.length, 2);
});

test("skip reason drives the outcome and its label", () => {
  const run = parseAutomationRun(
    mkEntry({
      status: "skipped",
      content: GENERATED.replace(
        "duration_ms: 2400",
        "duration_ms: 0\nskip_reason: cooldown",
      ).replace("- f9eskoor", "- none"),
    }),
  );
  assert.equal(runOutcome(run), "skipped");
  assert.equal(outcomeLabel(run), "skipped: cooldown");
});

test("the body outranks a stale entry status", () => {
  // Runs written as "queued" have been observed sitting at "blocked"
  // after unrelated bulk edits; the audit body is the honest record.
  const run = parseAutomationRun(mkEntry({ status: "blocked" }));
  assert.equal(run.entryStatus, "blocked");
  assert.equal(runOutcome(run), "generated");
  assert.equal(outcomeLabel(run), "generated 1 task");
});

test("a malformed body still yields a placed, timed row", () => {
  const run = parseAutomationRun(
    mkEntry({ content: "totally unexpected", created: "2026-08-21T10:49:19Z" }),
  );
  assert.equal(run.automationId, "");
  assert.equal(run.durationMs, undefined);
  assert.equal(durationLabel(run), "");
  assert.equal(runTime(run), "2026-08-21T10:49:19Z");
  assert.equal(triggerLabel(run), "manual");
});

test("durations render at human scale", () => {
  const at = (ms: number) =>
    durationLabel(parseAutomationRun(mkEntry({ content: `duration_ms: ${ms}` })));
  assert.equal(at(0), "0ms");
  assert.equal(at(940), "940ms");
  assert.equal(at(2400), "2.4s");
  assert.equal(at(45000), "45s");
  assert.equal(at(90000), "1.5m");
});

test("runs sort newest first, and fold to one row per automation", () => {
  const entries = [
    mkEntry({ id: "old", content: GENERATED }),
    mkEntry({
      id: "new",
      content: GENERATED.replace(
        "started_at: 2026-08-21T10:49:19Z",
        "started_at: 2026-08-21T11:00:00Z",
      ),
    }),
    mkEntry({
      id: "other",
      content: GENERATED.replace("automation_id: wvnbqz7w", "automation_id: zz11"),
    }),
  ];
  const runs = parseAutomationRuns(entries);
  assert.deepEqual(
    runs.map((r) => r.id),
    ["new", "old", "other"],
  );

  const latest = latestRunByAutomation(runs);
  assert.equal(latest.get("wvnbqz7w")?.id, "new");
  assert.equal(latest.get("zz11")?.id, "other");

  const counts = runCountByAutomation(runs);
  assert.equal(counts.get("wvnbqz7w"), 2);
  assert.equal(counts.get("zz11"), 1);
});
