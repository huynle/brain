/**
 * Tests for lib/actions/sessionActions.
 *
 * The things worth pinning: abort addresses the NEWEST discovered
 * session (the same rule instanceSessionRef encodes), the three
 * destructive verbs all confirm but none demand type-to-confirm (kill
 * is not a data delete — the transcript survives), pi/script rows get
 * honest executor reasons instead of a dead chat verb, and the watch
 * label flips to "View transcript" once the process has exited.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  abortSessionBlockedReason,
  abortTaskBlockedReason,
  buildSessionActions,
  killInstanceBlockedReason,
  latestSessionId,
  openTaskBlockedReason,
  sessionName,
  watchSessionBlockedReason,
  type SessionActionContext,
} from "./sessionActions";
import { isEnabled } from "./types";
import type { OpencodeInstance } from "../types";

function mkInst(over: Partial<OpencodeInstance> = {}): OpencodeInstance {
  return {
    instance_id: "inst-1",
    runner_id: "runner-a",
    kind: "task",
    status: "busy",
    project_id: "brain-api",
    task_id: "task-1",
    title: "Fix auth",
    executor: "opencode",
    pid: 4242,
    workdir: "/work/brain",
    session_ids: ["ses-old", "ses-new"],
    ...over,
  };
}

function recorder() {
  const calls: string[] = [];
  const ctx: SessionActionContext = {
    openSession: (i) => void calls.push(`openSession:${i.instance_id}`),
    openProcesses: (i) => void calls.push(`openProcesses:${i.instance_id}`),
    openTask: (i) => void calls.push(`openTask:${i.task_id}`),
    abortSession: async (i, sid) =>
      void calls.push(`abortSession:${i.instance_id}:${sid}`),
    abortTask: async (i) => void calls.push(`abortTask:${i.task_id}`),
    killInstance: async (i) => void calls.push(`killInstance:${i.instance_id}`),
    copyText: async (label, value) => void calls.push(`copy:${label}:${value}`),
  };
  return { calls, ctx };
}

function byId(inst: OpencodeInstance, ctx: SessionActionContext) {
  return new Map(buildSessionActions(inst, ctx).map((a) => [a.id, a]));
}

const ALL_IDS = [
  "abort-session",
  "abort-task",
  "copy-pid",
  "copy-workdir",
  "watch",
  "processes",
  "open-task",
  "kill",
];

// ─── presence ──────────────────────────────────────────────────────

test("every session verb is present regardless of status and kind", () => {
  const { ctx } = recorder();
  for (const status of ["starting", "idle", "busy", "exited"] as const) {
    for (const kind of ["task", "adhoc"] as const) {
      const ids = buildSessionActions(mkInst({ status, kind }), ctx).map(
        (a) => a.id,
      );
      for (const expected of ALL_IDS) {
        assert.ok(
          ids.includes(expected),
          `status=${status} kind=${kind}: missing action ${expected}`,
        );
      }
    }
  }
});

// ─── watch ─────────────────────────────────────────────────────────

test("watch is enabled live, reads View transcript once exited", () => {
  const { ctx } = recorder();
  const live = byId(mkInst({ status: "busy" }), ctx).get("watch")!;
  assert.equal(live.label, "Watch session");
  assert.ok(isEnabled(live));

  const exited = byId(mkInst({ status: "exited" }), ctx).get("watch")!;
  assert.equal(exited.label, "View transcript");
  assert.ok(isEnabled(exited));
});

test("watch is disabled with a reason when no transcript can exist", () => {
  // Exited before any session was discovered — nothing to read.
  assert.match(
    watchSessionBlockedReason(mkInst({ status: "exited", session_ids: [] })),
    /no transcript/i,
  );
  // Live pi process — there will never be a session to watch.
  assert.match(
    watchSessionBlockedReason(
      mkInst({ executor: "pi", session_ids: [] }),
    ),
    /pi/,
  );
});

// ─── abort session ─────────────────────────────────────────────────

test("abort session targets the newest discovered session", async () => {
  const { calls, ctx } = recorder();
  const abort = byId(mkInst({ session_ids: ["s1", "s2"] }), ctx).get(
    "abort-session",
  )!;
  assert.ok(isEnabled(abort));
  await abort.run();
  assert.deepEqual(calls, ["abortSession:inst-1:s2"]);
});

test("abort session gating: exited, no session yet, wrong executor", () => {
  assert.match(
    abortSessionBlockedReason(mkInst({ status: "exited" })),
    /exited/i,
  );
  assert.match(
    abortSessionBlockedReason(mkInst({ status: "starting", session_ids: [] })),
    /no session/i,
  );
  assert.match(
    abortSessionBlockedReason(mkInst({ executor: "pi", session_ids: [] })),
    /abort the task instead/i,
  );
});

// ─── abort task ────────────────────────────────────────────────────

test("abort task requires a linked task and a live process", () => {
  assert.equal(abortTaskBlockedReason(mkInst()), "");
  assert.match(
    abortTaskBlockedReason(
      mkInst({ kind: "adhoc", task_id: undefined }),
    ),
    /no task/i,
  );
  assert.match(
    abortTaskBlockedReason(mkInst({ status: "exited" })),
    /exited/i,
  );
});

// ─── kill ──────────────────────────────────────────────────────────

test("kill is enabled unless the process already exited", () => {
  assert.equal(killInstanceBlockedReason(mkInst({ status: "idle" })), "");
  assert.match(
    killInstanceBlockedReason(mkInst({ status: "exited" })),
    /already exited/i,
  );
});

test("destructive verbs confirm, none demand type-to-confirm", () => {
  const { ctx } = recorder();
  const actions = byId(mkInst(), ctx);
  for (const id of ["abort-session", "abort-task", "kill"]) {
    const a = actions.get(id)!;
    assert.ok(a.danger, `${id} should render destructive`);
    assert.ok(a.confirm, `${id} should confirm`);
    assert.equal(
      a.confirm?.typeToConfirm,
      undefined,
      `${id} is recoverable — type-to-confirm is reserved for data loss`,
    );
  }
});

test("kill confirm explains the task-side consequence for task rows", () => {
  const { ctx } = recorder();
  assert.match(
    byId(mkInst(), ctx).get("kill")!.confirm!.body,
    /resume it/i,
  );
  assert.doesNotMatch(
    byId(mkInst({ kind: "adhoc", task_id: undefined }), ctx).get("kill")!
      .confirm!.body,
    /resume it/i,
  );
});

// ─── open linked task ──────────────────────────────────────────────

test("open task needs both a task id and its project", () => {
  assert.equal(openTaskBlockedReason(mkInst()), "");
  assert.match(
    openTaskBlockedReason(mkInst({ kind: "adhoc", task_id: undefined })),
    /no linked task/i,
  );
  assert.match(
    openTaskBlockedReason(mkInst({ project_id: undefined })),
    /project/i,
  );
});

// ─── clipboard ─────────────────────────────────────────────────────

test("copy verbs route the shown values and disable when absent", async () => {
  const { calls, ctx } = recorder();
  const actions = byId(mkInst(), ctx);
  await actions.get("copy-pid")!.run();
  await actions.get("copy-workdir")!.run();
  assert.deepEqual(calls, ["copy:PID:4242", "copy:Workdir:/work/brain"]);

  const bare = byId(mkInst({ pid: undefined, workdir: undefined }), ctx);
  assert.match(bare.get("copy-pid")!.disabledReason!, /no pid/i);
  assert.match(bare.get("copy-workdir")!.disabledReason!, /no workdir/i);
});

// ─── routing ───────────────────────────────────────────────────────

test("each verb routes to its context effect", async () => {
  const { calls, ctx } = recorder();
  const actions = byId(mkInst(), ctx);
  await actions.get("watch")!.run();
  await actions.get("processes")!.run();
  await actions.get("open-task")!.run();
  await actions.get("abort-session")!.run();
  await actions.get("abort-task")!.run();
  await actions.get("kill")!.run();
  assert.deepEqual(calls, [
    "openSession:inst-1",
    "openProcesses:inst-1",
    "openTask:task-1",
    "abortSession:inst-1:ses-new",
    "abortTask:task-1",
    "killInstance:inst-1",
  ]);
});

// ─── naming / session selection ────────────────────────────────────

test("sessionName precedence: title, ad-hoc marker, task id, instance id", () => {
  assert.equal(sessionName(mkInst()), "Fix auth");
  assert.equal(sessionName(mkInst({ title: "  " })), "task-1");
  assert.equal(
    sessionName(mkInst({ title: undefined, kind: "adhoc" })),
    "ad-hoc session inst-1",
  );
  assert.equal(
    sessionName(mkInst({ title: undefined, task_id: undefined })),
    "inst-1",
  );
});

test("latestSessionId is the newest discovered session", () => {
  assert.equal(latestSessionId(mkInst()), "ses-new");
  assert.equal(latestSessionId(mkInst({ session_ids: [] })), undefined);
  assert.equal(latestSessionId(mkInst({ session_ids: undefined })), undefined);
});
