import { describe, test, expect } from "bun:test";
import {
  BrainEntrySchema,
  CreateEntryRequestSchema,
  UpdateEntryRequestSchema,
} from "./schemas";

describe("API schemas - cron fields", () => {
  test("CreateEntryRequestSchema accepts cron fields", () => {
    const parsed = CreateEntryRequestSchema.parse({
      type: "cron",
      title: "Nightly Cron",
      content: "Run nightly",
      schedule: "0 2 * * *",
      next_run: "2026-02-23T02:00:00.000Z",
      max_runs: 3,
      starts_at: "2026-02-22T00:00:00.000Z",
      expires_at: "2026-03-01T00:00:00.000Z",
      cron_ids: ["cron_daily"],
      runs: [
        {
          run_id: "20260222-0200",
          status: "completed",
          started: "2026-02-22T02:00:00.000Z",
          completed: "2026-02-22T02:00:08.000Z",
          duration: 8000,
          tasks: 3,
        },
      ],
    });

    expect(parsed.schedule).toBe("0 2 * * *");
    expect(parsed.max_runs).toBe(3);
    expect(parsed.starts_at).toBe("2026-02-22T00:00:00.000Z");
    expect(parsed.expires_at).toBe("2026-03-01T00:00:00.000Z");
    expect(parsed.cron_ids).toEqual(["cron_daily"]);
    expect(parsed.runs?.[0]?.duration).toBe(8000);
  });

  test("UpdateEntryRequestSchema accepts cron-only updates", () => {
    const parsed = UpdateEntryRequestSchema.parse({
      schedule: "*/5 * * * *",
      next_run: "2026-02-22T10:35:00.000Z",
      max_runs: 1,
      run_once_at: "in 2 hours",
      cron_ids: ["cron_fast"],
      runs: [
        {
          run_id: "20260222-1030",
          status: "failed",
          started: "2026-02-22T10:30:00.000Z",
          failed_task: "abc12def",
        },
      ],
    });

    expect(parsed.schedule).toBe("*/5 * * * *");
    expect(parsed.max_runs).toBe(1);
    expect(parsed.run_once_at).toBe("in 2 hours");
    expect(parsed.cron_ids).toEqual(["cron_fast"]);
    expect(parsed.runs?.[0]?.failed_task).toBe("abc12def");
  });

  test("BrainEntrySchema includes cron run shape", () => {
    const parsed = BrainEntrySchema.parse({
      id: "abc12def",
      path: "projects/my-project/cron/abc12def.md",
      title: "Cron Entry",
      type: "cron",
      status: "active",
      content: "body",
      tags: ["cron"],
      schedule: "0 2 * * *",
      next_run: "2026-02-23T02:00:00.000Z",
      max_runs: 2,
      starts_at: "2026-02-22T00:00:00.000Z",
      expires_at: "2026-03-01T00:00:00.000Z",
      runs: [
        {
          run_id: "20260222-0200",
          status: "skipped",
          started: "2026-02-22T02:00:00.000Z",
          skip_reason: "task xyz in_progress",
        },
      ],
    });

    expect(parsed.runs?.[0]?.status).toBe("skipped");
    expect(parsed.runs?.[0]?.skip_reason).toBe("task xyz in_progress");
    expect(parsed.max_runs).toBe(2);
    expect(parsed.starts_at).toBe("2026-02-22T00:00:00.000Z");
    expect(parsed.expires_at).toBe("2026-03-01T00:00:00.000Z");
  });

  test("BrainEntrySchema includes run_finalizations shape", () => {
    const parsed = BrainEntrySchema.parse({
      id: "abc12def",
      path: "projects/my-project/task/abc12def.md",
      title: "Task Entry",
      type: "task",
      status: "completed",
      content: "body",
      tags: ["task"],
      run_finalizations: {
        run_20260225_001: {
          status: "completed",
          finalized_at: "2026-02-25T10:05:00.000Z",
          session_id: "ses_abc123",
        },
      },
    });

    expect(parsed.run_finalizations?.run_20260225_001?.status).toBe("completed");
    expect(parsed.run_finalizations?.run_20260225_001?.finalized_at).toBe(
      "2026-02-25T10:05:00.000Z"
    );
    expect(parsed.run_finalizations?.run_20260225_001?.session_id).toBe("ses_abc123");
  });
});
