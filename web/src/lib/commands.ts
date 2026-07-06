// k9s-style command grammar for the ":" bar.
//
//   input  ::= name (ws arg)*
//   name   ::= token            — exact primary/alias match wins, else fuzzy;
//                                 ambiguity returns suggestions, never a guess
//   arg    ::= token | '"' quoted '"'
//
// Pause/resume target sub-grammar (args in any order):
//   kind    ::= "tasks" (default) | "autos" | "automations"
//   scope   ::= "all" | "*"      — all projects
//             | "."              — the active project (default)
//             | <project fuzzy>
//
// Pure module: resolution takes an injected CommandCtx (projects list) and
// returns a typed outcome; execution lives in the CommandBar component.

import type { View } from "../store/ui";
import { fuzzyBest, fuzzyResolve } from "./fuzzy";
import { tokenizeFilter } from "./filter";

export interface CommandCtx {
  projects: string[];
}

export interface Suggestion {
  /** Full command text to insert (without the leading ":"). */
  insert: string;
  label: string;
  detail?: string;
}

export type CommandOutcome =
  | { type: "navigate"; view: View; project?: string }
  | { type: "projectPicker" }
  | { type: "projectSwitch"; project: string }
  | { type: "preset"; preset: "done" | "ready" | "merge-ready"; project?: string }
  | {
      type: "pauseResume";
      verb: "pause" | "resume";
      kind: "tasks" | "autos";
      scope: "all" | "active" | "project";
      project?: string;
    }
  | { type: "suggest"; suggestions: Suggestion[] }
  | { type: "error"; message: string; suggestions: Suggestion[] };

interface CommandSpec {
  primary: string;
  aliases: string[];
  describe: string;
  /** Argument hint shown in suggestions, e.g. "[project]". */
  argHint?: string;
  resolve: (args: string[], ctx: CommandCtx) => CommandOutcome;
}

function viewCommand(view: View, primary: string, aliases: string[], describe: string): CommandSpec {
  return {
    primary,
    aliases,
    describe,
    argHint: "[project]",
    resolve: (args, ctx) => {
      if (args.length === 0) return { type: "navigate", view };
      const project = resolveProject(args[0], ctx);
      if (typeof project !== "string") return project;
      return { type: "navigate", view, project };
    },
  };
}

function resolveProject(query: string, ctx: CommandCtx): string | CommandOutcome {
  const project = fuzzyResolve(ctx.projects, shortName, query) ?? fuzzyResolve(ctx.projects, (p) => p, query);
  if (project) return project;
  const suggestions = projectSuggestions("", query, ctx);
  return {
    type: "error",
    message: suggestions.length ? `Ambiguous project "${query}"` : `No project matches "${query}"`,
    suggestions,
  };
}

function shortName(id: string): string {
  return id.split(/[/\\]/).pop() || id;
}

const KIND_WORDS = new Set(["autos", "automations", "auto"]);
const TASKS_WORDS = new Set(["tasks", "task"]);
const ALL_WORDS = new Set(["all", "*"]);

function pauseResume(verb: "pause" | "resume"): CommandSpec {
  return {
    primary: verb,
    aliases: [],
    describe: `${verb === "pause" ? "Pause" : "Resume"} tasks or automations (all / active / a project)`,
    argHint: "[tasks|autos] [all|.|project]",
    resolve: (args, ctx) => {
      let kind: "tasks" | "autos" = "tasks";
      let scope: "all" | "active" | "project" = "active";
      let project: string | undefined;
      for (const arg of args) {
        const a = arg.toLowerCase();
        if (KIND_WORDS.has(a)) {
          kind = "autos";
        } else if (TASKS_WORDS.has(a)) {
          kind = "tasks";
        } else if (ALL_WORDS.has(a)) {
          scope = "all";
        } else if (a === ".") {
          scope = "active";
        } else {
          const resolved = resolveProject(arg, ctx);
          if (typeof resolved !== "string") return resolved;
          scope = "project";
          project = resolved;
        }
      }
      return { type: "pauseResume", verb, kind, scope, project };
    },
  };
}

const COMMANDS: CommandSpec[] = [
  viewCommand("tasks", "tasks", ["task", "ta", "t"], "Tasks view"),
  viewCommand("brain", "brain", ["entries", "entry", "notes", "note", "br", "b"], "Brain entries"),
  viewCommand("automations", "automations", ["automation", "auto", "au", "a"], "Automations"),
  viewCommand("runners", "runners", ["runner", "instances", "instance", "ru"], "Runners"),
  viewCommand("logs", "logs", ["log", "lo"], "Server logs"),
  {
    primary: "projects",
    aliases: ["project", "proj", "po"],
    describe: "Switch project (picker without an argument)",
    argHint: "[project]",
    resolve: (args, ctx) => {
      if (args.length === 0) return { type: "projectPicker" };
      const project = resolveProject(args[0], ctx);
      if (typeof project !== "string") return project;
      return { type: "projectSwitch", project };
    },
  },
  pauseResume("pause"),
  pauseResume("resume"),
  {
    primary: "done",
    aliases: ["history", "completed"],
    describe: "Executed tasks ordered by completion date",
    argHint: "[project]",
    resolve: (args, ctx) => {
      if (args.length === 0) return { type: "preset", preset: "done" };
      const project = resolveProject(args[0], ctx);
      if (typeof project !== "string") return project;
      return { type: "preset", preset: "done", project };
    },
  },
  {
    primary: "ready",
    aliases: [],
    describe: "Tasks ready to execute (status:ready filter)",
    argHint: "[project]",
    resolve: (args, ctx) => {
      if (args.length === 0) return { type: "preset", preset: "ready" };
      const project = resolveProject(args[0], ctx);
      if (typeof project !== "string") return project;
      return { type: "preset", preset: "ready", project };
    },
  },
  {
    primary: "merge-ready",
    aliases: ["merge"],
    describe: "Completed features that probably need merging",
    argHint: "[project]",
    resolve: (args, ctx) => {
      if (args.length === 0) return { type: "preset", preset: "merge-ready" };
      const project = resolveProject(args[0], ctx);
      if (typeof project !== "string") return project;
      return { type: "preset", preset: "merge-ready", project };
    },
  },
];

// Extension point: later phases append specs here (done/ready/feature/
// merge-ready presets) — one entry each, suggestions and help come free.
export function registerCommand(spec: CommandSpec): void {
  COMMANDS.push(spec);
}

function findExact(name: string): CommandSpec | null {
  const n = name.toLowerCase();
  return COMMANDS.find((c) => c.primary === n || c.aliases.includes(n)) ?? null;
}

function findFuzzy(name: string): CommandSpec | Suggestion[] {
  const exact = findExact(name);
  if (exact) return exact;
  const resolved = fuzzyResolve(COMMANDS, (c) => c.primary, name, 1);
  if (resolved) return resolved;
  return commandNameSuggestions(name);
}

function commandNameSuggestions(partial: string): Suggestion[] {
  const ranked = partial
    ? fuzzyBest(COMMANDS, (c) => c.primary, partial).map((m) => m.item)
    : COMMANDS;
  return ranked.slice(0, 8).map((c) => ({
    insert: c.primary,
    label: `:${c.primary}${c.argHint ? " " + c.argHint : ""}`,
    detail: c.describe,
  }));
}

function projectSuggestions(commandPrefix: string, partial: string, ctx: CommandCtx): Suggestion[] {
  const ranked = partial
    ? fuzzyBest(ctx.projects, shortName, partial).map((m) => m.item)
    : ctx.projects;
  const prefix = commandPrefix ? commandPrefix + " " : "";
  return ranked.slice(0, 8).map((p) => ({
    insert: `${prefix}${shortName(p)}`,
    label: `:${prefix}${shortName(p)}`,
    detail: p === shortName(p) ? undefined : p,
  }));
}

function clean(input: string): string {
  // Strip the leading ":" and leading whitespace only — a trailing space is
  // meaningful to suggest() (it moves completion from names to arguments).
  return input.replace(/^\s*:?\s*/, "");
}

export function resolveCommand(input: string, ctx: CommandCtx = { projects: [] }): CommandOutcome {
  const tokens = tokenizeFilter(clean(input));
  if (tokens.length === 0) return { type: "suggest", suggestions: commandNameSuggestions("") };
  const found = findFuzzy(tokens[0]);
  if (Array.isArray(found)) {
    return found.length
      ? { type: "suggest", suggestions: found }
      : { type: "error", message: `Unknown command "${tokens[0]}"`, suggestions: [] };
  }
  return found.resolve(tokens.slice(1), ctx);
}

/** Live suggestions for the command bar as the user types. */
export function suggest(input: string, ctx: CommandCtx = { projects: [] }): Suggestion[] {
  const raw = clean(input);
  const tokens = tokenizeFilter(raw);
  const trailingSpace = /\s$/.test(raw);

  // Still typing the command name.
  if (tokens.length === 0) return commandNameSuggestions("");
  if (tokens.length === 1 && !trailingSpace) return commandNameSuggestions(tokens[0]);

  // Typing arguments: suggest from the command's argument domain.
  const spec = findExact(tokens[0]) ?? (Array.isArray(findFuzzy(tokens[0])) ? null : (findFuzzy(tokens[0]) as CommandSpec));
  if (!spec) return commandNameSuggestions(tokens[0]);
  const partial = trailingSpace ? "" : tokens[tokens.length - 1];
  const priorArgs = tokens.slice(1, trailingSpace ? undefined : -1);
  const prefix = [spec.primary, ...priorArgs].join(" ");

  if (spec.primary === "pause" || spec.primary === "resume") {
    const words = ["tasks", "autos", "all", "."]
      .filter((w) => !partial || w.startsWith(partial.toLowerCase()))
      .map((w) => ({ insert: `${prefix} ${w}`, label: `:${prefix} ${w}` }));
    return [...words, ...projectSuggestions(prefix, partial, ctx)].slice(0, 8);
  }
  return projectSuggestions(prefix, partial, ctx);
}
