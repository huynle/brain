import type { View } from "../store/ui";

export type CommandResult =
  | { type: "view"; view: View }
  | { type: "projectPicker" }
  | { type: "suggest"; suggestions: string[] };

interface CommandSpec {
  primary: string;
  aliases: string[];
  result: Exclude<CommandResult, { type: "suggest" }>;
}

const COMMANDS: CommandSpec[] = [
  { primary: "tasks", aliases: ["task", "ta"], result: { type: "view", view: "tasks" } },
  { primary: "brain", aliases: ["entries", "entry", "notes", "note", "br"], result: { type: "view", view: "brain" } },
  { primary: "automations", aliases: ["automation", "auto", "au"], result: { type: "view", view: "automations" } },
  { primary: "runners", aliases: ["runner", "instances", "instance", "ru"], result: { type: "view", view: "runners" } },
  { primary: "logs", aliases: ["log", "lo"], result: { type: "view", view: "logs" } },
  { primary: "projects", aliases: ["project", "proj", "po"], result: { type: "projectPicker" } },
];

export function commandSuggestions(input: string): string[] {
  const query = clean(input);
  if (!query) return COMMANDS.map((cmd) => cmd.primary);
  const matched = COMMANDS.find((cmd) => [cmd.primary, ...cmd.aliases].some((name) => name.startsWith(query)));
  return matched ? [matched.primary, ...matched.aliases].filter((name) => name.length > query.length || name === matched.primary).slice(0, 3) : [];
}

export function resolveCommand(input: string): CommandResult {
  const query = clean(input);
  for (const cmd of COMMANDS) {
    if (cmd.primary === query || cmd.aliases.includes(query)) return cmd.result;
  }
  return { type: "suggest", suggestions: commandSuggestions(query) };
}

function clean(input: string) {
  return input.trim().replace(/^:/, "").toLowerCase();
}
