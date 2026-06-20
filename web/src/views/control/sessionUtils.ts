import type { OcSession, OpencodeInstance } from "../../lib/types";

export function sessionTime(session: OcSession): number {
  return session.time?.updated ?? session.time?.created ?? 0;
}

export function sortSessionsByExecutedTime(sessions: OcSession[]): OcSession[] {
  return [...sessions].sort((a, b) => {
    const diff = sessionTime(b) - sessionTime(a);
    if (diff !== 0) return diff;
    return b.id.localeCompare(a.id);
  });
}

export function latestInstanceSessionId(instance: OpencodeInstance, sessions: OcSession[]): string | null {
  const sorted = sortSessionsByExecutedTime(sessions);
  if (sorted.length > 0) return sorted[0].id;
  return instance.session_ids?.[0] ?? null;
}
