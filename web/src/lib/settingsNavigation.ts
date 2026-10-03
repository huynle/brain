import { deepEqual } from "./objectPath";

export type SettingsCategory =
  | "general"
  | "assistant"
  | "tasks"
  | "runner"
  | "advanced";

export function categoryForSection(section: string): SettingsCategory {
  if (section === "assistant") return "assistant";
  if (
    section === "task_defaults" ||
    section === "feature_checkout" ||
    section === "feature_delivery"
  )
    return "tasks";
  if (section === "runner" || section.startsWith("runner.")) return "runner";
  return "advanced";
}

type ConfigShape = Record<string, any>;

function serverSlice(config: ConfigShape, category: SettingsCategory): unknown {
  const server = config.server ?? {};
  if (category === "assistant") return server.assistant;
  if (category === "tasks")
    return {
      task_defaults: server.task_defaults,
      feature_checkout: server.feature_checkout,
      feature_delivery: server.feature_delivery,
    };
  if (category === "runner") return config.runner;
  if (category === "advanced") {
    const {
      assistant: _assistant,
      task_defaults: _taskDefaults,
      feature_checkout: _featureCheckout,
      feature_delivery: _featureDelivery,
      ...advanced
    } = server;
    return { server: advanced, mcp: config.mcp, plugins: config.plugins };
  }
  return undefined;
}

export function dirtySettingsCategories(
  before: ConfigShape,
  after: ConfigShape,
): Set<SettingsCategory> {
  const dirty = new Set<SettingsCategory>();
  for (const category of [
    "assistant",
    "tasks",
    "runner",
    "advanced",
  ] as const) {
    if (!deepEqual(serverSlice(before, category), serverSlice(after, category)))
      dirty.add(category);
  }
  return dirty;
}

export function nextSettingsCategory<T extends string>(
  categories: readonly T[],
  current: T,
  key: string,
): T | null {
  if (categories.length === 0) return null;
  if (key === "Home") return categories[0];
  if (key === "End") return categories[categories.length - 1];
  const direction =
    key === "ArrowRight" || key === "ArrowDown"
      ? 1
      : key === "ArrowLeft" || key === "ArrowUp"
        ? -1
        : 0;
  if (direction === 0) return null;
  const index = Math.max(0, categories.indexOf(current));
  return categories[(index + direction + categories.length) % categories.length];
}
