/**
 * Tests for lib/actions/goalActions.
 *
 * The things worth pinning: pause/resume are a status-aware pair with
 * exactly one enabled at a time, delete demands typing the goal id,
 * archive confirms without that friction, and the create-request
 * assembler emits exactly the wire shape the Go handlers decode
 * (internal/api/goals.go + internal/types/automation.go).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  archiveGoalBlockedReason,
  assembleCreateGoalRequest,
  buildGoalActions,
  goalCreateValidationError,
  goalStatusLabel,
  pauseGoalBlockedReason,
  resumeGoalBlockedReason,
  runGoalBlockedReason,
  summarizeGoalRun,
  type GoalActionContext,
  type GoalCreateForm,
} from "./goalActions";
import { isEnabled } from "./types";
import type { GoalReconcileAudit, GoalSummary } from "../types";

function mkGoal(over: Partial<GoalSummary> = {}): GoalSummary {
  return {
    entry_id: "e-1",
    goal_id: "keep-auth-green",
    title: "Keep auth green",
    project: "brain-api",
    status: "active",
    ...over,
  };
}

function recorder() {
  const calls: string[] = [];
  const ctx: GoalActionContext = {
    runGoal: async (g) => void calls.push(`run:${g.goal_id}`),
    pauseGoal: async (g) => void calls.push(`pause:${g.goal_id}`),
    resumeGoal: async (g) => void calls.push(`resume:${g.goal_id}`),
    archiveGoal: async (g) => void calls.push(`archive:${g.goal_id}`),
    deleteGoal: async (g) => void calls.push(`delete:${g.goal_id}`),
    openEdit: (g) => void calls.push(`edit:${g.goal_id}`),
    openDetails: (g) => void calls.push(`details:${g.goal_id}`),
  };
  return { calls, ctx };
}

function byId(goal: GoalSummary, ctx: GoalActionContext) {
  return new Map(buildGoalActions(goal, ctx).map((a) => [a.id, a]));
}

// ─── presence ──────────────────────────────────────────────────────

test("every goal verb is present regardless of status", () => {
  const { ctx } = recorder();
  for (const status of ["active", "blocked", "completed", "archived"]) {
    const ids = buildGoalActions(mkGoal({ status }), ctx).map((a) => a.id);
    for (const expected of [
      "run",
      "pause",
      "resume",
      "archive",
      "edit",
      "details",
      "delete",
    ]) {
      assert.ok(
        ids.includes(expected),
        `status=${status}: missing action ${expected}`,
      );
    }
  }
});

// ─── pause/resume pair ─────────────────────────────────────────────

test("active goal: pause enabled, resume disabled with reason", () => {
  const { ctx } = recorder();
  const actions = byId(mkGoal({ status: "active" }), ctx);
  assert.ok(isEnabled(actions.get("pause")!));
  assert.ok(!isEnabled(actions.get("resume")!));
  assert.match(actions.get("resume")!.disabledReason!, /already active/i);
});

test("paused goal: resume enabled, pause disabled with reason", () => {
  const { ctx } = recorder();
  const actions = byId(mkGoal({ status: "blocked" }), ctx);
  assert.ok(isEnabled(actions.get("resume")!));
  assert.ok(!isEnabled(actions.get("pause")!));
  assert.match(actions.get("pause")!.disabledReason!, /paused/i);
});

test("completed goal: resume reads Reactivate; pause disabled", () => {
  const { ctx } = recorder();
  const actions = byId(mkGoal({ status: "completed" }), ctx);
  assert.equal(actions.get("resume")!.label, "Reactivate goal");
  assert.ok(isEnabled(actions.get("resume")!));
  assert.ok(!isEnabled(actions.get("pause")!));
});

test("exactly one of pause/resume is enabled for every status", () => {
  const { ctx } = recorder();
  for (const status of ["active", "blocked", "completed", "archived"]) {
    const actions = byId(mkGoal({ status }), ctx);
    const enabled = [actions.get("pause")!, actions.get("resume")!].filter(
      isEnabled,
    );
    assert.equal(enabled.length, 1, `status=${status}`);
  }
});

// ─── run / archive gating ──────────────────────────────────────────

test("run is enabled for active/paused/completed, blocked for archived", () => {
  assert.equal(runGoalBlockedReason(mkGoal({ status: "active" })), "");
  assert.equal(runGoalBlockedReason(mkGoal({ status: "blocked" })), "");
  assert.equal(runGoalBlockedReason(mkGoal({ status: "completed" })), "");
  assert.match(
    runGoalBlockedReason(mkGoal({ status: "archived" })),
    /archived/,
  );
});

test("archive disabled only when already archived", () => {
  assert.equal(archiveGoalBlockedReason(mkGoal({ status: "active" })), "");
  assert.match(
    archiveGoalBlockedReason(mkGoal({ status: "archived" })),
    /already archived/i,
  );
});

test("pause/resume reasons stay in sync with the label map", () => {
  assert.equal(goalStatusLabel("blocked"), "Paused");
  assert.match(
    pauseGoalBlockedReason(mkGoal({ status: "completed" })),
    /completed/i,
  );
  assert.equal(resumeGoalBlockedReason(mkGoal({ status: "archived" })), "");
});

// ─── confirms ──────────────────────────────────────────────────────

test("delete requires typing the goal id; archive confirms without it", () => {
  const { ctx } = recorder();
  const actions = byId(mkGoal(), ctx);
  const del = actions.get("delete")!;
  assert.ok(del.danger);
  assert.equal(del.confirm?.typeToConfirm, "keep-auth-green");
  const archive = actions.get("archive")!;
  assert.ok(archive.confirm);
  assert.equal(archive.confirm?.typeToConfirm, undefined);
});

// ─── routing ───────────────────────────────────────────────────────

test("each verb routes to its context effect", async () => {
  const { calls, ctx } = recorder();
  const actions = byId(mkGoal({ status: "blocked" }), ctx);
  await actions.get("run")!.run();
  await actions.get("resume")!.run();
  await actions.get("edit")!.run();
  await actions.get("details")!.run();
  await actions.get("archive")!.run();
  await actions.get("delete")!.run();
  assert.deepEqual(calls, [
    "run:keep-auth-green",
    "resume:keep-auth-green",
    "edit:keep-auth-green",
    "details:keep-auth-green",
    "archive:keep-auth-green",
    "delete:keep-auth-green",
  ]);
});

// ─── run summaries ─────────────────────────────────────────────────

function mkAudit(over: Partial<GoalReconcileAudit> = {}): GoalReconcileAudit {
  return {
    timestamp: "2026-08-11T10:00:00Z",
    goal_id: "keep-auth-green",
    triggering_event: "manual",
    decision: "noop",
    reason: "work in progress",
    ...over,
  };
}

test("summarizeGoalRun maps decisions to tones", () => {
  assert.equal(
    summarizeGoalRun(mkAudit({ decision: "complete" })).kind,
    "success",
  );
  assert.equal(summarizeGoalRun(mkAudit({ decision: "block" })).kind, "warning");
  assert.equal(summarizeGoalRun(mkAudit({ decision: "noop" })).kind, "info");
  const needWork = summarizeGoalRun(
    mkAudit({ decision: "need_work", generated_task_id: "task-42" }),
  );
  assert.match(needWork.message, /task-42/);
  const steer = summarizeGoalRun(
    mkAudit({ decision: "steer", sessions_steered: 2, sessions_skipped: 1 }),
  );
  assert.match(steer.message, /Steered 2 live sessions/);
});

// ─── create-request assembly ───────────────────────────────────────

function mkForm(over: Partial<GoalCreateForm> = {}): GoalCreateForm {
  return {
    project: "brain-api",
    title: "Keep auth green",
    steeringEnabled: true,
    ...over,
  };
}

test("title and project are required", () => {
  assert.equal(goalCreateValidationError(mkForm()), "");
  assert.match(goalCreateValidationError(mkForm({ title: "  " })), /title/i);
  assert.match(
    goalCreateValidationError(mkForm({ project: "" })),
    /project/i,
  );
  assert.match(
    goalCreateValidationError(mkForm({ steeringCooldownMinutes: -5 })),
    /cooldown/i,
  );
});

test("minimal form assembles a minimal request", () => {
  const req = assembleCreateGoalRequest(mkForm());
  assert.deepEqual(req, {
    project: "brain-api",
    title: "Keep auth green",
    config: { id: "" }, // server derives the slug from the title
    action: { type: "prompt" },
  });
});

test("full form assembles every field into the Go wire shape", () => {
  const req = assembleCreateGoalRequest(
    mkForm({
      featureId: "auth-system",
      taskId: "task-9",
      criteria: " tests pass ",
      validation: "just check",
      prompt: "fix it",
      agent: "tdd-dev",
      model: "anthropic/claude",
      executor: "pi",
      steeringEnabled: false,
      steeringCooldownMinutes: 30,
    }),
  );
  assert.deepEqual(req, {
    project: "brain-api",
    feature_id: "auth-system",
    title: "Keep auth green",
    config: {
      id: "",
      criteria: "tests pass",
      validation: "just check",
      task_id: "task-9",
      steering: { enabled: false, cooldown_minutes: 30 },
    },
    action: {
      type: "prompt",
      direct_prompt: "fix it",
      agent: "tdd-dev",
      model: "anthropic/claude",
      executor: "pi",
    },
  });
});

test("default steering (enabled, no cooldown) is omitted entirely", () => {
  const req = assembleCreateGoalRequest(
    mkForm({ steeringEnabled: true, steeringCooldownMinutes: undefined }),
  );
  assert.equal(req.config.steering, undefined);
});

test("non-default cooldown keeps steering with enabled omitted", () => {
  const req = assembleCreateGoalRequest(
    mkForm({ steeringEnabled: true, steeringCooldownMinutes: 45 }),
  );
  assert.deepEqual(req.config.steering, { cooldown_minutes: 45 });
});
