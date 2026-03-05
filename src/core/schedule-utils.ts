import type { CronRun } from "./types";

export interface CronField {
  any: boolean;
  values: number[];
}

export interface CronSchedule {
  minute: CronField;
  hour: CronField;
  dayOfMonth: CronField;
  month: CronField;
  dayOfWeek: CronField;
}

interface CronFieldSpec {
  min: number;
  max: number;
  name: string;
}

const CRON_SPECS: Record<keyof CronSchedule, CronFieldSpec> = {
  minute: { min: 0, max: 59, name: "minute" },
  hour: { min: 0, max: 23, name: "hour" },
  dayOfMonth: { min: 1, max: 31, name: "dayOfMonth" },
  month: { min: 1, max: 12, name: "month" },
  dayOfWeek: { min: 0, max: 6, name: "dayOfWeek" },
};

function parseInteger(raw: string, fieldName: string): number {
  if (!/^\d+$/.test(raw)) {
    throw new Error(`Invalid ${fieldName} value: ${raw}`);
  }
  return Number.parseInt(raw, 10);
}

function toSortedValues(values: Set<number>): number[] {
  return Array.from(values).sort((a, b) => a - b);
}

function addRange(
  target: Set<number>,
  start: number,
  end: number,
  step: number,
  spec: CronFieldSpec,
  rawToken: string
): void {
  if (start < spec.min || end > spec.max || start > end) {
    throw new Error(`Invalid ${spec.name} token: ${rawToken}`);
  }
  if (step <= 0) {
    throw new Error(`Invalid ${spec.name} step: ${rawToken}`);
  }

  for (let value = start; value <= end; value += step) {
    target.add(value);
  }
}

function parseCronField(rawField: string, spec: CronFieldSpec): CronField {
  const trimmed = rawField.trim();
  if (!trimmed) {
    throw new Error(`Empty ${spec.name} field`);
  }

  const values = new Set<number>();
  const parts = trimmed.split(",");

  for (const part of parts) {
    const token = part.trim();
    if (!token) {
      throw new Error(`Invalid ${spec.name} token: ${rawField}`);
    }

    const segments = token.split("/");
    if (segments.length > 2) {
      throw new Error(`Invalid ${spec.name} token: ${token}`);
    }

    const base = segments[0];
    const step = segments[1] ? parseInteger(segments[1], spec.name) : 1;

    if (base === "*") {
      addRange(values, spec.min, spec.max, step, spec, token);
      continue;
    }

    if (base.includes("-")) {
      const rangeParts = base.split("-");
      if (rangeParts.length !== 2) {
        throw new Error(`Invalid ${spec.name} token: ${token}`);
      }

      const start = parseInteger(rangeParts[0], spec.name);
      const end = parseInteger(rangeParts[1], spec.name);
      addRange(values, start, end, step, spec, token);
      continue;
    }

    if (segments[1]) {
      const start = parseInteger(base, spec.name);
      addRange(values, start, spec.max, step, spec, token);
      continue;
    }

    const value = parseInteger(base, spec.name);
    if (value < spec.min || value > spec.max) {
      throw new Error(`Invalid ${spec.name} value: ${token}`);
    }
    values.add(value);
  }

  const sorted = toSortedValues(values);
  return {
    any: sorted.length === spec.max - spec.min + 1,
    values: sorted,
  };
}

function matchesField(field: CronField, value: number): boolean {
  return field.any || field.values.includes(value);
}

function matchesSchedule(schedule: CronSchedule, date: Date): boolean {
  const minute = date.getUTCMinutes();
  const hour = date.getUTCHours();
  const dayOfMonth = date.getUTCDate();
  const month = date.getUTCMonth() + 1;
  const dayOfWeek = date.getUTCDay();

  if (!matchesField(schedule.minute, minute)) return false;
  if (!matchesField(schedule.hour, hour)) return false;
  if (!matchesField(schedule.month, month)) return false;

  const domMatch = matchesField(schedule.dayOfMonth, dayOfMonth);
  const dowMatch = matchesField(schedule.dayOfWeek, dayOfWeek);

  if (schedule.dayOfMonth.any && schedule.dayOfWeek.any) return true;
  if (schedule.dayOfMonth.any) return dowMatch;
  if (schedule.dayOfWeek.any) return domMatch;
  return domMatch || dowMatch;
}

export function parseCronExpression(expr: string): CronSchedule {
  const fields = expr.trim().split(/\s+/);
  if (fields.length !== 5) {
    throw new Error(`Invalid cron expression: expected 5 fields, got ${fields.length}`);
  }

  return {
    minute: parseCronField(fields[0], CRON_SPECS.minute),
    hour: parseCronField(fields[1], CRON_SPECS.hour),
    dayOfMonth: parseCronField(fields[2], CRON_SPECS.dayOfMonth),
    month: parseCronField(fields[3], CRON_SPECS.month),
    dayOfWeek: parseCronField(fields[4], CRON_SPECS.dayOfWeek),
  };
}

export function getNextRun(schedule: string, after: Date = new Date()): Date {
  const parsed = parseCronExpression(schedule);
  const probe = new Date(after.getTime());
  probe.setUTCSeconds(0, 0);
  probe.setUTCMinutes(probe.getUTCMinutes() + 1);

  const maxIterations = 60 * 24 * 366 * 5;
  for (let i = 0; i < maxIterations; i++) {
    if (matchesSchedule(parsed, probe)) {
      return new Date(probe.getTime());
    }
    probe.setUTCMinutes(probe.getUTCMinutes() + 1);
  }

  throw new Error(`Unable to find next run for schedule: ${schedule}`);
}

export function shouldTrigger(
  cronEntry: { schedule?: string; next_run?: string },
  now: Date
): boolean {
  try {
    if (cronEntry.next_run) {
      const nextRun = new Date(cronEntry.next_run);
      if (!Number.isNaN(nextRun.getTime())) {
        return now.getTime() >= nextRun.getTime();
      }
    }

    if (!cronEntry.schedule) {
      return false;
    }

    const parsed = parseCronExpression(cronEntry.schedule);
    const roundedNow = new Date(now.getTime());
    roundedNow.setUTCSeconds(0, 0);
    return matchesSchedule(parsed, roundedNow);
  } catch {
    return false;
  }
}

export function canRunWithinBounds(
  cronEntry: { max_runs?: number; starts_at?: string; expires_at?: string; runs?: CronRun[] },
  now: Date,
  options: { countAttemptsForMaxRuns?: boolean } = {}
): { canRun: boolean; reason?: string } {
  if (typeof cronEntry.max_runs === "number") {
    const countedRuns = options.countAttemptsForMaxRuns
      ? (cronEntry.runs || []).filter(
          (run) =>
            run.status === "completed" ||
            run.status === "failed" ||
            run.status === "skipped" ||
            run.status === "in_progress" ||
            String(run.status) === "active"
        ).length
      : (cronEntry.runs || []).filter(
          (run) => run.status === "completed" || run.status === "failed"
        ).length;

    if (countedRuns >= cronEntry.max_runs) {
      return {
        canRun: false,
        reason: `max_runs limit reached (${cronEntry.max_runs})`,
      };
    }
  }

  if (cronEntry.starts_at) {
    const startsAt = new Date(cronEntry.starts_at);
    if (!Number.isNaN(startsAt.getTime()) && now.getTime() < startsAt.getTime()) {
      return { canRun: false, reason: `before starts_at (${cronEntry.starts_at})` };
    }
  }

  if (cronEntry.expires_at) {
    const expiresAt = new Date(cronEntry.expires_at);
    if (!Number.isNaN(expiresAt.getTime()) && now.getTime() > expiresAt.getTime()) {
      return { canRun: false, reason: `after expires_at (${cronEntry.expires_at})` };
    }
  }

  return { canRun: true };
}

export function generateRunId(triggerTime: Date): string {
  const year = triggerTime.getUTCFullYear();
  const month = String(triggerTime.getUTCMonth() + 1).padStart(2, "0");
  const day = String(triggerTime.getUTCDate()).padStart(2, "0");
  const hour = String(triggerTime.getUTCHours()).padStart(2, "0");
  const minute = String(triggerTime.getUTCMinutes()).padStart(2, "0");
  const uniqueSuffix = crypto.randomUUID().replace(/-/g, "").slice(0, 6);
  return `${year}${month}${day}-${hour}${minute}-${uniqueSuffix}`;
}
