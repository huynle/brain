// Brain PWA — Multi-Project Dashboard v2
// Standalone ES module wireframe. No deps.

// ---------- tiny DOM helper ----------
const $  = (s, r = document) => r.querySelector(s);
const $$ = (s, r = document) => [...r.querySelectorAll(s)];
const el = (tag, props = {}, ...children) => {
  const n = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v === null || v === undefined || v === false) continue;
    if (k === "class") n.className = v;
    else if (k === "text") n.textContent = v;
    else if (k === "html") n.innerHTML = v;
    else if (k === "style") Object.assign(n.style, v);
    else if (k.startsWith("on")) n.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k === "dataset") for (const [dk, dv] of Object.entries(v || {})) n.dataset[dk] = dv;
    else n.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    n.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
  }
  return n;
};
const uid = (p = "id") => `${p}_${Math.random().toString(36).slice(2, 9)}`;
const clamp = (n, a, b) => Math.max(a, Math.min(b, n));

// ---------- MOCK DATA ----------
const MOCK_PROJECTS = [
  { id: "orion-ai",        name: "orion-ai",        env: "prod",    color: "#f4b23a" },
  { id: "brain",           name: "brain",           env: "dev",     color: "#6fca7d" },
  { id: "dispatch-runner", name: "dispatch-runner", env: "staging", color: "#6a8bff" },
  { id: "pathfinding",     name: "pathfinding",     env: "dev",     color: "#d96060" },
  { id: "orion-web",       name: "orion-web",       env: "prod",    color: "#c86fca" },
];

const MOCK_FEATURES = {
  "orion-ai": [
    { id: "F-auth",     name: "OAuth + entitlements",  progress: 0.65, state: "busy" },
    { id: "F-metrics",  name: "Agent metrics + gateway", progress: 1.0, state: "done", lifecycle: "finished" },
    { id: "F-migrate",  name: "SSE per-project",       progress: 0.2, state: "block" },
  ],
  "brain": [
    { id: "F-dockable", name: "Dockable pane system",  progress: 0.5, state: "busy" },
    { id: "F-runner",   name: "Runner queue polish",   progress: 1.0, state: "done", lifecycle: "merged" },
  ],
  "dispatch-runner": [
    { id: "F-capacity", name: "Capacity race fix",     progress: 1.0, state: "done", lifecycle: "mr" },
    { id: "F-workers",  name: "Worker pool scaling",   progress: 0.15, state: "busy" },
  ],
  "pathfinding": [
    { id: "F-astar",    name: "A* heuristics",         progress: 0.75, state: "busy" },
  ],
  "orion-web": [
    { id: "F-shell",    name: "New shell + sidebar",   progress: 0.55, state: "busy" },
  ],
};

const MOCK_TASKS = {
  "orion-ai": [
    { id: "T-1001", feat: "F-auth",    title: "Wire OAuth callback",            status: "Ready"     },
    { id: "T-1002", feat: "F-auth",    title: "Add rate limiter to /entries",    status: "Active"    },
    { id: "T-1003", feat: "F-migrate", title: "Migrate SSE to per-project",     status: "Blocked"   },
    { id: "T-1004", feat: "F-metrics", title: "Gateway tool-call logging",      status: "Active"    },
    { id: "T-1005", feat: "F-auth",    title: "Docs: entitlement examples",     status: "Completed" },
    { id: "T-1006", feat: "F-auth",    title: "Refactor scope keying",           status: "Ready"     },
    { id: "T-1007", feat: "F-metrics", title: "Schema migration",                status: "Completed" },
  ],
  "brain": [
    { id: "T-2001", feat: "F-dockable", title: "Wireframe v2 - Claude-inspired", status: "Active" },
    { id: "T-2002", feat: "F-dockable", title: "Extract Splitter component",     status: "Ready" },
    { id: "T-2003", feat: "F-dockable", title: "PaneRegistry lib",               status: "Ready" },
    { id: "T-2004", feat: "F-runner",   title: "Idle detection cleanup",         status: "Completed" },
  ],
  "dispatch-runner": [
    { id: "T-3001", feat: "F-capacity", title: "Push dispatch capacity race",    status: "Active" },
    { id: "T-3002", feat: "F-capacity", title: "Placement reasons cap",          status: "Completed" },
    { id: "T-3003", feat: "F-workers",  title: "Worker pool scaling",            status: "Ready" },
    { id: "T-3004", feat: "F-workers",  title: "Autoscale on queue depth",       status: "Blocked" },
  ],
  "pathfinding": [
    { id: "T-4001", feat: "F-astar", title: "Heuristic tuning",                 status: "Active" },
    { id: "T-4002", feat: "F-astar", title: "Grid partitioning benchmark",     status: "Ready" },
  ],
  "orion-web": [
    { id: "T-5001", feat: "F-shell", title: "Sidebar sessions list",            status: "Active" },
  ],
};

// Live sessions attached to some tasks. `live` means we tick prompts.
const MOCK_SESSIONS = [
  { id: "S-a1", project: "orion-ai",        taskId: "T-1002", title: "rate limiter impl",     live: true,  agent: "tdd-dev",   model: "claude-4-5-sonnet" },
  { id: "S-a2", project: "orion-ai",        taskId: "T-1004", title: "gateway tool-call log", live: true,  agent: "build",     model: "claude-4-5-sonnet" },
  { id: "S-b1", project: "brain",           taskId: "T-2001", title: "wireframe v2",          live: true,  agent: "opencode",  model: "claude-opus-4-7"     },
  { id: "S-d1", project: "dispatch-runner", taskId: "T-3001", title: "capacity race repro",   live: true,  agent: "explore",   model: "claude-4-5-sonnet" },
  { id: "S-p1", project: "pathfinding",     taskId: "T-4001", title: "heuristic bench",       live: false, agent: "tdd-dev",   model: "claude-haiku"      },
  { id: "S-w1", project: "orion-web",       taskId: "T-5001", title: "sidebar list",          live: true,  agent: "frontend",  model: "claude-4-5-sonnet" },
];

// Runners — machines / hosts registered with brain
const MOCK_RUNNERS = [
  { id: "R-mac01",   name: "runner-macbook-01",  host: "e367212-mac.local",  os: "darwin",  arch: "arm64",  status: "online", capacity: 4,  running: 2, executor: "opencode",  labels: ["dev", "local"],       lastSeen: "just now" },
  { id: "R-ci-eu",   name: "runner-ci-eu",       host: "ci-eu-01.brain.io",  os: "linux",   arch: "amd64",  status: "online", capacity: 8,  running: 0, executor: "pi",         labels: ["ci", "eu"],           lastSeen: "5s ago"    },
  { id: "R-orion-gpu",name:"runner-orion-gpu",   host: "orion-gpu.us.lmco",  os: "linux",   arch: "amd64",  status: "stale",  capacity: 2,  running: 0, executor: "opencode",  labels: ["gpu", "prod"],        lastSeen: "2d ago"    },
  { id: "R-orion-01",name: "runner-orion-01",    host: "orion-01.us.lmco",   os: "linux",   arch: "amd64",  status: "online", capacity: 6,  running: 3, executor: "opencode",  labels: ["prod", "us"],         lastSeen: "just now" },
];

// Feature → runner assignments (mock; user can drag features onto runners to update)
const initialAssignments = { "F-metrics": "R-mac01", "F-dockable": "R-mac01", "F-capacity": "R-orion-01" };

// Automations per project
const MOCK_AUTOMATIONS = {
  "orion-ai": [
    { id: "A-1", name: "feature-checkout (builtin)",   trigger: "event: feature.completed",  status: "active",  lastRun: "2h ago",  lastResult: "success", nextRun: "on completion", runCount: 18, failures: 0, watches: ["finished features", "checkout_mode"] },
    { id: "A-2", name: "blocked-inspector",             trigger: "cron: */30 * * * *",        status: "active",  lastRun: "12m ago", lastResult: "success", nextRun: "18m", runCount: 42, failures: 1, watches: ["blocked tasks", "stale sessions"] },
    { id: "A-3", name: "dream-consolidation",           trigger: "cron: 0 3 * * *",           status: "active",  lastRun: "6h ago",  lastResult: "success", nextRun: "21h", runCount: 9, failures: 0, watches: ["project dreams", "entry links"] },
    { id: "A-4", name: "auto-triage from MR reviews",   trigger: "webhook: gitlab/mr.review", status: "paused",  lastRun: "1d ago",  lastResult: "failed",  nextRun: "paused", runCount: 7, failures: 2, watches: ["MR comments", "follow-up tasks"] },
  ],
  "brain": [
    { id: "A-5", name: "feature-checkout (builtin)",   trigger: "event: feature.completed",  status: "active",  lastRun: "1h ago",  lastResult: "success", nextRun: "on completion", runCount: 23, failures: 0, watches: ["feature.completed"] },
    { id: "A-6", name: "runner health probe",           trigger: "cron: */5 * * * *",         status: "active",  lastRun: "2m ago",  lastResult: "success", nextRun: "3m", runCount: 321, failures: 3, watches: ["runner heartbeat", "capacity"] },
  ],
  "dispatch-runner": [
    { id: "A-7", name: "feature-checkout (builtin)",   trigger: "event: feature.completed",  status: "active",  lastRun: "5h ago",  lastResult: "success", nextRun: "on completion", runCount: 11, failures: 0, watches: ["MR open", "merge result"] },
    { id: "A-8", name: "capacity metrics push",         trigger: "cron: * * * * *",           status: "active",  lastRun: "just now", lastResult: "success", nextRun: "47s", runCount: 840, failures: 0, watches: ["runner load", "queue depth"] },
  ],
  "pathfinding": [
    { id: "A-9", name: "benchmark schedule",            trigger: "cron: 0 2 * * *",           status: "paused",  lastRun: "8h ago",  lastResult: "success", nextRun: "paused", runCount: 5, failures: 0, watches: ["benchmark tasks"] },
  ],
  "orion-web": [
    { id: "A-10", name: "PR preview deploy",            trigger: "webhook: gitlab/mr.opened", status: "active",  lastRun: "3h ago",  lastResult: "success", nextRun: "on MR", runCount: 14, failures: 1, watches: ["MR opened", "preview env"] },
  ],
};

const MOCK_ENTRIES = [
  { id: "E-101", project: "orion-ai", type: "dream", title: "Orion AI project dream", updated: "12m ago", links: ["F-auth", "F-metrics"], excerpt: "Consolidate gateway metrics, entitlement behavior, and SSE migration decisions into one operating picture." },
  { id: "E-102", project: "orion-ai", type: "decision", title: "Use per-project SSE channels", updated: "6h ago", links: ["F-migrate"], excerpt: "Global streams caused cross-project noise; route task updates through project-scoped event channels." },
  { id: "E-201", project: "brain", type: "note", title: "Workspace wireframe principles", updated: "now", links: ["F-dockable"], excerpt: "A command center should make project juggling, feature execution, automations, and memory editing feel like one workflow." },
  { id: "E-202", project: "brain", type: "task", title: "Expose entries editor in PWA", updated: "1h ago", links: ["F-dockable"], excerpt: "Add read/edit/search affordances for Brain entries without leaving the execution dashboard." },
  { id: "E-301", project: "dispatch-runner", type: "runbook", title: "Capacity race response", updated: "9h ago", links: ["F-capacity", "A-8"], excerpt: "When queue depth spikes, compare runner capacity, placement reasons, and active session count before dispatching." },
];

// Rich task metadata (extras)
const TASK_META = {
  "T-1001": { deps: [], depBy: ["T-1002"], priority: "medium", createdBy: "e367212",   createdAt: "3d ago", updatedAt: "2h ago", branch: "feature/oauth-callback",           workdir: "~/code/orion-ai",         estimateH: 1.5, runCount: 0, tags: ["frontend", "auth"] },
  "T-1002": { deps: ["T-1001"], depBy: [], priority: "high",   createdBy: "e367212",   createdAt: "3d ago", updatedAt: "1m ago", branch: "feature/rate-limiter",             workdir: "~/code/orion-ai",         estimateH: 3,   runCount: 2, tags: ["backend", "auth"] },
  "T-1003": { deps: ["T-1002"], depBy: [], priority: "medium", createdBy: "runner",    createdAt: "1d ago", updatedAt: "6h ago", branch: "feature/sse-per-project",           workdir: "~/code/orion-ai",         estimateH: 5,   runCount: 3, tags: ["backend", "infra"] },
  "T-1004": { deps: [], depBy: [], priority: "high",           createdBy: "e367212",   createdAt: "2d ago", updatedAt: "1m ago", branch: "feature/gateway-log",              workdir: "~/code/orion-ai",         estimateH: 4,   runCount: 1, tags: ["telemetry"] },
  "T-1005": { deps: [], depBy: [], priority: "low",            createdBy: "e367212",   createdAt: "5d ago", updatedAt: "1d ago", branch: null,                                workdir: "~/code/orion-ai/docs",     estimateH: 0.5, runCount: 1, tags: ["docs"] },
  "T-1006": { deps: [], depBy: [], priority: "medium",         createdBy: "e367212",   createdAt: "4d ago", updatedAt: "1d ago", branch: "feature/scope-keying",              workdir: "~/code/orion-ai",         estimateH: 2,   runCount: 0, tags: ["refactor"] },
  "T-1007": { deps: [], depBy: [], priority: "high",           createdBy: "runner",    createdAt: "1w ago", updatedAt: "2d ago", branch: "feature/schema-migration",          workdir: "~/code/orion-ai",         estimateH: 6,   runCount: 4, tags: ["db"] },
  "T-2001": { deps: [], depBy: ["T-2002","T-2003"], priority: "high", createdBy: "e367212", createdAt: "1d ago", updatedAt: "1m ago", branch: "wireframe-panes-v2",      workdir: "~/code/brain/web",         estimateH: 3,   runCount: 1, tags: ["ui", "wireframe"] },
  "T-2002": { deps: ["T-2001"], depBy: [], priority: "medium",  createdBy: "e367212",  createdAt: "1d ago", updatedAt: "3h ago", branch: null,                                workdir: "~/code/brain/web",         estimateH: 2,   runCount: 0, tags: ["ui"] },
  "T-2003": { deps: ["T-2001"], depBy: [], priority: "medium",  createdBy: "e367212",  createdAt: "1d ago", updatedAt: "3h ago", branch: null,                                workdir: "~/code/brain/web",         estimateH: 2,   runCount: 0, tags: ["ui"] },
  "T-2004": { deps: [], depBy: [], priority: "low",             createdBy: "runner",   createdAt: "1w ago", updatedAt: "3d ago", branch: null,                                workdir: "~/code/brain",             estimateH: 1,   runCount: 1, tags: ["cleanup"] },
  "T-3001": { deps: [], depBy: [], priority: "high",            createdBy: "e367212",  createdAt: "3d ago", updatedAt: "1m ago", branch: "feature/capacity-race",             workdir: "~/code/brain/dispatch",    estimateH: 5,   runCount: 5, tags: ["scheduler"] },
  "T-3002": { deps: [], depBy: [], priority: "medium",          createdBy: "e367212",  createdAt: "1w ago", updatedAt: "3d ago", branch: null,                                workdir: "~/code/brain/dispatch",    estimateH: 3,   runCount: 2, tags: ["scheduler"] },
  "T-3003": { deps: [], depBy: [], priority: "medium",          createdBy: "e367212",  createdAt: "2d ago", updatedAt: "1d ago", branch: null,                                workdir: "~/code/brain/dispatch",    estimateH: 4,   runCount: 0, tags: ["scheduler"] },
  "T-3004": { deps: ["T-3003"], depBy: [], priority: "low",     createdBy: "e367212",  createdAt: "2d ago", updatedAt: "1d ago", branch: null,                                workdir: "~/code/brain/dispatch",    estimateH: 3,   runCount: 0, tags: ["scheduler"] },
  "T-4001": { deps: [], depBy: [], priority: "high",            createdBy: "e367212",  createdAt: "5d ago", updatedAt: "6h ago", branch: "feature/astar-heuristic",           workdir: "~/code/pathfinding",       estimateH: 4,   runCount: 3, tags: ["algo"] },
  "T-4002": { deps: [], depBy: [], priority: "medium",          createdBy: "e367212",  createdAt: "5d ago", updatedAt: "1d ago", branch: null,                                workdir: "~/code/pathfinding",       estimateH: 2,   runCount: 0, tags: ["benchmark"] },
  "T-5001": { deps: [], depBy: [], priority: "high",            createdBy: "e367212",  createdAt: "1d ago", updatedAt: "1m ago", branch: "feature/sidebar-list",              workdir: "~/code/orion-web",         estimateH: 4,   runCount: 1, tags: ["frontend"] },
};

// Rich feature metadata
const FEATURE_META = {
  "F-auth":     { description: "OAuth 2.0 callback, entitlement grant checks, per-user rate limiting.",           owner: "e367212",  priority: "high",   createdAt: "1w ago", targetBranch: "main", checkoutMode: "ai",      mergeStrategy: "squash", lifecycle: "active", activeAge: "4d" },
  "F-metrics":  { description: "Agent metrics gathering + gateway tool-call logging for observability.",           owner: "e367212",  priority: "high",   createdAt: "5d ago", targetBranch: "main", checkoutMode: "ai",      mergeStrategy: "squash", lifecycle: "finished", finishedAt: "today" },
  "F-migrate":  { description: "Migrate SSE stream keying from global to per-project scope.",                       owner: "runner",   priority: "medium", createdAt: "1d ago", targetBranch: "main", checkoutMode: "simple",  mergeStrategy: "merge",  lifecycle: "blocked", blockedAge: "6h" },
  "F-dockable": { description: "Dockable, drag-and-drop pane layout for the PWA, inspired by Claude Code Desktop.", owner: "e367212",  priority: "high",   createdAt: "3d ago", targetBranch: "main", checkoutMode: "ai",      mergeStrategy: "squash", lifecycle: "active", activeAge: "2d" },
  "F-runner":   { description: "Polish runner poll loop and idle detection.",                                       owner: "e367212",  priority: "medium", createdAt: "1w ago", targetBranch: "main", checkoutMode: "simple",  mergeStrategy: "squash", lifecycle: "merged", mr: "!482", mergedAt: "2h ago" },
  "F-capacity": { description: "Fix push-dispatch capacity race between runners under contention.",                 owner: "e367212",  priority: "high",   createdAt: "3d ago", targetBranch: "main", checkoutMode: "ai",      mergeStrategy: "squash", lifecycle: "mr", mr: "!517", mrAge: "9h" },
  "F-workers":  { description: "Autoscale worker pool based on queue depth.",                                       owner: "e367212",  priority: "medium", createdAt: "2d ago", targetBranch: "main", checkoutMode: "ai",      mergeStrategy: "squash", lifecycle: "active", activeAge: "2d" },
  "F-astar":    { description: "A* heuristic tuning + grid partitioning benchmarks.",                                owner: "e367212",  priority: "high",   createdAt: "5d ago", targetBranch: "main", checkoutMode: "ai",      mergeStrategy: "squash", lifecycle: "active", activeAge: "5d" },
  "F-shell":    { description: "New shell + sidebar for orion-web.",                                                owner: "e367212",  priority: "high",   createdAt: "1d ago", targetBranch: "main", checkoutMode: "ai",      mergeStrategy: "squash", lifecycle: "active", activeAge: "1d" },
};

const FEATURE_LIFECYCLE = {
  active:   { label: "active",   tone: "active" },
  blocked:  { label: "blocked",  tone: "blocked" },
  finished: { label: "finished", tone: "finished" },
  mr:       { label: "MR open",  tone: "mr" },
  merged:   { label: "merged",   tone: "merged" },
};

function featureLifecycle(feature) {
  const meta = FEATURE_META[feature.id] || {};
  const key = feature.lifecycle || meta.lifecycle || (feature.state === "done" ? "finished" : feature.state === "block" ? "blocked" : "active");
  return { key, ...(FEATURE_LIFECYCLE[key] || FEATURE_LIFECYCLE.active) };
}

// Default brain server settings (editable via Settings modal)
const DEFAULT_SETTINGS = {
  apiUrl: "http://localhost:3333",
  apiToken: "brain_dev_••••••••",
  defaultExecutor: "opencode",
  defaultAgent: "tdd-dev",
  defaultModel: "anthropic/claude-sonnet-4-20250514",
  defaultThinking: "high",
  defaultCheckoutMode: "ai",
  defaultMergeStrategy: "squash",
  defaultMergeTargetBranch: "main",
  defaultExecutionMode: "worktree",
  maxParallelTasks: 4,
  autoDispatchNewTasks: true,
  streamLogs: true,
  theme: "system",
  telemetry: true,
  density: "normal",
};

// Log lines that tick over time
const LOG_SNIPPETS = [
  { lvl: "INFO", msg: "session started" },
  { lvl: "INFO", msg: "step completed" },
  { lvl: "INFO", msg: "task claimed" },
  { lvl: "OK",   msg: "tests passing" },
  { lvl: "OK",   msg: "SSE reconnect" },
  { lvl: "WARN", msg: "runner idle" },
  { lvl: "INFO", msg: "commit pushed" },
  { lvl: "INFO", msg: "worktree ready" },
  { lvl: "ERROR",msg: "commit conflict — retrying" },
  { lvl: "INFO", msg: "spawn opencode" },
  { lvl: "INFO", msg: "tool: Edit(panes.css)" },
  { lvl: "INFO", msg: "tool: Bash(npm test)" },
];
const STREAM_TURNS = [
  { role: "user",      text: "Continue with the pane drag fixes."                        },
  { role: "assistant", text: "Reading current drag implementation..."                    },
  { role: "tool",      text: "Read(panes.js)"                                            },
  { role: "assistant", text: "Found 5 bugs. Applying fixes now."                         },
  { role: "tool",      text: "Edit(panes.js)"                                            },
  { role: "assistant", text: "Verifying with a smoke test."                              },
  { role: "tool",      text: "Bash(node --check panes.js)"                               },
  { role: "result",    text: "OK"                                                        },
  { role: "assistant", text: "All fixes applied cleanly. Ready to run."                  },
];

// ---------- pane registry ----------
const PANE_KINDS = {
  "overview": {
    label: "Overview",
    title: () => "All projects",
    render: (leaf, ctx) => renderOverview(ctx),
    canReuse: true,
  },
  "task-list": {
    label: "Tasks",
    title: (leaf) => `Tasks: ${leaf.target.projectId}`,
    render: (leaf, ctx) => renderTaskList(leaf.target.projectId, ctx),
    canReuse: true,
  },
  "task-detail": {
    label: "Detail",
    title: (leaf) => leaf.target.taskId ? `${leaf.target.taskId}` : "Detail",
    render: (leaf) => renderTaskDetail(leaf),
  },
  "session": {
    label: "Session",
    title: (leaf) => {
      const s = MOCK_SESSIONS.find(x => x.id === leaf.target.sessionId);
      return s ? s.title : "Session";
    },
    render: (leaf) => renderSessionView(leaf),
  },
  "logs": {
    label: "Logs",
    title: (leaf) => leaf.target.taskId ? `Logs: ${leaf.target.taskId}` : "Logs",
    render: (leaf) => renderLogsPane(leaf),
  },
  "runners": {
    label: "Runners",
    title: () => "Runners",
    render: () => renderRunners(),
    canReuse: true,
  },
  "automations": {
    label: "Automations",
    title: (leaf) => leaf.target.projectId ? `Automations: ${leaf.target.projectId}` : "Automations",
    render: (leaf) => renderAutomationCenter(leaf.target.projectId),
    canReuse: true,
  },
  "entries": {
    label: "Entries",
    title: (leaf) => leaf.target.projectId ? `Brain: ${leaf.target.projectId}` : "Brain entries",
    render: (leaf) => renderEntriesPane(leaf.target.projectId),
    canReuse: true,
  },
  "terminal": {
    label: "Terminal",
    title: (leaf) => `Terminal ${leaf.id.slice(-4)}`,
    render: () => renderTerminal(),
  },
  "browser": {
    label: "Browser",
    title: (leaf) => leaf.target.url ? `Web: ${leaf.target.url.replace(/^https?:\/\//,"")}` : "Browser",
    render: (leaf) => renderBrowser(leaf),
  },
};

// ---------- state ----------
const LS_KEY = "brain.wireframe.workspace.v2";
const defaultState = () => ({
  view: "overview",           // "overview" | "focus" | "session"
  focusSessionId: null,       // for view === "session"
  openProjects: MOCK_PROJECTS.map(p => p.id),
  cardOrder: MOCK_PROJECTS.map(p => p.id),  // controls order in overview
  activeCardTab: {},          // per project: "tasks" | "features" | "automations" | "logs" | "session"
  filters: {
    status: "all",            // all | active | blocked | ready | completed
    lifecycle: "all",         // all | attention | active | blocked | finished | mr | merged
    env: "all",               // all | prod | staging | dev
  },
  groupBy: "project",         // "project" | "status" | "env"
  selectedProject: "all",     // all | project id
  savedView: "execution",     // execution | review | memory | automation | runners
  drawer: null,               // { kind: "feature", projectId, featureId }
  commandPalette: false,
  assistantOpen: false,
  focusLayout: null,          // dockable pane tree when in focus mode
  sidebarSection: { projects: true, sessions: true, runners: true },
  sidebarCollapsed: false,
  mobile: false,
  streaming: true,            // live stream ticker enabled
  logs: {},                   // per-taskId ring buffer
  sessionMsgs: {},            // per-sessionId turn index
  featureAssignments: { ...initialAssignments },  // feature-id → runner-id
  settings: { ...DEFAULT_SETTINGS },
  modal: null,                // { kind: "settings" | "task" | "feature" | "automation" | "runner", id }
});
let state = load() || defaultState();

function load() {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (!raw) return null;
    const s = JSON.parse(raw);
    // Wipe live buffers on load — they'll re-tick from empty
    s.logs = {};
    s.sessionMsgs = {};
    // Backfill any new state fields from defaults
    const def = defaultState();
    if (!s.featureAssignments)    s.featureAssignments = def.featureAssignments;
    s.settings = { ...def.settings, ...(s.settings || {}) };
    if (!s.filters)               s.filters = def.filters;
    if (!s.filters.lifecycle)     s.filters.lifecycle = def.filters.lifecycle;
    if (!s.selectedProject)        s.selectedProject = def.selectedProject;
    if (!s.savedView)              s.savedView = def.savedView;
    if (s.commandPalette === undefined) s.commandPalette = def.commandPalette;
    if (s.assistantOpen === undefined) s.assistantOpen = def.assistantOpen;
    if (s.drawer === undefined)    s.drawer = def.drawer;
    if (!s.sidebarSection.runners) s.sidebarSection.runners = true;
    if (s.sidebarCollapsed === undefined) s.sidebarCollapsed = def.sidebarCollapsed;
    s.modal = null;
    return s;
  } catch { return null; }
}
function save() {
  try {
    const { logs, sessionMsgs, ...rest } = state;
    localStorage.setItem(LS_KEY, JSON.stringify(rest));
  } catch {}
}
function resetState() {
  localStorage.removeItem(LS_KEY);
  state = defaultState();
  render();
}

// ---------- helpers for stats ----------
function projStats(pid) {
  const tasks = MOCK_TASKS[pid] || [];
  return {
    active:    tasks.filter(t => t.status === "Active").length,
    ready:     tasks.filter(t => t.status === "Ready").length,
    blocked:   tasks.filter(t => t.status === "Blocked").length,
    completed: tasks.filter(t => t.status === "Completed").length,
    total:     tasks.length,
  };
}
function featureAge(feature) {
  const meta = FEATURE_META[feature.id] || {};
  return meta.mergedAt || meta.finishedAt || meta.mrAge || meta.blockedAge || meta.activeAge || meta.createdAt || "new";
}
function runnerForFeature(feature) {
  const assignedRunner = state.featureAssignments[feature.id];
  return MOCK_RUNNERS.find(r => r.id === assignedRunner);
}
function runnerWarning(feature) {
  const runner = runnerForFeature(feature);
  if (!runner) return null;
  if (runner.status === "online") return null;
  return `${runner.name.replace("runner-", "")} ${runner.status}`;
}
function featureNeedsAttention(feature) {
  const lifecycle = featureLifecycle(feature);
  return lifecycle.key === "blocked" || lifecycle.key === "mr" || !!runnerWarning(feature);
}
function projectLifecycleStats(pid) {
  const totals = { active: 0, blocked: 0, finished: 0, mr: 0, merged: 0, attention: 0 };
  for (const f of MOCK_FEATURES[pid] || []) {
    const lifecycle = featureLifecycle(f);
    if (totals[lifecycle.key] !== undefined) totals[lifecycle.key] += 1;
    if (featureNeedsAttention(f)) totals.attention += 1;
  }
  return totals;
}
function projectHealth(pid) {
  const lifecycle = projectLifecycleStats(pid);
  const tasks = projStats(pid);
  if (lifecycle.blocked || tasks.blocked) return { label: "blocked", tone: "blocked" };
  if (lifecycle.mr) return { label: "reviewing", tone: "mr" };
  if (lifecycle.attention) return { label: "attention", tone: "blocked" };
  if (tasks.active || lifecycle.active) return { label: "active", tone: "active" };
  return { label: "healthy", tone: "merged" };
}
function allAttentionFeatures() {
  const items = [];
  for (const project of MOCK_PROJECTS) {
    for (const feature of MOCK_FEATURES[project.id] || []) {
      if (!featureNeedsAttention(feature)) continue;
      const lifecycle = featureLifecycle(feature);
      const meta = FEATURE_META[feature.id] || {};
      items.push({ project, feature, lifecycle, meta, runnerIssue: runnerWarning(feature) });
    }
  }
  return items;
}
function renderLifecycleStrip(pid) {
  const s = projectLifecycleStats(pid);
  return el("div", { class: "flow-strip" },
    ...[
      ["active", "active", s.active],
      ["blocked", "blocked", s.blocked],
      ["finished", "finished", s.finished],
      ["mr", "MR", s.mr],
      ["merged", "merged", s.merged],
    ].filter(([, , n]) => n > 0).map(([tone, label, n]) =>
      el("span", { class: `flow-pill ${tone}` }, el("b", { text: n }), ` ${label}`)
    )
  );
}
function renderAttentionQueue() {
  const items = allAttentionFeatures();
  if (!items.length) return null;
  return el("div", { class: "review-queue" },
    el("div", { class: "rq-head" },
      el("span", { text: "Needs attention" }),
      el("span", { class: "rq-count", text: `${items.length} feature${items.length === 1 ? "" : "s"}` }),
    ),
    el("div", { class: "rq-list" },
      ...items.map(({ project, feature, lifecycle, meta, runnerIssue }) => el("div", {
        class: "rq-item",
        onclick: () => openModal("feature", { projectId: project.id, featureId: feature.id }),
      },
        el("span", { class: `life-badge ${lifecycle.tone}`, text: lifecycle.label }),
        el("span", { class: "rq-name", text: feature.name }),
        el("span", { class: "rq-meta", text: `${project.name} · ${meta.mr || runnerIssue || featureAge(feature)}` }),
      ))
    )
  );
}
function renderLifecycleBoard() {
  const lanes = [
    ["active", "Active"],
    ["blocked", "Blocked"],
    ["finished", "Finished"],
    ["mr", "MR open"],
    ["merged", "Merged"],
  ];
  const cards = [];
  for (const project of MOCK_PROJECTS) {
    for (const feature of MOCK_FEATURES[project.id] || []) cards.push({ project, feature, lifecycle: featureLifecycle(feature) });
  }
  return el("div", { class: "flow-board" },
    ...lanes.map(([key, title]) => {
      const items = cards.filter(c => c.lifecycle.key === key);
      return el("div", { class: `flow-lane ${key}` },
        el("div", { class: "lane-head" },
          el("span", { text: title }),
          el("b", { text: String(items.length) }),
        ),
        el("div", { class: "lane-items" },
          ...items.slice(0, 4).map(({ project, feature, lifecycle }) => el("button", {
            class: "lane-card",
            onclick: () => openModal("feature", { projectId: project.id, featureId: feature.id }),
          },
            el("span", { class: "lane-name", text: feature.name }),
            el("span", { class: "lane-meta", text: `${project.name} · ${featureAge(feature)}` }),
          )),
          items.length > 4 ? el("span", { class: "lane-more", text: `+${items.length - 4} more` }) : null,
        )
      );
    })
  );
}
function renderWorkflowCenter() {
  const queue = executionQueue();
  const autos = automationStats(state.selectedProject === "all" ? undefined : state.selectedProject);
  const scopedEntries = state.selectedProject === "all" ? MOCK_ENTRIES : entriesForProject(state.selectedProject);
  const activeRunners = MOCK_RUNNERS.filter(r => r.status === "online").length;
  const scopeLabel = state.selectedProject === "all" ? "all projects" : state.selectedProject;
  return el("div", { class: "workflow-center" },
    el("div", { class: "wc-head" },
      el("div", {},
        el("div", { class: "wc-title", text: `Workflow command center · ${scopeLabel}` }),
        el("div", { class: "wc-sub", text: "Execute features, track automation consequences, and update Brain memory from one control surface." }),
      ),
      el("button", { class: "primary", text: "Run next ready feature", onclick: () => queue[0] ? toast(`Dispatch ${queue[0].feature.id} (mock)`) : toast("No executable features") }),
      el("button", { text: "Open Brain entries", onclick: () => openInFocus("entries", { projectId: state.selectedProject === "all" ? undefined : state.selectedProject }) }),
    ),
    el("div", { class: "wc-metrics" },
      el("div", {}, el("b", { text: String(queue.length) }), el("span", { text: " executable features" })),
      el("div", {}, el("b", { text: `${activeRunners}/${MOCK_RUNNERS.length}` }), el("span", { text: " runners online" })),
      el("div", {}, el("b", { text: `${autos.active}/${autos.total}` }), el("span", { text: " automations active" })),
      el("div", {}, el("b", { text: String(scopedEntries.length) }), el("span", { text: " linked entries" })),
    ),
    el("div", { class: "wc-queue" },
      ...queue.slice(0, 5).map(({ project, feature, lifecycle, runner, warn }) => el("div", { class: "wc-row", dataset: { projectId: project.id, featureId: feature.id } },
        el("span", { class: `life-badge ${lifecycle.tone}`, text: lifecycle.label }),
        el("span", { class: "wc-feature", text: feature.name }),
        el("span", { class: "wc-meta", text: `${project.name} · ${runner ? runner.name.replace("runner-", "") : "unassigned"}${warn ? ` · ${warn}` : ""}` }),
        el("button", { text: "Run", onclick: () => toast(`Run ${feature.id} (mock)`) }),
        el("button", { text: "Queue", onclick: () => toast(`Queued ${feature.id} (mock)`) }),
        el("button", { text: "Plan", dataset: { action: "plan" }, onclick: () => openFeatureDrawer(project.id, feature.id) }),
      ))
    )
  );
}
function renderAutomationTimeline() {
  const events = automationEvents(state.selectedProject);
  return el("div", { class: "automation-timeline-panel" },
    el("div", { class: "timeline-head" },
      el("span", { text: "Automation timeline" }),
      el("button", { text: "Open center", onclick: () => openInFocus("automations", { projectId: state.selectedProject === "all" ? undefined : state.selectedProject }) }),
    ),
    el("div", { class: "timeline-list" },
      ...events.slice(0, 6).map(({ project, automation }) => el("div", { class: `timeline-item ${automation.lastResult}` },
        el("span", { class: "dot" }),
        el("div", {},
          el("div", { class: "timeline-title", text: automation.name }),
          el("div", { class: "timeline-meta", text: `${project.name} · last ${automation.lastRun} · next ${automation.nextRun || "event"}` }),
        ),
        el("button", { text: automation.status === "active" ? "Pause" : "Resume", onclick: () => toast(`${automation.status === "active" ? "Paused" : "Resumed"} ${automation.id} (mock)`) }),
      ))
    )
  );
}
function renderEntriesPreview() {
  return el("div", { class: "entries-preview" },
    el("div", { class: "ep-head" },
      el("span", { text: "Brain entries" }),
      el("button", { text: "Open editor", onclick: () => openInFocus("entries", {}) }),
      el("button", { text: "+ Entry", onclick: () => openModal("entry", { entryId: MOCK_ENTRIES[0].id }) }),
    ),
    el("div", { class: "entry-grid" },
      ...MOCK_ENTRIES.slice(0, 5).map(entry => renderEntryCard(entry))
    )
  );
}
function renderEntryCard(entry) {
  return el("div", { class: "entry-card", onclick: () => openModal("entry", { entryId: entry.id }) },
    el("div", { class: "entry-top" },
      el("span", { class: `entry-type ${entry.type}`, text: entry.type }),
      el("span", { class: "entry-updated", text: entry.updated }),
    ),
    el("div", { class: "entry-title", text: entry.title }),
    el("div", { class: "entry-excerpt", text: entry.excerpt }),
    el("div", { class: "entry-links" },
      ...entry.links.map(link => el("span", { class: "chip mini", text: link }))
    )
  );
}
function lifecycleTotals() {
  return MOCK_PROJECTS.reduce((acc, p) => {
    const s = projectLifecycleStats(p.id);
    for (const [k, v] of Object.entries(s)) acc[k] = (acc[k] || 0) + v;
    return acc;
  }, { active: 0, blocked: 0, finished: 0, mr: 0, merged: 0, attention: 0 });
}
function automationStats(pid) {
  const list = pid ? (MOCK_AUTOMATIONS[pid] || []) : Object.values(MOCK_AUTOMATIONS).flat();
  return {
    active: list.filter(a => a.status === "active").length,
    paused: list.filter(a => a.status === "paused").length,
    failed: list.filter(a => a.lastResult === "failed").length,
    total: list.length,
  };
}
function entriesForProject(pid) {
  return MOCK_ENTRIES.filter(e => !pid || e.project === pid);
}
function scopedProjects() {
  return state.selectedProject && state.selectedProject !== "all"
    ? MOCK_PROJECTS.filter(p => p.id === state.selectedProject)
    : MOCK_PROJECTS;
}
function entriesForFeature(featureId) {
  return MOCK_ENTRIES.filter(e => (e.links || []).includes(featureId));
}
function automationEvents(pid) {
  const rows = [];
  for (const project of (pid && pid !== "all" ? MOCK_PROJECTS.filter(p => p.id === pid) : MOCK_PROJECTS)) {
    for (const a of MOCK_AUTOMATIONS[project.id] || []) rows.push({ project, automation: a });
  }
  return rows.sort((a, b) => (a.automation.lastResult === "failed" ? -1 : 1));
}
function selectProject(pid) {
  state.selectedProject = pid;
  if (pid !== "all") state.filters.env = "all";
  save(); render();
}
function openFeatureDrawer(projectId, featureId) {
  state.drawer = { kind: "feature", projectId, featureId };
  save(); render();
}
function closeDrawer() {
  state.drawer = null;
  save(); render();
}
function executionQueue() {
  const rows = [];
  for (const project of scopedProjects()) {
    for (const feature of MOCK_FEATURES[project.id] || []) {
      const lifecycle = featureLifecycle(feature);
      if (["merged", "finished"].includes(lifecycle.key)) continue;
      rows.push({ project, feature, lifecycle, runner: runnerForFeature(feature), warn: runnerWarning(feature) });
    }
  }
  return rows.sort((a, b) => Number(featureNeedsAttention(b.feature)) - Number(featureNeedsAttention(a.feature)));
}
function isProjectVisible(pid) {
  if (!state.openProjects.includes(pid)) return false;
  const proj = MOCK_PROJECTS.find(p => p.id === pid);
  if (!proj) return false;
  if (state.filters.env !== "all" && proj.env !== state.filters.env) return false;
  const lifecycleFilter = state.filters.lifecycle || "all";
  if (lifecycleFilter !== "all") {
    const features = MOCK_FEATURES[pid] || [];
    const hasLifecycle = lifecycleFilter === "attention"
      ? features.some(featureNeedsAttention)
      : features.some(f => featureLifecycle(f).key === lifecycleFilter);
    if (!hasLifecycle) return false;
  }
  const s = projStats(pid);
  if (state.filters.status === "active"    && s.active === 0) return false;
  if (state.filters.status === "blocked"   && s.blocked === 0) return false;
  if (state.filters.status === "ready"     && s.ready === 0) return false;
  if (state.filters.status === "completed" && s.completed === 0) return false;
  return true;
}
function projSessions(pid) {
  return MOCK_SESSIONS.filter(s => s.project === pid);
}

// ---------- live stream ticker ----------
let tickHandle = null;
function tick() {
  if (!state.streaming) return;
  // For each live session, append a log line + maybe a stream msg
  let dirtyLogs = false, dirtySess = false;
  for (const s of MOCK_SESSIONS) {
    if (!s.live) continue;
    if (Math.random() < 0.55) {
      const snip = LOG_SNIPPETS[Math.floor(Math.random() * LOG_SNIPPETS.length)];
      const line = {
        ts: new Date().toLocaleTimeString(),
        lvl: snip.lvl,
        msg: `[${s.taskId}] ${snip.msg}`,
        _new: true,
      };
      const buf = state.logs[s.taskId] ||= [];
      buf.push(line);
      if (buf.length > 60) buf.shift();
      dirtyLogs = true;
    }
    if (Math.random() < 0.25) {
      const idx = state.sessionMsgs[s.id] ?? 0;
      state.sessionMsgs[s.id] = idx + 1;
      dirtySess = true;
    }
  }
  if (dirtyLogs) refreshLogMinis();
  if (dirtySess) refreshSessionView();
}
function startTicker() {
  if (tickHandle) clearInterval(tickHandle);
  tickHandle = setInterval(tick, 900);
}

// ---------- top-level render ----------
function resolvedTheme() {
  const mode = state.settings?.theme || "system";
  if (mode === "system") return window.matchMedia?.("(prefers-color-scheme: light)").matches ? "light" : "dark";
  return mode;
}
function themeLabel() {
  const mode = state.settings?.theme || "system";
  if (mode === "system") return `System ${resolvedTheme() === "light" ? "☀" : "☾"}`;
  return mode === "light" ? "Light ☀" : "Dark ☾";
}
function cycleTheme() {
  const order = ["system", "dark", "light"];
  const current = state.settings.theme || "system";
  state.settings.theme = order[(order.indexOf(current) + 1) % order.length];
  save(); render();
  toast(`Theme: ${themeLabel()}`);
}
const app = $("#app");
function render() {
  app.innerHTML = "";
  document.body.classList.toggle("mobile", !!state.mobile);
  document.body.classList.toggle("sidebar-collapsed", !!state.sidebarCollapsed);
  document.body.dataset.theme = resolvedTheme();
  document.body.dataset.themeMode = state.settings.theme || "system";
  document.body.dataset.density = state.settings.density || "normal";

  app.appendChild(renderTopbar());
  if (state.mobile) app.appendChild(renderMobileNav());
  if (!state.sidebarCollapsed) app.appendChild(renderSidebar());
  else app.appendChild(renderSidebarRestore());
  app.appendChild(renderWorkspace());
  if (state.drawer) app.appendChild(renderFeatureDrawer());
  if (state.assistantOpen) app.appendChild(renderAssistantPanel());
  if (state.commandPalette) app.appendChild(renderCommandPalette());
  app.appendChild(renderStatusbar());
}

// ---------- topbar ----------
function renderTopbar() {
  return el("div", { class: "topbar" },
    el("span", { class: "brand" }, "brain", " ", el("span", { text: "workspace" })),
    el("div", { class: "viewmode" },
      ...[
        ["overview", "Overview"],
        ["focus",    "Focus"],
      ].map(([k, lbl]) =>
        el("button", {
          class: state.view === k ? "active" : "",
          text: lbl,
          onclick: () => { state.view = k; save(); render(); },
        })
      ),
    ),
    el("div", { class: "search" },
      el("span", { text: "⌕", style: { color: "#6b757e" } }),
      el("input", { type: "search", placeholder: "Search projects, tasks, entries…", onfocus: () => { state.commandPalette = true; save(); render(); } }),
      el("span", { class: "hint", text: "⌘K" }),
    ),
    el("div", { class: "saved-views" },
      ...[["execution", "Execute"], ["review", "Review"], ["memory", "Memory"], ["automation", "Automate"], ["runners", "Runners"]].map(([k, lbl]) =>
        el("button", { class: state.savedView === k ? "active" : "", text: lbl, onclick: () => { state.savedView = k; save(); render(); } })
      )
    ),
    el("div", { class: "spacer" }),
    el("button", { class: "icon-btn", title: "Command palette", text: "⌘K", dataset: { action: "command-palette" }, onclick: () => { state.commandPalette = true; save(); render(); } }),
    el("button", { class: "icon-btn", title: "New session", text: "＋ session", onclick: () => toast("New session (mock)") }),
    el("button", { class: "icon-btn", title: "Notifications", text: "🔔" }),
    el("button", {
      class: "icon-btn",
      title: state.mobile ? "Switch to desktop view" : "Switch to mobile view",
      text: state.mobile ? "🖥 Desktop" : "📱 Mobile",
      onclick: () => { state.mobile = !state.mobile; save(); render(); },
    }),
    el("button", { class: "icon-btn", title: "Assistant", text: `Assistant ${state.assistantOpen ? "▾" : "▸"}`, onclick: () => { state.assistantOpen = !state.assistantOpen; save(); render(); } }),
  );
}

// ---------- sidebar ----------
function renderSidebar() {
  const bar = el("div", { class: "sidebar" });


  // Filters
  const totals = MOCK_PROJECTS.reduce((a, p) => {
    const s = projStats(p.id);
    a.active += s.active; a.blocked += s.blocked; a.ready += s.ready; a.completed += s.completed;
    return a;
  }, { active:0, blocked:0, ready:0, completed:0 });

  bar.appendChild(el("div", { class: "sb-filters" },
    ...[
      ["all",       "All"],
      ["active",    "Active",    totals.active],
      ["ready",     "Ready",     totals.ready],
      ["blocked",   "Blocked",   totals.blocked],
      ["completed", "Done",      totals.completed],
    ].map(([k, lbl, n]) => el("span", {
      class: `chip ${state.filters.status === k ? "active" : ""}`,
      onclick: () => { state.filters.status = k; save(); render(); },
    }, lbl, n !== undefined ? el("span", { class: "n", text: n }) : null)),
  ));
  const lifecycle = lifecycleTotals();
  bar.appendChild(el("div", { class: "sb-filters lifecycle-filters" },
    ...[
      ["all", "all flow"],
      ["attention", "attention", lifecycle.attention],
      ["blocked", "blocked", lifecycle.blocked],
      ["mr", "MR", lifecycle.mr],
      ["finished", "finished", lifecycle.finished],
      ["merged", "merged", lifecycle.merged],
    ].map(([k, lbl, n]) => el("span", {
      class: `chip ${state.filters.lifecycle === k ? "active" : ""}`,
      onclick: () => { state.filters.lifecycle = k; save(); render(); },
    }, lbl, n !== undefined ? el("span", { class: "n", text: n }) : null)),
  ));
  bar.appendChild(el("div", { class: "sb-filters" },
    ...[["all", "all envs"], ["prod", "prod"], ["staging", "staging"], ["dev", "dev"]].map(([k, lbl]) =>
      el("span", {
        class: `chip ${state.filters.env === k ? "active" : ""}`,
        text: lbl,
        onclick: () => { state.filters.env = k; save(); render(); },
      })
    ),
    el("span", { class: "chip reset", text: "clear", onclick: () => { state.filters = { status: "all", lifecycle: "all", env: "all" }; save(); render(); } }),
  ));

  // Projects section
  const projSection = el("div", { class: "sb-section" },
    el("div", {
      class: `sb-head ${!state.sidebarSection.projects ? "collapsed" : ""}`,
      onclick: (e) => {
        if (e.target.classList.contains("add")) return;
        state.sidebarSection.projects = !state.sidebarSection.projects;
        save(); render();
      },
    },
      el("span", { class: "caret", text: "▾" }),
      "Projects",
      el("span", { class: "count", text: `${MOCK_PROJECTS.filter(p => isProjectVisible(p.id)).length}/${MOCK_PROJECTS.length}` }),
      el("button", { class: "add", title: "Add project", text: "＋", onclick: (e) => { e.stopPropagation(); toast("Add project (mock)"); } }),
    ),
  );
  if (state.sidebarSection.projects) {
    const list = el("div", { class: "sb-list" });
    for (const pid of state.cardOrder) {
      if (!isProjectVisible(pid)) continue;
      const proj = MOCK_PROJECTS.find(p => p.id === pid);
      const s = projStats(pid);
      const hasBusy = s.active > 0;
      const hasErr  = s.blocked > 0;
      const row = el("div", {
        class: `proj-row ${state.view === "focus" ? "" : ""}`,
        onclick: () => {
          state.view = "overview";
          save(); render();
          // scroll to card
          setTimeout(() => {
            const c = document.querySelector(`.pcard[data-project="${pid}"]`);
            c?.scrollIntoView({ block: "nearest", behavior: "smooth" });
            c?.classList.add("focused"); setTimeout(() => c?.classList.remove("focused"), 800);
          }, 30);
        },
        oncontextmenu: (e) => {
          e.preventDefault();
          showContextMenu(e.clientX, e.clientY, [
            { label: "Show in overview",       onClick: () => { state.view = "overview"; save(); render(); } },
            { label: "Open in focus workspace", onClick: () => openInFocus("task-list", { projectId: pid }) },
            { sep: true },
            { label: "Hide project", onClick: () => {
              state.openProjects = state.openProjects.filter(x => x !== pid);
              save(); render();
              toast(`Hid ${pid}`);
            } },
          ]);
        },
      },
        el("span", { class: `dot ${hasBusy ? "busy" : hasErr ? "err" : "on"}` }),
        el("span", { class: "name", text: proj.name }),
        el("span", { class: "stats" },
          s.active   ? el("span", { class: "active",  text: s.active   + "▸" }) : null,
          s.ready    ? el("span", { class: "ready",   text: s.ready    + "▪" }) : null,
          s.blocked  ? el("span", { class: "blocked", text: s.blocked  + "✕" }) : null,
        ),
      );
      list.appendChild(row);
    }
    // Hidden projects re-add
    const hidden = MOCK_PROJECTS.filter(p => !state.openProjects.includes(p.id));
    if (hidden.length) {
      list.appendChild(el("div", { style: { padding: "4px 8px", fontSize: "10px", color: "#4b545c" }, text: "Hidden:" }));
      for (const p of hidden) {
        list.appendChild(el("div", {
          class: "proj-row", style: { opacity: 0.6 },
          onclick: () => { state.openProjects.push(p.id); save(); render(); },
        },
          el("span", { class: "dot" }),
          el("span", { class: "name", text: p.name }),
          el("span", { class: "stats", text: "add ＋" }),
        ));
      }
    }
    projSection.appendChild(list);
  }
  bar.appendChild(projSection);

  // Sessions section
  const sessSection = el("div", { class: "sb-section", style: { flex: 1, minHeight: 0 } },
    el("div", {
      class: `sb-head ${!state.sidebarSection.sessions ? "collapsed" : ""}`,
      onclick: () => { state.sidebarSection.sessions = !state.sidebarSection.sessions; save(); render(); },
    },
      el("span", { class: "caret", text: "▾" }),
      "Live sessions",
      el("span", { class: "count", text: String(MOCK_SESSIONS.filter(s => s.live).length) }),
    ),
  );
  if (state.sidebarSection.sessions) {
    const list = el("div", { class: "sb-list" });
    const shown = MOCK_SESSIONS.filter(s => isProjectVisible(s.project));
    for (const s of shown) {
      const proj = MOCK_PROJECTS.find(p => p.id === s.project);
      const row = el("div", {
        class: `sess-row ${state.view === "session" && state.focusSessionId === s.id ? "active" : ""}`,
        onclick: () => { state.view = "session"; state.focusSessionId = s.id; save(); render(); },
        oncontextmenu: (e) => {
          e.preventDefault();
          showContextMenu(e.clientX, e.clientY, [
            { label: "Open session (full view)",  onClick: () => { state.view = "session"; state.focusSessionId = s.id; save(); render(); } },
            { label: "Open logs in focus pane",   onClick: () => openInFocus("logs", { projectId: s.project, taskId: s.taskId }) },
            { label: "Open session in focus pane", onClick: () => openInFocus("session", { projectId: s.project, sessionId: s.id }) },
            { sep: true },
            { label: "Copy session id",           onClick: () => { navigator.clipboard?.writeText(s.id); toast("Copied " + s.id); } },
          ]);
        },
      },
        el("span", { class: "glyph", text: s.live ? "▸" : "○" }),
        el("span", { class: "name", text: s.title }),
        el("span", { class: "proj", text: proj?.name || s.project }),
        s.live ? el("span", { class: "live-dot" }) : null,
      );
      attachDrag(row, {
        source: "session",
        projectId: s.project,
        sessionId: s.id,
        taskId: s.taskId,
        title: s.title,
      });
      list.appendChild(row);
    }
    sessSection.appendChild(list);
  }
  bar.appendChild(sessSection);

  // Runners section (drop targets for features)
  const runSection = el("div", { class: "sb-section" },
    el("div", {
      class: `sb-head ${!state.sidebarSection.runners ? "collapsed" : ""}`,
      onclick: () => { state.sidebarSection.runners = !state.sidebarSection.runners; save(); render(); },
    },
      el("span", { class: "caret", text: "▾" }),
      "Runners",
      el("span", { class: "count", text: `${MOCK_RUNNERS.filter(r => r.status === "online").length}/${MOCK_RUNNERS.length}` }),
    ),
  );
  if (state.sidebarSection.runners) {
    const list = el("div", { class: "sb-list" });
    for (const r of MOCK_RUNNERS) {
      const assignedFeatures = Object.entries(state.featureAssignments)
        .filter(([, rid]) => rid === r.id)
        .map(([fid]) => fid);
      const stateGlyph = r.status === "online" ? "on" : r.status === "stale" ? "err" : "";
      const row = el("div", {
        class: `runner-row`,
        dataset: { runnerId: r.id, dropTarget: "runner" },
        onclick: () => openModal("runner", { runnerId: r.id }),
        oncontextmenu: (e) => {
          e.preventDefault();
          showContextMenu(e.clientX, e.clientY, [
            { label: "Runner details",     onClick: () => openModal("runner", { runnerId: r.id }) },
            { label: r.status === "online" ? "Take offline" : "Bring online", onClick: () => toast("Toggle runner status (mock)") },
            { sep: true },
            { label: "Clear all assignments", onClick: () => {
              for (const [fid, rid] of Object.entries(state.featureAssignments)) if (rid === r.id) delete state.featureAssignments[fid];
              save(); render(); toast(`Cleared ${r.name}`);
            }},
          ]);
        },
      },
        el("span", { class: `dot ${stateGlyph}` }),
        el("div", { class: "runner-body" },
          el("div", { class: "runner-name", text: r.name }),
          el("div", { class: "runner-meta" },
            el("span", { text: `${r.running}/${r.capacity}` }),
            el("span", { text: r.os }),
            el("span", { text: r.executor }),
          ),
          assignedFeatures.length ? el("div", { class: "runner-assign" },
            ...assignedFeatures.map(fid => el("span", {
              class: "chip mini",
              title: `Unassign ${fid}`,
              onclick: (e) => { e.stopPropagation(); delete state.featureAssignments[fid]; save(); render(); toast(`Unassigned ${fid}`); },
            }, fid))
          ) : null,
        ),
      );
      list.appendChild(row);
    }
    runSection.appendChild(list);
  }
  bar.appendChild(runSection);

  // Footer user + settings
  bar.appendChild(el("div", { class: "sb-foot" },
    el("span", { class: "avatar", text: "e" }),
    el("span", { text: "e367212" }),
    el("span", { class: "spacer", style: { flex: 1 } }),
    el("button", { class: "icon-btn", title: `Theme: ${themeLabel()}`, text: themeLabel(), onclick: cycleTheme }),
    el("button", { class: "icon-btn", title: "Settings", text: "⚙", onclick: () => openModal("settings") }),
  ));

  return bar;
}

function renderSidebarRestore() {
  return el("button", {
    class: "sidebar-restore",
    title: "Show sidebar",
    onclick: () => { state.sidebarCollapsed = false; save(); render(); },
  }, "☰", el("span", { text: "sidebar" }));
}

function renderAssistantPanel() {
  const scoped = state.selectedProject === "all" ? "all projects" : state.selectedProject;
  const attention = allAttentionFeatures();
  return el("aside", { class: "assistant-panel" },
    el("div", { class: "assistant-head" },
      el("div", {},
        el("div", { class: "assistant-kicker", text: "Brain assistant" }),
        el("h3", { text: `Workflow copilot · ${scoped}` }),
      ),
      el("button", { class: "drawer-close", text: "×", onclick: () => { state.assistantOpen = false; save(); render(); } }),
    ),
    el("div", { class: "assistant-card primary" },
      el("div", { class: "assistant-title", text: "Suggested next move" }),
      el("p", { text: attention.length ? `Review ${attention[0].feature.name}; it is ${attention[0].lifecycle.label.toLowerCase()} and blocking clean execution.` : "Queue the next ready feature and keep Brain entries updated as work lands." }),
      el("div", { class: "assistant-actions" },
        attention[0] ? el("button", { text: "Open suggestion", onclick: () => openFeatureDrawer(attention[0].project.id, attention[0].feature.id) }) : null,
        el("button", { text: "Run next", onclick: () => toast("Assistant queued next feature (mock)") }),
      ),
    ),
    el("div", { class: "assistant-card" },
      el("div", { class: "assistant-title", text: "Ask / command" }),
      el("textarea", { placeholder: "Ask about project status, generate tasks, summarize entries, or plan the next feature..." }),
      el("div", { class: "assistant-actions" },
        el("button", { class: "primary", text: "Send", onclick: () => toast("Assistant response streamed (mock)") }),
        el("button", { text: "Summarize scope", onclick: () => toast(`Summarized ${scoped} (mock)`) }),
      ),
    ),
    el("div", { class: "assistant-card" },
      el("div", { class: "assistant-title", text: "Quick actions" }),
      el("button", { text: "Create tasks from selected feature", onclick: () => toast("Created follow-up tasks (mock)") }),
      el("button", { text: "Draft Brain entry from current context", onclick: () => openModal("entry", { entryId: MOCK_ENTRIES[0].id }) }),
      el("button", { text: "Audit automations", onclick: () => { state.savedView = "automation"; save(); render(); } }),
      el("button", { text: "Open command palette", onclick: () => { state.commandPalette = true; save(); render(); } }),
    ),
    el("div", { class: "assistant-card" },
      el("div", { class: "assistant-title", text: "Context" }),
      el("p", { text: `${executionQueue().length} executable features · ${automationEvents(state.selectedProject).length} automation signals · ${MOCK_ENTRIES.length} Brain entries indexed.` }),
    ),
  );
}

function renderFeatureDrawer() {
  const { projectId, featureId } = state.drawer || {};
  const feature = (MOCK_FEATURES[projectId] || []).find(f => f.id === featureId);
  if (!feature) return el("aside", { class: "feature-drawer" }, el("button", { class: "drawer-close", text: "×", onclick: closeDrawer }), "Feature not found");
  const meta = FEATURE_META[feature.id] || {};
  const lifecycle = featureLifecycle(feature);
  const runner = runnerForFeature(feature);
  const linked = entriesForFeature(feature.id);
  const tasks = (MOCK_TASKS[projectId] || []).filter(t => t.feat === feature.id);
  return el("aside", { class: "feature-drawer" },
    el("div", { class: "drawer-head" },
      el("div", {},
        el("div", { class: "drawer-kicker", text: `${projectId} · ${feature.id}` }),
        el("h3", { text: feature.name }),
      ),
      el("button", { class: "drawer-close", text: "×", onclick: closeDrawer }),
    ),
    renderFeatureTrail(feature),
    el("div", { class: "drawer-actions" },
      el("button", { class: "primary", text: "Run", onclick: () => toast(`Run ${feature.id} (mock)`) }),
      el("button", { text: "Queue", onclick: () => toast(`Queued ${feature.id} (mock)`) }),
      el("button", { text: "Assign runner", onclick: () => showContextMenu(innerWidth - 340, 180, featureContextMenu(projectId, feature.id)) }),
      el("button", { text: "Open modal", onclick: () => openModal("feature", { projectId, featureId: feature.id }) }),
    ),
    el("div", { class: "drawer-section" },
      el("h4", { text: "Status" }),
      el("div", { class: "kv-grid" },
        el("div", { class: "k", text: "Lifecycle" }), el("div", { class: "v" }, el("span", { class: `life-badge ${lifecycle.tone}`, text: lifecycle.label })),
        el("div", { class: "k", text: "Age" }), el("div", { class: "v", text: featureAge(feature) }),
        el("div", { class: "k", text: "Runner" }), el("div", { class: "v", text: runner ? runner.name : "unassigned" }),
        el("div", { class: "k", text: "Branch" }), el("div", { class: "v", text: meta.targetBranch || "main" }),
      )
    ),
    el("div", { class: "drawer-section" },
      el("h4", { text: "Tasks" }),
      ...tasks.map(t => el("div", { class: "drawer-task", onclick: () => openModal("task", { projectId, taskId: t.id }) },
        el("span", { text: t.status }),
        el("b", { text: t.title }),
      )),
    ),
    el("div", { class: "drawer-section" },
      el("h4", { text: "Linked Brain entries" }),
      linked.length ? linked.map(entry => renderEntryCard(entry)) : el("div", { class: "empty-note", text: "No linked memory yet. Create a decision/runbook before executing." }),
      el("button", { text: "+ Link/new entry", onclick: () => openModal("entry", { entryId: linked[0]?.id || MOCK_ENTRIES[0].id }) }),
    ),
  );
}

function renderCommandPalette() {
  const commands = [
    ["Run next ready feature", () => toast("Dispatch next ready feature (mock)")],
    ["Open Brain entries", () => openInFocus("entries", {})],
    ["Open automation center", () => openInFocus("automations", {})],
    ["Show review queue", () => { state.savedView = "review"; save(); render(); }],
    ["Toggle sidebar", () => { state.sidebarCollapsed = !state.sidebarCollapsed; save(); render(); }],
    ["Switch theme", cycleTheme],
  ];
  return el("div", { class: "palette-scrim", onclick: (e) => { if (e.target.className === "palette-scrim") { state.commandPalette = false; save(); render(); } } },
    el("div", { class: "command-palette" },
      el("input", { type: "search", placeholder: "Type a command, project, feature, or entry...", autofocus: true }),
      el("div", { class: "palette-list" },
        ...commands.map(([label, action]) => el("button", { onclick: () => { state.commandPalette = false; action(); save(); render(); } },
          el("span", { text: label }),
          el("kbd", { text: "Enter" }),
        )),
      )
    )
  );
}

// ---------- mobile top nav ----------
function renderMobileNav() {
  return el("div", { class: "mobile-nav" },
    el("span", { class: `pill ${state.view === "overview" ? "active" : ""}`, text: "Overview",
      onclick: () => { state.view = "overview"; save(); render(); } }),
    el("span", { class: `pill ${state.view === "focus" ? "active" : ""}`, text: "Focus",
      onclick: () => { state.view = "focus"; save(); render(); } }),
    ...MOCK_SESSIONS.filter(s => s.live).map(s =>
      el("span", { class: `pill ${state.view === "session" && state.focusSessionId === s.id ? "active" : ""}`,
        onclick: () => { state.view = "session"; state.focusSessionId = s.id; save(); render(); } },
        el("span", { class: "live-dot", style: { display: "inline-block", verticalAlign: "middle", marginRight: "4px" } }),
        s.title
      )
    ),
  );
}

// ---------- workspace ----------
function renderWorkspace() {
  const ws = el("div", { class: "workspace" });
  if (state.view === "session" && state.focusSessionId) {
    ws.appendChild(renderSessionFull(state.focusSessionId));
  } else if (state.view === "focus") {
    if (!state.focusLayout) {
      // Empty focus mode: prompt user
      ws.appendChild(el("div", {
        style: { padding: "40px", color: "#6b757e", display: "flex", flexDirection: "column", gap: "12px", alignItems: "center" }
      },
        el("div", { style: { fontSize: "13px", color: "#eaedef" }, text: "Focus workspace" }),
        el("div", { style: { fontSize: "11px" }, text: "Drag a task, session, or project card here — or click a suggestion:" }),
        el("div", { style: { display: "flex", gap: "6px", flexWrap: "wrap", justifyContent: "center" } },
          el("button", { text: "＋ Task list (orion-ai)", onclick: () => openInFocus("task-list", { projectId: "orion-ai" }) }),
          el("button", { text: "＋ Live session (rate limiter)", onclick: () => openInFocus("session", { projectId: "orion-ai", sessionId: "S-a1" }) }),
          el("button", { text: "＋ Runners", onclick: () => openInFocus("runners", {}) }),
          el("button", { text: "＋ Terminal", onclick: () => openInFocus("terminal", {}) }),
        ),
      ));
    } else {
      ws.appendChild(renderPaneNode(state.focusLayout));
    }
  } else {
    // Default: Overview grid
    ws.appendChild(renderOverviewGrid());
  }
  return ws;
}

// ---------- Overview grid ----------
function renderOverviewGrid() {
  const grid = el("div", { class: "overview" });
  grid.appendChild(el("div", { class: "scopebar" },
    el("div", { class: "hint" }, el("span", { text: "◆" }), "Every project you care about, live and side-by-side. Use lifecycle filters to move from active work to review and merge queues."),
    el("select", { onchange: (e) => selectProject(e.target.value) },
      el("option", { value: "all", selected: state.selectedProject === "all", text: "All projects" }),
      ...MOCK_PROJECTS.map(p => el("option", { value: p.id, selected: state.selectedProject === p.id, text: p.name })),
    ),
  ));
  grid.appendChild(renderWorkflowCenter());
  const attentionQueue = renderAttentionQueue();
  if (attentionQueue) grid.appendChild(attentionQueue);
  if (["automation", "execution", "review"].includes(state.savedView)) grid.appendChild(renderAutomationTimeline());
  grid.appendChild(renderLifecycleBoard());
  grid.appendChild(renderEntriesPreview());
  const visible = state.cardOrder.filter(pid => isProjectVisible(pid));
  for (const pid of visible) {
    grid.appendChild(renderProjectCard(pid));
  }
  if (visible.length === 0) {
    grid.appendChild(el("div", { class: "empty-state" },
      el("div", { text: "No project cards match the current filters." }),
      el("button", { text: "Clear filters", onclick: () => { state.filters = { status: "all", lifecycle: "all", env: "all" }; save(); render(); } }),
      el("button", { text: "Open Brain entries anyway", onclick: () => openInFocus("entries", {}) }),
    ));
  }
  return grid;
}

function renderProjectCard(pid) {
  const proj = MOCK_PROJECTS.find(p => p.id === pid);
  const stats = projStats(pid);
  const health = projectHealth(pid);
  const activeTab = state.activeCardTab[pid] || "tasks";
  const card = el("div", {
    class: "pcard",
    dataset: { project: pid },
  });
  // Head
  const head = el("div", { class: "pcard-head" },
    el("span", { class: `dot ${stats.active ? "busy" : "on"}` }),
    el("span", { class: "name", text: proj.name }),
    el("span", { class: "env", text: proj.env }),
    el("span", { class: `health ${health.tone}`, text: health.label }),
    el("span", { class: "spacer" }),
    el("span", { class: "stats" },
      el("span", { class: "active",  html: `<b>${stats.active}</b> active` }),
      el("span", { class: "ready",   html: `<b>${stats.ready}</b> ready` }),
      el("span", { class: "blocked", html: `<b>${stats.blocked}</b> blocked` }),
      el("span", { html: `<b>${stats.completed}</b> done` }),
    ),
    el("button", {
      class: "close", title: "Hide", text: "×",
      onclick: (e) => { e.stopPropagation(); state.openProjects = state.openProjects.filter(x => x !== pid); save(); render(); },
    }),
  );
  attachDrag(head, {
    source: "project",
    projectId: pid,
    title: proj.name,
  });
  card.appendChild(head);
  card.appendChild(renderLifecycleStrip(pid));
  // Tabs
  const tabs = el("div", { class: "pcard-tabs" },
    ...[
      ["tasks",       "Tasks"],
      ["features",    "Features"],
    ].map(([k, lbl]) =>
      el("button", {
        class: activeTab === k ? "active" : "",
        text: lbl,
        onclick: () => { state.activeCardTab[pid] = k; save(); render(); },
      })
    ),
    el("button", { class: ["automations", "session", "logs"].includes(activeTab) ? "active" : "", text: "More",
      onclick: (e) => {
        showContextMenu(e.clientX, e.clientY, [
          { label: "Automations", onClick: () => { state.activeCardTab[pid] = "automations"; save(); render(); } },
          { label: "Session",     onClick: () => { state.activeCardTab[pid] = "session"; save(); render(); } },
          { label: "Logs",        onClick: () => { state.activeCardTab[pid] = "logs"; save(); render(); } },
        ]);
      }
    }),
    el("span", { class: "spacer" }),
    el("button", { class: "icon", title: "Open in focus", text: "⤢",
      onclick: () => openInFocus(activeTab === "session" ? "session" : activeTab === "automations" ? "automations" : activeTab === "features" ? "entries" : "task-list",
        activeTab === "session"
          ? { projectId: pid, sessionId: projSessions(pid)[0]?.id }
          : { projectId: pid })
    }),
  );
  card.appendChild(tabs);
  // Body
  const body = el("div", { class: "pcard-body" });
  if (activeTab === "tasks") body.appendChild(renderCardTasks(pid));
  else if (activeTab === "features") body.appendChild(renderCardFeatures(pid));
  else if (activeTab === "automations") body.appendChild(renderCardAutomations(pid));
  else if (activeTab === "session") body.appendChild(renderCardSession(pid));
  else if (activeTab === "logs") body.appendChild(renderCardLogs(pid));
  card.appendChild(body);
  return card;
}

function renderCardTasks(pid) {
  const wrap = el("div");
  const tasks = MOCK_TASKS[pid] || [];
  // Group by feature
  const byFeat = new Map();
  for (const t of tasks) {
    if (!byFeat.has(t.feat)) byFeat.set(t.feat, []);
    byFeat.get(t.feat).push(t);
  }
  const features = MOCK_FEATURES[pid] || [];
  for (const f of features) {
    const items = byFeat.get(f.id) || [];
    const featEl = el("div", { class: `feat ${f.state}` });
    const assignedRunner = state.featureAssignments[f.id];
    const runner = MOCK_RUNNERS.find(r => r.id === assignedRunner);
    const lifecycle = featureLifecycle(f);
    const warn = runnerWarning(f);
    const featHead = el("div", {
      class: "feat-head",
      onclick: (e) => {
        if (e.target.closest("button, .caret, .assign-chip")) return;
        openFeatureDrawer(pid, f.id);
      },
      oncontextmenu: (e) => {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, featureContextMenu(pid, f.id));
      },
    },
      el("span", { class: "caret", text: "▾" }),
      el("span", { class: "name", text: f.name }),
      el("span", { class: `life-badge ${lifecycle.tone}`, text: lifecycle.label }),
      el("span", { class: "age", text: featureAge(f) }),
      runner
        ? el("span", { class: `assign-chip ${warn ? "warn" : ""}`, title: warn || "Click to unassign", onclick: (e) => { e.stopPropagation(); delete state.featureAssignments[f.id]; save(); render(); toast(`Unassigned ${f.id}`); } },
            warn ? "⚠ " : "🖥 ", warn || runner.name.replace("runner-", ""))
        : el("span", { class: "assign-chip empty", title: "Drag onto a runner in the sidebar to assign", text: "· unassigned ·" }),
      el("span", { class: "bar" }, el("i", { style: { width: `${Math.round(f.progress * 100)}%` } })),
      el("span", { class: "prog", text: `${Math.round(f.progress * 100)}%` }),
    );
    // Make the whole feature header draggable → drop onto a runner to assign
    attachDrag(featHead, { source: "feature", projectId: pid, featureId: f.id, title: f.name });
    featEl.appendChild(featHead);
    for (const t of items) {
      const glyph = t.status === "Active" ? "▸"
                  : t.status === "Blocked" ? "✕"
                  : t.status === "Completed" ? "✓"
                  : "▪";
      const gc = t.status === "Active" ? "busy" : t.status === "Blocked" ? "blk" : t.status === "Completed" ? "ok" : "";
      const row = el("div", {
        class: "trow",
        onclick: (e) => { if (e.target.closest("button, .close")) return; openModal("task", { projectId: pid, taskId: t.id }); },
        oncontextmenu: (e) => {
          e.preventDefault();
          showContextMenu(e.clientX, e.clientY, [
            { label: "View metadata (modal)",       onClick: () => openModal("task", { projectId: pid, taskId: t.id }) },
            { label: "Open detail in focus pane",   onClick: () => openInFocus("task-detail", { projectId: pid, taskId: t.id }) },
            { label: "Open logs in focus pane",     onClick: () => openInFocus("logs", { projectId: pid, taskId: t.id }) },
            { label: "Open session in focus pane",  onClick: () => {
              const sess = MOCK_SESSIONS.find(s => s.taskId === t.id);
              if (sess) openInFocus("session", { projectId: pid, sessionId: sess.id });
              else toast("No session for this task");
            }},
          ]);
        },
      },
        el("span", { class: `glyph ${gc}`, text: glyph }),
        el("span", { class: "name", text: t.title }),
        el("span", { class: "status", text: t.status }),
        el("span", { class: "id", text: t.id }),
      );
      attachDrag(row, {
        source: "task",
        projectId: pid,
        taskId: t.id,
        title: t.title,
      });
      featEl.appendChild(row);
    }
    wrap.appendChild(featEl);
  }
  return wrap;
}

function featureContextMenu(pid, fid) {
  const currentRunner = state.featureAssignments[fid];
  return [
    { label: "View metadata (modal)", onClick: () => openModal("feature", { projectId: pid, featureId: fid }) },
    { sep: true },
    { lbl: "Assign to runner" },
    ...MOCK_RUNNERS.map(r => ({
      label: `${r.id === currentRunner ? "✓ " : "  "}${r.name}${r.status !== "online" ? ` (${r.status})` : ""}`,
      onClick: () => { state.featureAssignments[fid] = r.id; save(); render(); toast(`Assigned ${fid} → ${r.name}`); },
    })),
    currentRunner ? { label: "  ↺ Clear assignment", onClick: () => { delete state.featureAssignments[fid]; save(); render(); toast(`Unassigned ${fid}`); } } : null,
  ].filter(Boolean);
}

function renderCardFeatures(pid) {
  const wrap = el("div");
  for (const f of MOCK_FEATURES[pid] || []) {
    const ftasks = (MOCK_TASKS[pid] || []).filter(t => t.feat === f.id);
    const doneN = ftasks.filter(t => t.status === "Completed").length;
    const assignedRunner = state.featureAssignments[f.id];
    const runner = MOCK_RUNNERS.find(r => r.id === assignedRunner);
    const meta = FEATURE_META[f.id] || {};
    const lifecycle = featureLifecycle(f);
    const warn = runnerWarning(f);
    const card = el("div", { class: `feat ${f.state}`, style: { marginBottom: "8px", cursor: "pointer" } });
    const head = el("div", {
      class: "feat-head",
      onclick: (e) => { if (e.target.closest("button, .caret, .assign-chip")) return; openFeatureDrawer(pid, f.id); },
      oncontextmenu: (e) => { e.preventDefault(); showContextMenu(e.clientX, e.clientY, featureContextMenu(pid, f.id)); },
    },
      el("span", { class: "name", text: f.name }),
      el("span", { class: `life-badge ${lifecycle.tone}`, text: lifecycle.label }),
      el("span", { class: "age", text: featureAge(f) }),
      runner
        ? el("span", { class: `assign-chip ${warn ? "warn" : ""}`, title: warn || "Click to unassign", onclick: (e) => { e.stopPropagation(); delete state.featureAssignments[f.id]; save(); render(); toast(`Unassigned ${f.id}`); } },
            warn ? "⚠ " : "🖥 ", warn || runner.name.replace("runner-", ""))
        : el("span", { class: "assign-chip empty", text: "· unassigned ·" }),
      el("span", { class: "bar" }, el("i", { style: { width: `${Math.round(f.progress * 100)}%` } })),
      el("span", { class: "prog", text: `${doneN}/${ftasks.length}` }),
    );
    attachDrag(head, { source: "feature", projectId: pid, featureId: f.id, title: f.name });
    card.appendChild(head);
    card.appendChild(el("div", { style: { fontSize: "10.5px", color: "#9098a1", padding: "2px 0 2px 6px" }, text: meta.description || `${f.id} · ${f.state.toUpperCase()}` }));
    const linkedEntries = entriesForFeature(f.id);
    card.appendChild(el("div", { class: "feature-meta-row" },
      el("span", { text: `${f.id}` }),
      el("span", { text: `state: ${lifecycle.label}` }),
      el("span", { text: `age: ${featureAge(f)}` }),
      warn ? el("span", { class: "warn-text", text: `runner: ${warn}` }) : null,
      el("span", { text: `priority: ${meta.priority || "medium"}` }),
      el("span", { text: `checkout: ${meta.checkoutMode || "ai"}` }),
      el("span", { text: `→ ${meta.targetBranch || "main"}` }),
      el("span", { class: linkedEntries.length ? "entry-cue" : "entry-cue stale", text: linkedEntries.length ? `${linkedEntries.length} entries` : "no memory" }),
    ));
    wrap.appendChild(card);
  }
  return wrap;
}

function renderAutomationCenter(pid) {
  const projects = pid ? [MOCK_PROJECTS.find(p => p.id === pid)].filter(Boolean) : MOCK_PROJECTS;
  const wrap = el("div", { class: "automation-center" });
  for (const project of projects) {
    const stats = automationStats(project.id);
    wrap.appendChild(el("div", { class: "auto-project" },
      el("div", { class: "auto-project-head" },
        el("span", { class: "name", text: project.name }),
        el("span", { text: `${stats.active} active · ${stats.paused} paused · ${stats.failed} failed` }),
        el("button", { text: "Run all due", onclick: () => toast(`Run due automations for ${project.name} (mock)`) }),
      ),
      renderCardAutomations(project.id)
    ));
  }
  return wrap;
}

function renderEntriesPane(pid) {
  const entries = entriesForProject(pid);
  return el("div", { class: "entries-pane" },
    el("div", { class: "entries-toolbar" },
      el("input", { type: "search", placeholder: "Search Brain entries, links, frontmatter..." }),
      el("select", {},
        el("option", { text: "All types" }),
        el("option", { text: "Dreams" }),
        el("option", { text: "Decisions" }),
        el("option", { text: "Runbooks" }),
      ),
      el("button", { class: "primary", text: "+ New entry", onclick: () => openModal("entry", { entryId: MOCK_ENTRIES[0].id }) }),
    ),
    el("div", { class: "entries-split" },
      el("div", { class: "entries-list" },
        ...entries.map(entry => renderEntryCard(entry))
      ),
      el("div", { class: "entry-editor" },
        el("div", { class: "editor-head" },
          el("span", { text: entries[0]?.title || "No entry selected" }),
          el("button", { text: "Save", onclick: () => toast("Saved entry (mock)") }),
        ),
        el("textarea", { text: entries[0] ? `---\ntype: ${entries[0].type}\nproject: ${entries[0].project}\nlinks: [${entries[0].links.join(", ")}]\n---\n\n# ${entries[0].title}\n\n${entries[0].excerpt}\n\n## Next actions\n- Link this to the active feature plan\n- Update after automation runs` : "" }),
      ),
    )
  );
}

function renderCardAutomations(pid) {
  const wrap = el("div");
  const list = MOCK_AUTOMATIONS[pid] || [];
  if (!list.length) return el("div", { style: { color: "#6b757e", fontSize: "11px", padding: "8px" }, text: "No automations for this project." });
  for (const a of list) {
    const status = a.status === "active" ? "ok" : a.status === "paused" ? "wrn" : "";
    wrap.appendChild(el("div", {
      class: "auto-row",
      onclick: () => openModal("automation", { projectId: pid, automationId: a.id }),
      oncontextmenu: (e) => {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, [
          { label: "View metadata (modal)",       onClick: () => openModal("automation", { projectId: pid, automationId: a.id }) },
          { label: a.status === "active" ? "Pause" : "Resume", onClick: () => toast(`${a.status === "active" ? "Paused" : "Resumed"} ${a.name} (mock)`) },
          { label: "Run now",                     onClick: () => toast(`Triggered ${a.name} (mock)`) },
          { sep: true },
          { label: "Delete automation",           onClick: () => toast("Delete automation (mock)") },
        ]);
      },
    },
      el("div", { class: "auto-row-head" },
        el("span", { class: `dot ${status === "ok" ? "on" : "err"}` }),
        el("span", { class: "name", text: a.name }),
        el("span", { class: `pill ${status}`, text: a.status }),
      ),
      el("div", { class: "auto-row-meta" },
        el("span", { text: a.trigger }),
        el("span", { text: `next: ${a.nextRun || "event"}` }),
        el("span", { style: { marginLeft: "auto" }, text: `last: ${a.lastRun} · ${a.lastResult}` }),
      ),
      el("div", { class: "auto-row-watch" },
        ...(a.watches || []).map(w => el("span", { class: "chip mini", text: w })),
      ),
    ));
  }
  wrap.appendChild(el("button", { style: { fontSize: "10px", marginTop: "6px" }, text: "＋ New automation", onclick: () => toast("New automation (mock)") }));
  return wrap;
}

function renderCardSession(pid) {
  const sess = projSessions(pid);
  if (!sess.length) return el("div", { style: { color: "#6b757e", fontSize: "11px", padding: "8px" }, text: "No live sessions for this project." });
  const wrap = el("div");
  for (const s of sess) {
    wrap.appendChild(renderMiniSession(s));
  }
  return wrap;
}

function renderMiniSession(s) {
  const idx = state.sessionMsgs[s.id] ?? 0;
  const turns = STREAM_TURNS.slice(0, Math.max(2, Math.min(idx + 2, STREAM_TURNS.length)));
  return el("div", {},
    el("div", { style: { fontSize: "11px", color: "#eaedef", display: "flex", alignItems: "center", gap: "6px", marginBottom: "4px" } },
      s.live ? el("span", { class: "live-dot" }) : null,
      el("b", { text: s.title }),
      el("span", { style: { color: "#6b757e", marginLeft: "auto", fontSize: "10px" }, text: `${s.agent} · ${s.model}` }),
    ),
    ...turns.map(t => el("div", { class: "sess-mini" },
      el("span", { class: "who", text: t.role }),
      el("span", { class: "txt", text: t.text }),
    )),
    el("button", { style: { fontSize: "10px", marginTop: "4px" }, text: "Open full session ↗",
      onclick: () => { state.view = "session"; state.focusSessionId = s.id; save(); render(); },
    }),
  );
}

function renderCardLogs(pid) {
  const wrap = el("div", { class: "log-mini", dataset: { project: pid } });
  const sess = projSessions(pid);
  const taskIds = sess.map(s => s.taskId);
  wrap.appendChild(el("div", { class: "head" },
    el("span", { class: "live-dot" }),
    el("span", { class: "title", text: `Live: ${taskIds.join(", ") || "no active tasks"}` }),
    el("button", { text: "Clear", onclick: () => { for (const id of taskIds) delete state.logs[id]; render(); } }),
  ));
  const body = el("div", { class: "body", dataset: { logbody: pid } });
  renderLogLines(body, taskIds);
  wrap.appendChild(body);
  return wrap;
}

function renderLogLines(bodyEl, taskIds) {
  bodyEl.innerHTML = "";
  // Interleave logs from taskIds
  const all = [];
  for (const id of taskIds) {
    for (const l of (state.logs[id] || [])) all.push({ ...l, task: id });
  }
  const recent = all.slice(-30);
  for (const l of recent) {
    const cls = l.lvl === "ERROR" ? "err" : l.lvl === "OK" ? "ok" : l.lvl === "WARN" ? "wrn" : "";
    bodyEl.appendChild(el("div", { class: `l ${cls} ${l._new ? "new" : ""}` },
      el("span", { class: "ts", text: (l.ts || "").split(" ").pop() }),
      el("span", { class: "lvl", text: l.lvl }),
      el("span", { class: "msg", text: l.msg }),
    ));
    l._new = false;
  }
  bodyEl.scrollTop = bodyEl.scrollHeight;
}

// Called on every tick to update only .log-mini bodies (avoid full re-render)
function refreshLogMinis() {
  for (const bodyEl of $$(".log-mini .body")) {
    const pid = bodyEl.dataset.logbody;
    if (!pid) continue;
    const sess = projSessions(pid);
    renderLogLines(bodyEl, sess.map(s => s.taskId));
  }
  // Also update mini session bubbles that are currently visible
  refreshSessionCardTicker();
}
function refreshSessionCardTicker() {
  // For simplicity, re-render session mini in overview view only
  if (state.view !== "overview") return;
  for (const card of $$(".pcard")) {
    const pid = card.dataset.project;
    const active = state.activeCardTab[pid] || "tasks";
    if (active !== "session") continue;
    const body = card.querySelector(".pcard-body");
    if (body) { body.innerHTML = ""; body.appendChild(renderCardSession(pid)); }
  }
}
function refreshSessionView() {
  if (state.view !== "session") return;
  const container = $(".session-view .stream");
  if (!container) return;
  container.innerHTML = "";
  renderSessionStreamInto(container, state.focusSessionId);
  container.scrollTop = container.scrollHeight;
}

// ---------- Session full view ----------
function renderSessionFull(sid) {
  const s = MOCK_SESSIONS.find(x => x.id === sid);
  if (!s) return el("div", { text: "Session not found." });
  const proj = MOCK_PROJECTS.find(p => p.id === s.project);
  const view = el("div", { class: "session-view" });
  view.appendChild(el("div", { class: "hdr" },
    el("span", { style: { color: "#f4b23a" }, text: proj.name }),
    el("span", { style: { color: "#6b757e" }, text: "›" }),
    el("span", { text: s.title }),
    el("span", { style: { color: "#6b757e" }, text: `· ${s.taskId} · ${s.agent} · ${s.model}` }),
    s.live ? el("span", { style: { display: "inline-flex", alignItems: "center", gap: "4px", marginLeft: "8px", color: "#6fca7d", fontSize: "10px" } },
      el("span", { class: "live-dot" }),
      "streaming",
    ) : null,
    el("span", { class: "spacer", style: { flex: 1 } }),
    el("button", { text: "◀ Overview", onclick: () => { state.view = "overview"; save(); render(); } }),
    el("button", { text: "Focus split ⤢", onclick: () => openInFocus("session", { projectId: s.project, sessionId: s.id }) }),
  ));
  const stream = el("div", { class: "stream" });
  renderSessionStreamInto(stream, sid);
  view.appendChild(stream);

  // Right rail metadata
  const rail = el("div", { class: "sidebar-r" },
    el("h5", { text: "Session" }),
    el("div", { class: "kv" },
      el("b", { text: "ID" }),      el("span", { text: s.id }),
      el("b", { text: "Task" }),    el("span", { text: s.taskId }),
      el("b", { text: "Project" }), el("span", { text: s.project }),
      el("b", { text: "Agent" }),   el("span", { text: s.agent }),
      el("b", { text: "Model" }),   el("span", { text: s.model }),
    ),
    el("h5", { text: "Related task" }),
    (() => {
      const t = (MOCK_TASKS[s.project] || []).find(x => x.id === s.taskId);
      return t ? el("div", { class: "kv" },
        el("b", { text: "Title" }),  el("span", { text: t.title }),
        el("b", { text: "Status" }), el("span", { text: t.status }),
        el("b", { text: "Feature" }), el("span", { text: t.feat }),
      ) : el("div", { style: { color: "#6b757e" }, text: "(no task linked)" });
    })(),
    el("h5", { text: "Live logs" }),
    (() => {
      const wrap = el("div", { class: "log-mini", style: { border: "0", padding: 0 } });
      const body = el("div", { class: "body", dataset: { logbody: s.project } });
      renderLogLines(body, [s.taskId]);
      wrap.appendChild(body);
      return wrap;
    })(),
    el("h5", { text: "Tools" }),
    el("div", { style: { display: "flex", gap: "4px", flexWrap: "wrap" } },
      el("button", { text: "Open logs pane" , onclick: () => openInFocus("logs", { projectId: s.project, taskId: s.taskId }) }),
      el("button", { text: "Open terminal",   onclick: () => openInFocus("terminal", {}) }),
      el("button", { text: "New side chat",   onclick: () => toast("Side chat (mock)") }),
    ),
  );
  view.appendChild(rail);

  // Composer
  const composer = el("div", { class: "composer" },
    el("input", { type: "text", placeholder: `Message the ${s.agent} agent…` }),
    el("button", { text: "Send" }),
  );
  view.appendChild(composer);

  return view;
}

function renderSessionStreamInto(container, sid) {
  const idx = state.sessionMsgs[sid] ?? 0;
  const turns = STREAM_TURNS.slice(0, Math.max(3, Math.min(idx + 3, STREAM_TURNS.length)));
  for (const t of turns) {
    container.appendChild(el("div", { class: `msg ${t.role}` },
      el("div", { class: "role" }, el("span", { text: t.role }), el("span", { style: { color: "#6b757e" }, text: "· just now" })),
      el("div", { text: t.text }),
    ));
  }
  if (state.streaming) {
    container.appendChild(el("div", { class: "msg assistant", style: { opacity: 0.55 } },
      el("div", { class: "role" }, "assistant", el("span", { style: { color: "#6b757e" }, text: "· typing…" })),
      el("div", { text: "▌" }),
    ));
  }
}

// ---------- registry-pane renderers (focus mode) ----------
function renderOverview() {
  return renderOverviewGrid();
}
function renderTaskList(pid) {
  const wrap = el("div");
  wrap.appendChild(el("div", { style: { fontSize: "11px", color: "#6b757e", marginBottom: "6px" } }, `Project: `, el("b", { text: pid })));
  wrap.appendChild(renderCardTasks(pid));
  return wrap;
}
function renderTaskDetail(leaf) {
  const { taskId, projectId } = leaf.target;
  const t = (MOCK_TASKS[projectId] || []).find(x => x.id === taskId);
  if (!t) return el("div", { text: `Task ${taskId} not found` });
  const sess = MOCK_SESSIONS.find(s => s.taskId === taskId);
  return el("div",{},
    el("h3", { style: { margin: "0 0 6px" }, text: t.title }),
    el("div", { style: { display: "grid", gridTemplateColumns: "80px 1fr", gap: "3px 8px", fontSize: "11.5px" } },
      el("b", { text: "ID" }),      el("span", { text: t.id }),
      el("b", { text: "Status" }),  el("span", { text: t.status }),
      el("b", { text: "Project" }), el("span", { text: projectId }),
      el("b", { text: "Feature" }), el("span", { text: t.feat || "-" }),
      el("b", { text: "Session" }), el("span", { text: sess?.id || "-" }),
    ),
    el("div", { style: { display: "flex", gap: "4px", marginTop: "8px" } },
      el("button", { text: "▶ Run" }),
      el("button", { text: "✓ Complete" }),
      el("button", { text: "✎ Edit" }),
      el("button", { text: "⊘ Cancel" }),
    ),
    sess ? el("div", { style: { marginTop: "10px" } },
      el("div", { style: { fontSize: "10px", color: "#6b757e", marginBottom: "4px" }, text: "LIVE SESSION" }),
      renderMiniSession(sess),
    ) : null,
  );
}
function renderSessionView(leaf) {
  return renderSessionFull(leaf.target.sessionId);
}
function renderLogsPane(leaf) {
  const wrap = el("div", { class: "log-mini", style: { border: "0", padding: 0 } });
  wrap.appendChild(el("div", { class: "head" },
    el("span", { class: "live-dot" }),
    el("span", { class: "title", text: `Task ${leaf.target.taskId}` }),
  ));
  const body = el("div", { class: "body", dataset: { logbody: leaf.target.projectId } });
  renderLogLines(body, [leaf.target.taskId]);
  wrap.appendChild(body);
  return wrap;
}
function renderRunners() {
  return el("div", {},
    el("h3", { style: { margin: "0 0 6px" }, text: "Runners" }),
    ...[
      { name: "runner-macbook-01", state: "online", tasks: 2 },
      { name: "runner-ci-eu",      state: "online", tasks: 0 },
      { name: "runner-orion-gpu",  state: "stale",  tasks: 0 },
    ].map(r => el("div", { style: { padding: "6px 8px", background: "#12161a", border: "1px solid #22272c", borderRadius: "4px", marginBottom: "4px", fontSize: "11px" } },
      el("b", { text: r.name }),
      el("span", { style: { color: "#6b757e", marginLeft: "6px" }, text: `· ${r.state}` }),
      el("span", { style: { color: "#f4b23a", marginLeft: "6px" }, text: `${r.tasks} tasks` }),
    ))
  );
}
function renderTerminal() {
  return el("pre", { style: { background: "#000", color: "#b8f0b8", padding: "8px", margin: 0, borderRadius: "4px", fontSize: "11px", height: "100%", overflow: "auto" } },
    "$ just build\n→ ok ✓ 2 binaries built (3.2s)\n$ ▌"
  );
}
function renderBrowser(leaf) {
  const url = leaf.target.url || "https://brain.local/docs";
  return el("div", { style: { display: "flex", flexDirection: "column", height: "100%" } },
    el("div", { style: { background: "#12161a", padding: "4px", display: "flex", gap: "4px" } },
      el("button", { text: "←" }), el("button", { text: "→" }), el("button", { text: "⟳" }),
      el("input", { value: url, style: { flex: 1, background: "#0a0c0e", border: "1px solid #22272c", color: "#eaedef", padding: "3px 6px", borderRadius: "3px" } }),
    ),
    el("div", { style: { flex: 1, background: "#f5f5f5", color: "#222", padding: "20px", marginTop: "4px", borderRadius: "3px", overflow: "auto" } },
      el("h2", { text: "In-app browser (mock)" }),
      el("p", { text: `Loaded: ${url}` }),
    ),
  );
}

// ---------- Focus workspace: pane node tree ----------
function firstLeaf(node) { let f = null; walkNode(node, (n) => { if (n.type === "leaf") { f = n.leaf; return true; } if (n.type === "tabs") { f = n.children[0]; return true; } }); return f; }
function walkNode(node, visit) {
  if (!node) return;
  if (visit(node) === true) return;
  if (node.type === "split") { walkNode(node.children[0], visit); walkNode(node.children[1], visit); }
}
function findLeafInfo(root, id) {
  let f = null;
  const w = (n, parent, key) => {
    if (!n) return;
    if (n.type === "leaf" && n.leaf.id === id) { f = { holder: "leaf", node: n, parent, key }; return true; }
    if (n.type === "tabs") { const i = n.children.findIndex(c => c.id === id); if (i >= 0) { f = { holder: "tabs", container: n, index: i, parent, key }; return true; } }
    if (n.type === "split") { if (w(n.children[0], n, 0)) return true; if (w(n.children[1], n, 1)) return true; }
  };
  w(root, null, null);
  return f;
}
function replaceInTree(rootRef, oldNode, newNode) {
  if (state.focusLayout === oldNode) { state.focusLayout = newNode; return; }
  walkNode(state.focusLayout, (n) => {
    if (n.type === "split") {
      if (n.children[0] === oldNode) { n.children[0] = newNode; return true; }
      if (n.children[1] === oldNode) { n.children[1] = newNode; return true; }
    }
  });
}
function removeFromTree(node) {
  if (state.focusLayout === node) { state.focusLayout = null; return; }
  walkNode(state.focusLayout, (n) => {
    if (n.type === "split") {
      if (n.children[0] === node) { replaceInTree(null, n, n.children[1]); return true; }
      if (n.children[1] === node) { replaceInTree(null, n, n.children[0]); return true; }
    }
  });
}

function openInFocus(kind, target) {
  state.view = "focus";
  const newLeaf = { id: uid("l"), kind, target };
  if (!state.focusLayout) {
    state.focusLayout = { type: "leaf", leaf: newLeaf };
  } else {
    // dock into first tabs, else split right
    let placed = false;
    walkNode(state.focusLayout, (n) => {
      if (placed) return true;
      if (n.type === "tabs") { n.children.push(newLeaf); n.activeId = newLeaf.id; placed = true; return true; }
    });
    if (!placed) {
      state.focusLayout = {
        type: "split", axis: "row", ratio: 0.5,
        children: [state.focusLayout, { type: "leaf", leaf: newLeaf }],
      };
    }
  }
  save(); render();
  toast(`Opened ${PANE_KINDS[kind].label} in focus`);
}

function closeLeafInFocus(leafId) {
  const info = findLeafInfo(state.focusLayout, leafId);
  if (!info) return;
  if (info.holder === "tabs") {
    info.container.children.splice(info.index, 1);
    if (info.container.children.length === 1) {
      const remaining = info.container.children[0];
      replaceInTree(null, info.container, { type: "leaf", leaf: remaining });
    } else if (info.container.activeId === leafId) {
      info.container.activeId = info.container.children[0].id;
    }
  } else {
    removeFromTree(info.node);
  }
  save(); render();
}

function renderPaneNode(node) {
  if (!node) return el("div", { style: { padding: "20px", color: "#6b757e" }, text: "Empty" });
  if (node.type === "split") {
    const outer = el("div", { class: `node split-${node.axis}`, style: { flex: 1, display: "flex", flexDirection: node.axis === "row" ? "row" : "column", minWidth: 0, minHeight: 0 } });
    const a = renderPaneNode(node.children[0]);
    const b = renderPaneNode(node.children[1]);
    a.style.flex = `${node.ratio}`; a.style.minWidth = 0; a.style.minHeight = 0;
    b.style.flex = `${1 - node.ratio}`; b.style.minWidth = 0; b.style.minHeight = 0;
    const splitter = el("div", { class: `splitter ${node.axis === "row" ? "col" : "row"}` });
    attachSplitter(splitter, node, outer);
    outer.appendChild(a); outer.appendChild(splitter); outer.appendChild(b);
    return outer;
  }
  if (node.type === "tabs") return renderTabsPane(node);
  if (node.type === "leaf") return renderLeafPane(node.leaf);
}

function renderTabsPane(tabsNode) {
  const pane = el("div", { class: "pane" });
  const activeLeaf = tabsNode.children.find(c => c.id === tabsNode.activeId) ?? tabsNode.children[0];
  if (!activeLeaf) return pane;
  const tabbar = el("div", { class: "pane-tabbar" });
  for (const l of tabsNode.children) {
    const proj = MOCK_PROJECTS.find(p => p.id === l.target?.projectId);
    const tab = el("div", {
      class: `pane-tab ${l.id === tabsNode.activeId ? "active" : ""} ${l.pinned ? "pinned" : ""}`,
      onclick: () => { tabsNode.activeId = l.id; save(); render(); },
      oncontextmenu: (e) => {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, paneContextMenu(l));
      },
    },
      proj ? el("span", { class: "proj-badge", style: { color: proj.color }, text: proj.name.slice(0, 4) }) : null,
      el("span", { class: "kind", text: PANE_KINDS[l.kind]?.label ?? l.kind }),
      el("span", { class: "pane-tab-title", text: PANE_KINDS[l.kind]?.title(l) ?? l.kind }),
      el("span", { class: "close", text: "×", onclick: (e) => { e.stopPropagation(); closeLeafInFocus(l.id); } }),
    );
    attachPaneDrag(tab, l.id);
    tabbar.appendChild(tab);
  }
  pane.appendChild(tabbar);
  const body = el("div", { class: "pane-body" });
  body.appendChild(PANE_KINDS[activeLeaf.kind].render(activeLeaf, {}));
  pane.appendChild(body);
  attachDropZones(pane, activeLeaf.id);
  return pane;
}
function renderLeafPane(leaf) {
  const pane = el("div", { class: "pane" });
  const proj = MOCK_PROJECTS.find(p => p.id === leaf.target?.projectId);
  const tabbar = el("div", { class: "pane-tabbar" });
  const tab = el("div", {
    class: "pane-tab active",
    oncontextmenu: (e) => { e.preventDefault(); showContextMenu(e.clientX, e.clientY, paneContextMenu(leaf)); },
  },
    proj ? el("span", { class: "proj-badge", style: { color: proj.color }, text: proj.name.slice(0, 4) }) : null,
    el("span", { class: "kind", text: PANE_KINDS[leaf.kind]?.label ?? leaf.kind }),
    el("span", { class: "pane-tab-title", text: PANE_KINDS[leaf.kind]?.title(leaf) ?? leaf.kind }),
    el("span", { class: "close", text: "×", onclick: (e) => { e.stopPropagation(); closeLeafInFocus(leaf.id); } }),
  );
  attachPaneDrag(tab, leaf.id);
  tabbar.appendChild(tab);
  pane.appendChild(tabbar);
  const body = el("div", { class: "pane-body" });
  body.appendChild(PANE_KINDS[leaf.kind].render(leaf, {}));
  pane.appendChild(body);
  attachDropZones(pane, leaf.id);
  return pane;
}

function paneContextMenu(leaf) {
  return [
    { label: leaf.pinned ? "Unpin pane" : "Pin pane", onClick: () => { leaf.pinned = !leaf.pinned; save(); render(); } },
    { sep: true },
    { label: "Close pane", onClick: () => closeLeafInFocus(leaf.id) },
  ];
}

// ---------- Drag: unified for pane tabs, project cards, sessions, tasks ----------
let drag = null;
const ghost = $("#drag-ghost");

function attachDrag(node, payload) {
  let startPt = null, longPressTimer = null, suppressClick = false;
  node.addEventListener("click", (e) => {
    if (suppressClick) { e.preventDefault(); e.stopImmediatePropagation(); suppressClick = false; }
  }, true);
  node.addEventListener("pointerdown", (e) => {
    if (e.button === 2) return;
    if (e.target?.classList?.contains("close")) return;
    e.preventDefault();
    startPt = { x: e.clientX, y: e.clientY };
    try { node.setPointerCapture(e.pointerId); } catch {}

    if (state.mobile) {
      longPressTimer = setTimeout(() => { showMoveSheet(payload); longPressTimer = null; }, 500);
    }
    const onMove = (ev) => {
      const dx = ev.clientX - startPt.x, dy = ev.clientY - startPt.y;
      if (!drag && (Math.abs(dx) > 6 || Math.abs(dy) > 6)) {
        if (longPressTimer) { clearTimeout(longPressTimer); longPressTimer = null; }
        if (state.mobile) return;
        startDrag(payload, node);
        suppressClick = true;
      }
      if (drag) {
        ev.preventDefault();
        ghost.style.left = `${ev.clientX}px`;
        ghost.style.top  = `${ev.clientY}px`;
        updateDropTarget(ev.clientX, ev.clientY);
      }
    };
    const onUp = (ev) => {
      if (longPressTimer) { clearTimeout(longPressTimer); longPressTimer = null; }
      if (drag) endDrag(ev.clientX, ev.clientY);
      try { node.releasePointerCapture(e.pointerId); } catch {}
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
      document.removeEventListener("pointercancel", onUp);
    };
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
    document.addEventListener("pointercancel", onUp);
  });
}
function attachPaneDrag(node, leafId) {
  attachDrag(node, { source: "pane", leafId });
}

function startDrag(payload, sourceNode) {
  drag = { payload, target: null };
  ghost.textContent = `↳ ${payload.title || payload.leafId || "pane"}`;
  ghost.hidden = false;
  sourceNode.classList.add("dragging");
  document.querySelectorAll(".dropzones").forEach(dz => dz.classList.add("active"));
}
function endDrag(x, y) {
  ghost.hidden = true;
  $$(".dragging").forEach(n => n.classList.remove("dragging"));
  $$(".dz.hot").forEach(n => n.classList.remove("hot"));
  $$(".pcard.drop-target").forEach(n => n.classList.remove("drop-target"));
  $$(".runner-row.drop-target").forEach(n => n.classList.remove("drop-target"));
  $$(".dropzones").forEach(dz => dz.classList.remove("active"));

  if (drag?.target) doDrop(drag.payload, drag.target);
  drag = null;
}
function updateDropTarget(x, y) {
  $$(".dz.hot").forEach(n => n.classList.remove("hot"));
  $$(".pcard.drop-target").forEach(n => n.classList.remove("drop-target"));
  $$(".runner-row.drop-target").forEach(n => n.classList.remove("drop-target"));
  drag.target = null;
  const stack = document.elementsFromPoint(x, y);
  for (const el of stack) {
    if (el.classList?.contains("dz")) {
      el.classList.add("hot");
      const pane = el.closest(".pane");
      drag.target = { kind: "pane-edge", leafId: pane?.dataset?.leafId, edge: el.dataset?.edge };
      return;
    }
    // Runner drop target (only relevant when dragging a feature)
    const runnerRow = el.closest?.(".runner-row");
    if (runnerRow && drag.payload?.source === "feature") {
      runnerRow.classList.add("drop-target");
      drag.target = { kind: "runner", runnerId: runnerRow.dataset.runnerId };
      return;
    }
    if (el.classList?.contains("pcard")) {
      el.classList.add("drop-target");
      drag.target = { kind: "pcard", projectId: el.dataset?.project };
      return;
    }
    if (el.classList?.contains("workspace") && state.view === "focus") {
      drag.target = { kind: "focus-empty" };
      return;
    }
  }
}
function doDrop(payload, target) {
  // Feature → Runner assignment
  if (target.kind === "runner" && payload.source === "feature") {
    state.featureAssignments[payload.featureId] = target.runnerId;
    save(); render();
    const runner = MOCK_RUNNERS.find(r => r.id === target.runnerId);
    toast(`Assigned ${payload.featureId} → ${runner?.name || target.runnerId}`);
    return;
  }
  if (target.kind === "pane-edge") {
    // Dropping onto a pane edge: create/move
    if (payload.source === "pane" && payload.leafId) {
      // Move existing leaf
      movePaneLeaf(payload.leafId, target.leafId, target.edge);
    } else {
      // Create a new leaf from the payload
      const kind = kindFromPayload(payload);
      if (!kind) return;
      addLeafAtEdge(kind.kind, kind.target, target.leafId, target.edge);
    }
    return;
  }
  if (target.kind === "pcard") {
    // Dropped onto a project card — treat as "add this session/task to project overview focus"
    toast(`Dropped ${payload.title || "item"} onto ${target.projectId}`);
    return;
  }
  if (target.kind === "focus-empty") {
    const k = kindFromPayload(payload);
    if (k) openInFocus(k.kind, k.target);
    return;
  }
}
function kindFromPayload(p) {
  if (p.source === "task")    return { kind: "task-detail", target: { projectId: p.projectId, taskId: p.taskId } };
  if (p.source === "session") return { kind: "session",     target: { projectId: p.projectId, sessionId: p.sessionId } };
  if (p.source === "project") return { kind: "task-list",   target: { projectId: p.projectId } };
  if (p.source === "feature") return { kind: "task-list",   target: { projectId: p.projectId } };
  return null;
}
function addLeafAtEdge(kind, target, targetLeafId, edge) {
  state.view = "focus";
  const newLeaf = { id: uid("l"), kind, target };
  if (!state.focusLayout) { state.focusLayout = { type: "leaf", leaf: newLeaf }; save(); render(); return; }
  const info = findLeafInfo(state.focusLayout, targetLeafId);
  if (!info) { openInFocus(kind, target); return; }
  if (edge === "center") {
    if (info.holder === "tabs") { info.container.children.push(newLeaf); info.container.activeId = newLeaf.id; }
    else {
      const oldLeaf = info.node.leaf;
      const tabs = { type: "tabs", children: [oldLeaf, newLeaf], activeId: newLeaf.id };
      replaceInTree(null, info.node, tabs);
    }
  } else {
    const axis = (edge === "left" || edge === "right") ? "row" : "col";
    const first = (edge === "left" || edge === "top");
    const targetContainer = info.holder === "tabs" ? info.container : info.node;
    const newLeafNode = { type: "leaf", leaf: newLeaf };
    const split = { type: "split", axis, ratio: 0.5, children: first ? [newLeafNode, targetContainer] : [targetContainer, newLeafNode] };
    replaceInTree(null, targetContainer, split);
  }
  save(); render();
}
function movePaneLeaf(sourceLeafId, targetLeafId, edge) {
  if (sourceLeafId === targetLeafId) { toast("Drop cancelled (same pane)"); return; }
  const info = findLeafInfo(state.focusLayout, sourceLeafId);
  if (!info) return;
  let leafObj;
  if (info.holder === "tabs") {
    leafObj = info.container.children.splice(info.index, 1)[0];
    if (info.container.children.length === 1) {
      replaceInTree(null, info.container, { type: "leaf", leaf: info.container.children[0] });
    } else if (info.container.activeId === sourceLeafId) {
      info.container.activeId = info.container.children[0].id;
    }
  } else {
    leafObj = info.node.leaf; removeFromTree(info.node);
  }
  // Now re-insert
  const dstInfo = findLeafInfo(state.focusLayout, targetLeafId);
  if (!dstInfo) {
    // target no longer exists; put back
    if (!state.focusLayout) state.focusLayout = { type: "leaf", leaf: leafObj };
    else openInFocus(leafObj.kind, leafObj.target);
    return;
  }
  if (edge === "center") {
    if (dstInfo.holder === "tabs") { dstInfo.container.children.push(leafObj); dstInfo.container.activeId = leafObj.id; }
    else {
      const oldLeaf = dstInfo.node.leaf;
      const tabs = { type: "tabs", children: [oldLeaf, leafObj], activeId: leafObj.id };
      replaceInTree(null, dstInfo.node, tabs);
    }
  } else {
    const axis = (edge === "left" || edge === "right") ? "row" : "col";
    const first = (edge === "left" || edge === "top");
    const targetContainer = dstInfo.holder === "tabs" ? dstInfo.container : dstInfo.node;
    const newLeafNode = { type: "leaf", leaf: leafObj };
    const split = { type: "split", axis, ratio: 0.5, children: first ? [newLeafNode, targetContainer] : [targetContainer, newLeafNode] };
    replaceInTree(null, targetContainer, split);
  }
  save(); render();
}

// ---------- drop zones per pane ----------
function attachDropZones(paneEl, leafId) {
  paneEl.dataset.leafId = leafId;
  const dz = el("div", { class: "dropzones" });
  for (const edge of ["top", "right", "bottom", "left", "center"]) {
    dz.appendChild(el("div", { class: `dz ${edge}`, dataset: { edge } }));
  }
  paneEl.appendChild(dz);
}
function attachSplitter(splitEl, node, containerEl) {
  splitEl.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    splitEl.setPointerCapture(e.pointerId);
    splitEl.classList.add("active");
    const rect = containerEl.getBoundingClientRect();
    const onMove = (ev) => {
      const pos = node.axis === "row"
        ? (ev.clientX - rect.left) / rect.width
        : (ev.clientY - rect.top) / rect.height;
      node.ratio = clamp(pos, 0.1, 0.9);
      save(); render();
    };
    const onUp = () => {
      splitEl.classList.remove("active");
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
    };
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
  });
  splitEl.addEventListener("dblclick", () => { node.ratio = 0.5; save(); render(); });
}

// ---------- status bar ----------
function renderStatusbar() {
  const liveCount = MOCK_SESSIONS.filter(s => s.live).length;
  return el("div", { class: "statusbar" },
    el("span", { class: "live-dot" }),
    el("span", { text: state.streaming ? "streaming" : "paused" }),
    el("span", { text: `· ${liveCount} live` }),
    el("span", { text: `· ${state.openProjects.length} projects` }),
    el("span", { class: "spacer" }),
    el("span", { class: "usage" }, "context ",
      el("span", { class: "bar" }, el("i", { style: { width: "34%" } })),
      " 34%"),
    el("span", { class: "usage" }, "session ",
      el("span", { class: "bar" }, el("i", { style: { width: "12%" } })),
      " 12%"),
    el("span", { text: `· v2 wireframe` }),
  );
}

// ---------- context menu / sheet / toast ----------
const ctxmenu = $("#ctxmenu");
function showContextMenu(x, y, items) {
  ctxmenu.innerHTML = "";
  for (const it of items) {
    if (it.sep) { ctxmenu.appendChild(el("div", { class: "sep" })); continue; }
    ctxmenu.appendChild(el("button", { text: it.label, onclick: () => { hideContextMenu(); it.onClick?.(); } }));
  }
  ctxmenu.style.left = `${Math.min(x, window.innerWidth - 260)}px`;
  ctxmenu.style.top  = `${Math.min(y, window.innerHeight - 300)}px`;
  ctxmenu.hidden = false;
  setTimeout(() => document.addEventListener("mousedown", onDocMouseDown, { once: true }), 0);
}
function hideContextMenu() { ctxmenu.hidden = true; }
function onDocMouseDown(e) { if (!ctxmenu.contains(e.target)) hideContextMenu(); }

const sheetEl = $("#sheet"), sheetScrim = $("#sheet-scrim"), sheetBody = $("#sheet-body");
function showSheet(title, items) {
  sheetBody.innerHTML = "";
  sheetBody.appendChild(el("h4", { text: title }));
  for (const it of items) {
    sheetBody.appendChild(el("div", { class: "row", onclick: () => { hideSheet(); it.onClick?.(); } },
      el("div", { class: "icon", text: it.icon || "→" }), el("div", { text: it.label })
    ));
  }
  sheetEl.hidden = false; sheetScrim.hidden = false;
}
function hideSheet() { sheetEl.hidden = true; sheetScrim.hidden = true; }
sheetScrim.addEventListener("click", hideSheet);

function showMoveSheet(payload) {
  showSheet("Move", [
    ...(payload.source === "task" || payload.source === "session" || payload.source === "project"
      ? [
          { icon: "⤢", label: "Open in Focus", onClick: () => { const k = kindFromPayload(payload); if (k) openInFocus(k.kind, k.target); } },
          { icon: "▸", label: "Open in Overview", onClick: () => { state.view = "overview"; save(); render(); } },
        ]
      : []),
    { icon: "✕", label: "Cancel", onClick: () => {} },
  ]);
}

const toastEl = $("#toast");
let toastTimer;
function toast(msg) {
  toastEl.textContent = msg;
  toastEl.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toastEl.classList.remove("show"), 1400);
}

// ---------- modal ----------
const modalScrimEl = $("#modal-scrim");
const modalEl      = $("#modal");
const modalTitleEl = $("#modal-title");
const modalBodyEl  = $("#modal-body");
const modalFootEl  = $("#modal-foot");
let modalTab = null;

function openModal(kind, target = {}) {
  state.modal = { kind, ...target };
  modalTab = null; // reset tab
  renderModal();
}
function closeModal() {
  state.modal = null;
  modalScrimEl.hidden = true;
  modalEl.hidden = true;
  modalBodyEl.innerHTML = "";
  modalFootEl.innerHTML = "";
}
$("#modal-close").addEventListener("click", closeModal);
modalScrimEl.addEventListener("click", closeModal);
document.addEventListener("click", (e) => {
  const plan = e.target.closest?.('.wc-row button[data-action="plan"]');
  if (plan) {
    const row = plan.closest('.wc-row');
    if (row?.dataset.projectId && row?.dataset.featureId) openFeatureDrawer(row.dataset.projectId, row.dataset.featureId);
  }
  const palette = e.target.closest?.('button[data-action="command-palette"]');
  if (palette) {
    state.commandPalette = true;
    save();
    render();
  }
});

window.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && state.modal) closeModal();
});

function renderModal() {
  if (!state.modal) return;
  const { kind } = state.modal;
  modalScrimEl.hidden = false;
  modalEl.hidden = false;
  modalBodyEl.innerHTML = "";
  modalFootEl.innerHTML = "";
  // Strip any tabs from prior modal
  modalEl.querySelectorAll(".modal-tabs").forEach(n => n.remove());

  const dispatch = {
    runner:     renderRunnerModal,
    task:       renderTaskModal,
    feature:    renderFeatureModal,
    automation: renderAutomationModal,
    entry:      renderEntryModal,
    settings:   renderSettingsModal,
  };
  const fn = dispatch[kind];
  if (fn) fn(); else { modalTitleEl.textContent = kind; modalBodyEl.textContent = "No renderer for " + kind; }
}

// ---- Runner modal (Overview / Shell / Logs) ----
function renderRunnerModal() {
  const r = MOCK_RUNNERS.find(x => x.id === state.modal.runnerId);
  if (!r) { modalTitleEl.textContent = "Runner"; modalBodyEl.textContent = "Not found"; return; }
  modalTitleEl.textContent = "";
  modalTitleEl.append(
    el("span", { class: `dot ${r.status === "online" ? "on" : r.status === "stale" ? "err" : ""}`, style: {
      display: "inline-block", width: "8px", height: "8px", borderRadius: "50%",
      background: r.status === "online" ? "#6fca7d" : r.status === "stale" ? "#d96060" : "#4b545c",
      marginRight: "8px", verticalAlign: "middle",
    }}),
    r.name,
    el("span", { style: { color: "#6b757e", fontWeight: 400, marginLeft: "8px", fontSize: "11px" }, text: `· ${r.host}` }),
  );

  const tab = modalTab || "shell";
  // Remove any existing tabs strip before inserting new one
  modalEl.querySelectorAll(".modal-tabs").forEach(n => n.remove());
  const tabs = el("div", { class: "modal-tabs" },
    ...[["overview","Overview"],["shell","Shell"],["logs","Logs"]].map(([k,label]) =>
      el("div", { class: `modal-tab ${tab === k ? "active" : ""}`, text: label,
        onclick: () => { modalTab = k; renderModal(); } }),
    ),
  );
  modalBodyEl.parentElement.insertBefore(tabs, modalBodyEl);

  if (tab === "overview") {
    const assigned = Object.entries(state.featureAssignments).filter(([, rid]) => rid === r.id).map(([fid]) => fid);
    modalBodyEl.append(
      el("div", { class: "kv-grid" },
        el("div", { class: "k", text: "Status" }),      el("div", { class: "v", text: r.status }),
        el("div", { class: "k", text: "Host" }),        el("div", { class: "v", text: r.host }),
        el("div", { class: "k", text: "OS / Arch" }),   el("div", { class: "v", text: `${r.os} · ${r.arch}` }),
        el("div", { class: "k", text: "Executor" }),    el("div", { class: "v", text: r.executor }),
        el("div", { class: "k", text: "Capacity" }),    el("div", { class: "v", text: `${r.running} running / ${r.capacity} slots` }),
        el("div", { class: "k", text: "Labels" }),      el("div", { class: "v" },
          ...r.labels.map(l => el("span", { class: "chip mini", text: l, style: { marginRight: "4px" } }))
        ),
        el("div", { class: "k", text: "Last seen" }),   el("div", { class: "v", text: r.lastSeen }),
        el("div", { class: "k", text: "Assignments" }), el("div", { class: "v", text: assigned.length ? assigned.join(", ") : "· none ·" }),
      ),
    );
  } else if (tab === "shell") {
    modalBodyEl.append(renderRunnerShell(r));
  } else if (tab === "logs") {
    modalBodyEl.append(el("pre", {
      style: { background: "#0a0d10", color: "#eaedef", padding: "10px 12px", borderRadius: "4px",
               fontSize: "11px", lineHeight: "1.5", margin: 0, height: "340px", overflow: "auto",
               border: "1px solid #22272c" },
      text:
`[${r.lastSeen}] runner ${r.name} heartbeat ok
[${r.lastSeen}] executor=${r.executor} capacity=${r.capacity} running=${r.running}
[2m ago]  claimed task T-3001 (feat F-capacity)
[2m ago]  spawn opencode  workdir=~/code/orion-ai  branch=feature/push-dispatch-race
[1m ago]  session ses_abc123 idle → active
[45s ago] tool.execute  bash  "go test ./internal/dispatch/..."
[30s ago] tool.result    exit=0  duration=12.4s
[20s ago] message.part   "All tests pass."
[10s ago] session.status idle
[just now] task T-3001 completed`,
    }));
  }

  modalFootEl.append(
    el("button", { text: r.status === "online" ? "Take offline" : "Bring online",
      onclick: () => toast(`Toggle ${r.name} (mock)`) }),
    el("button", { class: "primary", text: "Close", onclick: closeModal }),
  );
}

function renderRunnerShell(r) {
  const wrap = el("div", { class: "runner-shell", tabindex: "0" });
  const history = [
    { type: "dim",  text: `Connected to ${r.host} via brain-runner shell (mock)` },
    { type: "dim",  text: `Runner: ${r.name}   OS: ${r.os}/${r.arch}   Executor: ${r.executor}` },
    { type: "dim",  text: `Type 'help' for available commands.` },
  ];
  const promptPs = `${r.name.replace("runner-", "")}:~ $`;

  const draw = () => {
    wrap.innerHTML = "";
    for (const line of history) {
      const cls = line.type === "err" ? "out-err" : line.type === "warn" ? "out-warn" : line.type === "dim" ? "out-dim" : "";
      wrap.append(el("div", { class: cls, text: line.text }));
    }
    const promptLine = el("div", { class: "prompt-line" },
      el("span", { class: "ps", text: promptPs }),
    );
    const input = el("input", { class: "shell-input", type: "text", autofocus: true,
      placeholder: "type a command…" });
    input.addEventListener("keydown", (e) => {
      if (e.key !== "Enter") return;
      const cmd = input.value.trim();
      history.push({ type: "cmd", text: `${promptPs} ${cmd}` });
      const out = mockShellRun(r, cmd);
      for (const l of out) history.push(l);
      draw();
      // scroll to bottom
      requestAnimationFrame(() => { wrap.scrollTop = wrap.scrollHeight; wrap.querySelector("input")?.focus(); });
    });
    promptLine.append(input);
    wrap.append(promptLine);
    requestAnimationFrame(() => { wrap.scrollTop = wrap.scrollHeight; input.focus(); });
  };
  draw();
  wrap.addEventListener("click", () => wrap.querySelector("input")?.focus());
  return wrap;
}

function mockShellRun(r, cmd) {
  if (!cmd) return [];
  const c = cmd.toLowerCase();
  if (c === "help") return [
    { type: "out", text: "Available (mock) commands:" },
    { type: "out", text: "  help          show this help" },
    { type: "out", text: "  status        runner status summary" },
    { type: "out", text: "  ps            running processes" },
    { type: "out", text: "  tasks         current claimed tasks" },
    { type: "out", text: "  logs          tail runner logs" },
    { type: "out", text: "  uname         system info" },
    { type: "out", text: "  clear         clear screen (mock; refresh modal)" },
    { type: "out", text: "  exit          close shell" },
  ];
  if (c === "status") return [
    { type: "out",  text: `runner: ${r.name}` },
    { type: "out",  text: `status: ${r.status}` },
    { type: "out",  text: `capacity: ${r.running}/${r.capacity}` },
    { type: "out",  text: `executor: ${r.executor}` },
    { type: "dim",  text: `last-seen: ${r.lastSeen}` },
  ];
  if (c === "ps") return [
    { type: "out", text: "PID    CMD" },
    { type: "out", text: "  431  brain-runner --project all" },
    { type: "out", text: " 1244  opencode --port 4100" },
    { type: "out", text: " 1289  git worktree list" },
  ];
  if (c === "tasks") {
    const assigned = Object.entries(state.featureAssignments).filter(([, rid]) => rid === r.id).map(([fid]) => fid);
    if (!assigned.length) return [{ type: "dim", text: "no active tasks assigned" }];
    return [
      { type: "out", text: `ID       FEAT         STATUS` },
      ...assigned.map(fid => ({ type: "out", text: `T-${1000 + Math.floor(Math.random()*9000)}  ${fid.padEnd(12)} running` })),
    ];
  }
  if (c === "logs") return [
    { type: "dim", text: "[+0.0s] runner heartbeat ok" },
    { type: "dim", text: "[+0.2s] claim task T-3001" },
    { type: "dim", text: "[+0.4s] spawn opencode  branch=feature/push-dispatch-race" },
    { type: "out", text: "[+1.1s] tool.result exit=0" },
  ];
  if (c === "uname" || c === "uname -a") return [
    { type: "out", text: `${r.os} ${r.host} ${r.arch} brain-runner/1.4.2 (mock)` },
  ];
  if (c === "clear") { /* handled by returning nothing; user hits refresh */ return [{ type: "dim", text: "(mock) reopen shell to clear" }]; }
  if (c === "exit" || c === "logout") { setTimeout(closeModal, 0); return [{ type: "dim", text: "closing shell…" }]; }
  return [{ type: "err", text: `brain-shell: command not found: ${cmd}` }];
}

// ---- Task modal ----
function renderTaskModal() {
  const { projectId, taskId } = state.modal;
  const task = (MOCK_TASKS[projectId] || []).find(t => t.id === taskId);
  const meta = TASK_META[taskId];
  modalTitleEl.textContent = task ? `${task.id} · ${task.title}` : `Task ${taskId}`;
  if (!task) { modalBodyEl.textContent = "Task not found"; return; }
  modalBodyEl.append(
    el("div", { class: "kv-grid" },
      el("div", { class: "k", text: "Project" }),   el("div", { class: "v", text: projectId }),
      el("div", { class: "k", text: "Feature" }),   el("div", { class: "v", text: task.feat || "—" }),
      el("div", { class: "k", text: "Status" }),    el("div", { class: "v", text: task.status }),
      ...(meta ? [
        el("div", { class: "k", text: "Priority" }),  el("div", { class: "v", text: meta.priority }),
        el("div", { class: "k", text: "Created" }),   el("div", { class: "v", text: `${meta.createdAt} by ${meta.createdBy}` }),
        el("div", { class: "k", text: "Updated" }),   el("div", { class: "v", text: meta.updatedAt }),
        el("div", { class: "k", text: "Branch" }),    el("div", { class: "v", text: meta.branch || "—" }),
        el("div", { class: "k", text: "Workdir" }),   el("div", { class: "v", text: meta.workdir }),
        el("div", { class: "k", text: "Runs" }),      el("div", { class: "v", text: `${meta.runCount} (est ${meta.estimateH}h)` }),
        el("div", { class: "k", text: "Deps" }),      el("div", { class: "v", text: meta.deps.length ? meta.deps.join(", ") : "· none ·" }),
        el("div", { class: "k", text: "Depended by" }), el("div", { class: "v", text: meta.depBy.length ? meta.depBy.join(", ") : "· none ·" }),
        el("div", { class: "k", text: "Tags" }),      el("div", { class: "v" },
          ...(meta.tags || []).map(t => el("span", { class: "chip mini", text: t, style: { marginRight: "4px" } }))
        ),
      ] : []),
    ),
  );
  modalFootEl.append(
    el("button", { text: "Open logs in focus", onclick: () => { openInFocus("logs", { projectId, taskId }); closeModal(); } }),
    el("button", { class: "primary", text: "Close", onclick: closeModal }),
  );
}

// ---- Feature modal ----
function renderFeatureTrail(feature) {
  const meta = FEATURE_META[feature.id] || {};
  const lifecycle = featureLifecycle(feature);
  const steps = [
    ["tasks", "Tasks done", feature.progress >= 1 || lifecycle.key !== "active"],
    ["checkout", "Checkout", ["finished", "mr", "merged"].includes(lifecycle.key)],
    ["mr", meta.mr || "MR", ["mr", "merged"].includes(lifecycle.key)],
    ["merged", "Merged", lifecycle.key === "merged"],
  ];
  return el("div", { class: "handoff" },
    ...steps.map(([key, label, done], idx) => el("span", { class: `handoff-step ${done ? "done" : ""} ${key === lifecycle.key ? "current" : ""}` },
      el("span", { class: "idx", text: String(idx + 1) }),
      el("span", { text: label }),
    ))
  );
}

function renderFeatureModal() {
  const { projectId, featureId } = state.modal;
  const f = (MOCK_FEATURES[projectId] || []).find(x => x.id === featureId);
  const meta = FEATURE_META[featureId];
  modalTitleEl.textContent = f ? `${f.id} · ${f.name}` : featureId;
  if (!f) { modalBodyEl.textContent = "Feature not found"; return; }
  const assignedRunner = state.featureAssignments[f.id];
  const runner = MOCK_RUNNERS.find(r => r.id === assignedRunner);
  const lifecycle = featureLifecycle(f);
  const warn = runnerWarning(f);
  modalBodyEl.append(
    renderFeatureTrail(f),
    el("div", { class: "kv-grid" },
      el("div", { class: "k", text: "Project" }),        el("div", { class: "v", text: projectId }),
      el("div", { class: "k", text: "State" }),          el("div", { class: "v" }, el("span", { class: `life-badge ${lifecycle.tone}`, text: lifecycle.label })),
      el("div", { class: "k", text: "Progress" }),       el("div", { class: "v", text: `${Math.round((f.progress || 0) * 100)}%` }),
      el("div", { class: "k", text: "Assigned to" }),    el("div", { class: "v", text: runner ? `${runner.name}${warn ? ` (${warn})` : ""}` : "· unassigned ·" }),
      el("div", { class: "k", text: "Age" }),            el("div", { class: "v", text: featureAge(f) }),
      ...(meta ? [
        el("div", { class: "k", text: "Description" }),   el("div", { class: "v", text: meta.description }),
        el("div", { class: "k", text: "Owner" }),         el("div", { class: "v", text: meta.owner }),
        el("div", { class: "k", text: "Priority" }),      el("div", { class: "v", text: meta.priority }),
        el("div", { class: "k", text: "Target branch" }), el("div", { class: "v", text: meta.targetBranch }),
        el("div", { class: "k", text: "Checkout mode" }), el("div", { class: "v", text: meta.checkoutMode }),
        el("div", { class: "k", text: "Merge strategy" }),el("div", { class: "v", text: meta.mergeStrategy }),
        ...(meta.mr ? [el("div", { class: "k", text: "MR" }), el("div", { class: "v", text: meta.mr })] : []),
        ...(meta.finishedAt ? [el("div", { class: "k", text: "Finished" }), el("div", { class: "v", text: meta.finishedAt })] : []),
        ...(meta.mergedAt ? [el("div", { class: "k", text: "Merged" }), el("div", { class: "v", text: meta.mergedAt })] : []),
        el("div", { class: "k", text: "Created" }),       el("div", { class: "v", text: meta.createdAt }),
      ] : []),
    ),
  );
  modalFootEl.append(el("button", { class: "primary", text: "Close", onclick: closeModal }));
}

// ---- Automation modal ----
function renderAutomationModal() {
  const { projectId, automationId } = state.modal;
  const a = (MOCK_AUTOMATIONS[projectId] || []).find(x => x.id === automationId);
  modalTitleEl.textContent = a ? `${a.id} · ${a.name}` : automationId;
  if (!a) { modalBodyEl.textContent = "Automation not found"; return; }
  modalBodyEl.append(
    el("div", { class: "kv-grid" },
      el("div", { class: "k", text: "Project" }),    el("div", { class: "v", text: projectId }),
      el("div", { class: "k", text: "Trigger" }),    el("div", { class: "v", text: a.trigger }),
      el("div", { class: "k", text: "Status" }),     el("div", { class: "v", text: a.status }),
      el("div", { class: "k", text: "Last run" }),   el("div", { class: "v", text: a.lastRun }),
      el("div", { class: "k", text: "Next run" }),   el("div", { class: "v", text: a.nextRun || "event" }),
      el("div", { class: "k", text: "Last result" }),el("div", { class: "v", text: a.lastResult }),
      el("div", { class: "k", text: "Runs" }),       el("div", { class: "v", text: `${a.runCount || 0} total · ${a.failures || 0} failures` }),
      el("div", { class: "k", text: "Watches" }),    el("div", { class: "v" }, ...(a.watches || []).map(w => el("span", { class: "chip mini", text: w }))),
    ),
    el("div", { class: "automation-timeline" },
      el("div", { text: "recent runs" }),
      el("span", { class: "ok", text: `${a.lastRun} success: trigger matched, no follow-up needed` }),
      el("span", { class: a.lastResult === "failed" ? "err" : "ok", text: a.lastResult === "failed" ? "1d ago failed: permission denied reading MR comments" : "previous run success" }),
      el("span", { class: "dim", text: `next: ${a.nextRun || "event driven"}` }),
    ),
  );
  modalFootEl.append(
    el("button", { text: "Run now", onclick: () => toast(`Ran ${a.name} (mock)`) }),
    el("button", { class: "primary", text: "Close", onclick: closeModal }),
  );
}

// ---- Entry modal ----
function renderEntryModal() {
  const entry = MOCK_ENTRIES.find(e => e.id === state.modal.entryId) || MOCK_ENTRIES[0];
  modalTitleEl.textContent = entry ? `${entry.id} · ${entry.title}` : "Brain entry";
  if (!entry) { modalBodyEl.textContent = "Entry not found"; return; }
  modalBodyEl.append(
    el("div", { class: "entry-modal" },
      el("div", { class: "kv-grid" },
        el("div", { class: "k", text: "Project" }), el("div", { class: "v", text: entry.project }),
        el("div", { class: "k", text: "Type" }),    el("div", { class: "v", text: entry.type }),
        el("div", { class: "k", text: "Updated" }), el("div", { class: "v", text: entry.updated }),
        el("div", { class: "k", text: "Links" }),   el("div", { class: "v" }, ...entry.links.map(link => el("span", { class: "chip mini", text: link }))),
      ),
      el("textarea", { text: `---\ntype: ${entry.type}\nproject: ${entry.project}\nlinks: [${entry.links.join(", ")}]\n---\n\n# ${entry.title}\n\n${entry.excerpt}\n\n## Notes\n- Edit this entry while watching linked tasks/features.\n- Save writes back to Brain (mock).` }),
    )
  );
  modalFootEl.append(
    el("button", { text: "Open in focus", onclick: () => { openInFocus("entries", { projectId: entry.project }); closeModal(); } }),
    el("button", { text: "Save", onclick: () => toast(`Saved ${entry.id} (mock)`) }),
    el("button", { class: "primary", text: "Close", onclick: closeModal }),
  );
}

// ---- Settings modal ----
function settingText(key, label, placeholder = "") {
  return el("label", { class: "setting-field" },
    el("span", { text: label }),
    el("input", {
      type: "text",
      value: state.settings[key] || "",
      placeholder,
      onchange: (e) => { state.settings[key] = e.target.value; save(); toast(`Updated ${label}`); },
    }),
  );
}
function settingNumber(key, label) {
  return el("label", { class: "setting-field" },
    el("span", { text: label }),
    el("input", {
      type: "number",
      value: state.settings[key],
      min: "1",
      onchange: (e) => { state.settings[key] = Number(e.target.value || 1); save(); toast(`Updated ${label}`); },
    }),
  );
}
function settingSelect(key, label, options) {
  return el("label", { class: "setting-field" },
    el("span", { text: label }),
    el("select", {
      onchange: (e) => { state.settings[key] = e.target.value; save(); toast(`Updated ${label}`); },
    }, ...options.map(opt => el("option", { value: opt, selected: state.settings[key] === opt, text: opt }))),
  );
}
function settingCheckbox(key, label, onChange) {
  return el("label", {}, el("input", { type: "checkbox", checked: !!state.settings[key],
    onchange: (e) => { state.settings[key] = e.target.checked; onChange?.(e.target.checked); save(); render(); } }), label);
}
function renderSettingsModal() {
  modalTitleEl.textContent = "Settings";
  modalBodyEl.append(
    el("div", { class: "settings-grid" },
      el("div", { class: "mod-group" },
        el("h4", { text: "Brain server" }),
        settingText("apiUrl", "API URL", "http://localhost:3333"),
        settingText("apiToken", "API token", "brain_dev_..."),
        settingNumber("maxParallelTasks", "Max parallel tasks"),
        settingCheckbox("autoDispatchNewTasks", "Auto-dispatch new ready tasks"),
        settingCheckbox("streamLogs", "Stream server logs"),
        el("div", { class: "settings-actions" },
          el("button", { text: "Test connection", onclick: () => toast(`Connected to ${state.settings.apiUrl} (mock)`) }),
          el("button", { text: "Sync projects", onclick: () => toast("Synced projects from Brain (mock)") }),
        ),
      ),
      el("div", { class: "mod-group" },
        el("h4", { text: "Execution defaults" }),
        settingSelect("defaultExecutor", "Executor", ["opencode", "pi"]),
        settingText("defaultAgent", "Agent"),
        settingText("defaultModel", "Model"),
        settingSelect("defaultThinking", "Thinking", ["off", "minimal", "low", "medium", "high", "xhigh"]),
        settingSelect("defaultCheckoutMode", "Checkout mode", ["ai", "simple"]),
        settingSelect("defaultMergeStrategy", "Merge strategy", ["squash", "merge", "rebase"]),
        settingText("defaultMergeTargetBranch", "Merge target"),
        settingSelect("defaultExecutionMode", "Execution mode", ["worktree", "in_place"]),
      ),
      el("div", { class: "mod-group" },
        el("h4", { text: "UI layout" }),
        el("label", {}, el("input", { type: "checkbox", checked: state.mobile,
          onchange: (e) => { state.mobile = e.target.checked; save(); render(); } }), "Mobile mode"),
        el("label", {}, el("input", { type: "checkbox", checked: state.streaming,
          onchange: (e) => { state.streaming = e.target.checked; save(); } }), "Live stream logs"),
        el("label", {}, el("input", { type: "checkbox", checked: state.sidebarCollapsed,
          onchange: (e) => { state.sidebarCollapsed = e.target.checked; save(); render(); } }), "Collapse sidebar completely"),
        settingSelect("theme", "Theme", ["system", "dark", "light"]),
        settingSelect("density", "Density", ["compact", "normal", "spacious"]),
        settingCheckbox("telemetry", "Show telemetry/status metrics"),
      ),
      el("div", { class: "mod-group" },
        el("h4", { text: "Sidebar sections" }),
        ...["projects","sessions","runners"].map(k =>
          el("label", {}, el("input", { type: "checkbox", checked: state.sidebarSection[k],
            onchange: (e) => { state.sidebarSection[k] = e.target.checked; save(); render(); } }), k),
        ),
        el("div", { class: "settings-actions" },
          el("button", { text: "Clear filters", onclick: () => { state.filters = { status: "all", lifecycle: "all", env: "all" }; save(); render(); toast("Filters cleared"); } }),
          el("button", { text: "Expand sidebar", onclick: () => { state.sidebarCollapsed = false; save(); render(); } }),
        ),
      ),
      el("div", { class: "mod-group wide" },
        el("h4", { text: "About" }),
        el("div", { style: { fontSize: "11px", color: "#9098a1" }, text: "Brain PWA · Multi-project wireframe v2 · Settings control Brain server, execution defaults, and UI workspace behavior." }),
      ),
    ),
  );
  modalFootEl.append(
    el("button", { text: "Reset workspace", onclick: () => { if (confirm("Reset?")) { resetState(); closeModal(); } } }),
    el("button", { text: "Save settings", onclick: () => { save(); toast("Settings saved"); } }),
    el("button", { class: "primary", text: "Close", onclick: closeModal }),
  );
}

// ---------- banner buttons ----------
$("#reset-btn").addEventListener("click", () => { if (confirm("Reset wireframe?")) resetState(); });
$("#toggle-mobile-btn").addEventListener("click", () => { state.mobile = !state.mobile; save(); render(); });
$("#toggle-stream-btn").addEventListener("click", () => {
  state.streaming = !state.streaming;
  $("#toggle-stream-btn").textContent = state.streaming ? "Pause stream" : "Resume stream";
  save();
});

// ---------- keyboard ----------
document.addEventListener("click", (e) => {
  const plan = e.target.closest?.('.wc-row button[data-action="plan"]');
  if (plan) {
    const row = plan.closest('.wc-row');
    if (row?.dataset.projectId && row?.dataset.featureId) openFeatureDrawer(row.dataset.projectId, row.dataset.featureId);
  }
  const palette = e.target.closest?.('button[data-action="command-palette"]');
  if (palette) {
    state.commandPalette = true;
    save();
    render();
  }
});

window.addEventListener("keydown", (e) => {
  if (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA") return;
  if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); state.commandPalette = true; save(); render(); return; }
  if (e.key === "Escape") { hideContextMenu(); hideSheet(); if (state.drawer || state.commandPalette || state.assistantOpen) { state.drawer = null; state.commandPalette = false; state.assistantOpen = false; save(); render(); } }
  if (e.key === "1" && e.altKey) { state.view = "overview"; save(); render(); }
  if (e.key === "2" && e.altKey) { state.view = "focus"; save(); render(); }
});

// auto mobile if narrow
if (window.matchMedia("(max-width: 720px)").matches && !localStorage.getItem(LS_KEY)) {
  state.mobile = true;
}
window.matchMedia?.("(prefers-color-scheme: light)").addEventListener?.("change", () => {
  if ((state.settings?.theme || "system") === "system") render();
});

render();
startTicker();
