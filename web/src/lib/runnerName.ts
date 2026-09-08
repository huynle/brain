/**
 * How a runner is spelled in the UI.
 *
 * A runner id is random hex (`runner_26e7c256`), and a machine can host
 * any number of runners, so the id alone cannot tell an operator WHICH
 * box they are about to put work on. `brain runner start -n <name>` sets
 * the `name` label the runner advertises at registration; runners started
 * without one — and every runner from before named runners landed —
 * advertise nothing, so the name is always optional.
 *
 * The id is never dropped in favour of the name: every API call, dispatch
 * lease and log line is keyed by the id, so a named runner reads as
 * "<name> <id>", not as the name alone.
 */

/** Just the labels/id fields — works for RunnerInfo and any row that carries them. */
export interface RunnerNamed {
  runner_id: string;
  labels?: Record<string, string>;
}

/** The runner's advertised name, or "" when it advertises none. */
export function runnerName(r: RunnerNamed): string {
  return r.labels?.name?.trim() || "";
}

/** Single-string form for tooltips and plain-text slots: "pve-1 · runner_26e7c256". */
export function runnerLabel(r: RunnerNamed): string {
  const name = runnerName(r);
  return name ? `${name} · ${r.runner_id}` : r.runner_id;
}
