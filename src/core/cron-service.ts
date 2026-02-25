import type { CronRun, Task } from "./types";
import { buildLookupMaps, resolveDep } from "./task-deps";

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
  cronEntry: { schedule: string; next_run?: string },
  now: Date
): boolean {
  try {
    if (cronEntry.next_run) {
      const nextRun = new Date(cronEntry.next_run);
      if (!Number.isNaN(nextRun.getTime())) {
        return now.getTime() >= nextRun.getTime();
      }
    }

    const parsed = parseCronExpression(cronEntry.schedule);
    const roundedNow = new Date(now.getTime());
    roundedNow.setUTCSeconds(0, 0);
    return matchesSchedule(parsed, roundedNow);
  } catch {
    return false;
  }
}

export function resolveCronPipeline(cronId: string, tasks: Task[]): Task[] {
  if (tasks.length === 0) return [];

  const maps = buildLookupMaps(tasks);
  const byId = maps.byId;

  const seedIds = tasks
    .filter((task) => task.cron_ids.includes(cronId))
    .map((task) => task.id);

  if (seedIds.length === 0) return [];

  const included = new Set<string>(seedIds);
  const visitedUpstream = new Set<string>();
  const queue = [...seedIds];
  while (queue.length > 0) {
    const current = queue.shift()!;
    if (visitedUpstream.has(current)) continue;
    visitedUpstream.add(current);

    const currentTask = byId.get(current);
    if (!currentTask) continue;

    for (const depRef of currentTask.depends_on || []) {
      const depId = resolveDep(depRef, maps);
      if (!depId) continue;

      const depTask = byId.get(depId);
      if (!depTask) continue;

      if (depTask.cron_ids.includes(cronId) && !included.has(depId)) {
        included.add(depId);
      }

      if (!visitedUpstream.has(depId)) {
        queue.push(depId);
      }
    }
  }

  const dependents = new Map<string, string[]>();
  for (const task of tasks) {
    for (const depRef of task.depends_on || []) {
      const depId = resolveDep(depRef, maps);
      if (!depId) continue;
      const list = dependents.get(depId) || [];
      list.push(task.id);
      dependents.set(depId, list);
    }
  }

  const taskIndex = new Map<string, number>(
    tasks.map((task, index) => [task.id, index])
  );

  const indegree = new Map<string, number>();
  for (const id of included) {
    indegree.set(id, 0);
  }

  for (const id of included) {
    const task = byId.get(id);
    if (!task) continue;
    let count = 0;
    for (const depRef of task.depends_on || []) {
      const depId = resolveDep(depRef, maps);
      if (depId && included.has(depId)) {
        count++;
      }
    }
    indegree.set(id, count);
  }

  const zeroQueue = Array.from(included)
    .filter((id) => (indegree.get(id) || 0) === 0)
    .sort((a, b) => (taskIndex.get(a) || 0) - (taskIndex.get(b) || 0));

  const orderedIds: string[] = [];

  while (zeroQueue.length > 0) {
    const id = zeroQueue.shift()!;
    orderedIds.push(id);

    for (const dependentId of dependents.get(id) || []) {
      if (!included.has(dependentId)) continue;
      const nextDegree = (indegree.get(dependentId) || 0) - 1;
      indegree.set(dependentId, nextDegree);
      if (nextDegree === 0) {
        zeroQueue.push(dependentId);
        zeroQueue.sort((a, b) => (taskIndex.get(a) || 0) - (taskIndex.get(b) || 0));
      }
    }
  }

  if (orderedIds.length < included.size) {
    const unresolved = Array.from(included)
      .filter((id) => !orderedIds.includes(id))
      .sort((a, b) => (taskIndex.get(a) || 0) - (taskIndex.get(b) || 0));
    orderedIds.push(...unresolved);
  }

  return orderedIds
    .map((id) => byId.get(id))
    .filter((task): task is Task => Boolean(task));
}

export function canTriggerPipeline(
  pipelineTasks: Task[],
  cronRuns: CronRun[] = []
): { canTrigger: boolean; reason?: string } {
  if (pipelineTasks.length === 0) {
    return { canTrigger: false, reason: "no tasks in pipeline" };
  }

  const activeRun = cronRuns.find(
    (run) => run.status === "in_progress" || String(run.status) === "active"
  );
  if (activeRun) {
    return {
      canTrigger: false,
      reason: `cron run ${activeRun.run_id} already in_progress`,
    };
  }

  const inProgress = pipelineTasks.find((task) => task.status === "in_progress");
  if (inProgress) {
    return {
      canTrigger: false,
      reason: `task ${inProgress.id} already in_progress`,
    };
  }

  return { canTrigger: true };
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
