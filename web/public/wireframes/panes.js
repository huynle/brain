// Brain PWA — Dockable Panes Wireframe
// Self-contained, no deps. Plain ES module.

// ---------- helpers ----------
const $ = (sel, root = document) => root.querySelector(sel);
const el = (tag, props = {}, ...children) => {
  const node = document.createElement(tag);
  for (const [k, v] of Object.entries(props)) {
    if (k === "class") node.className = v;
    else if (k === "text") node.textContent = v;
    else if (k === "html") node.innerHTML = v;
    else if (k === "style") Object.assign(node.style, v);
    else if (k.startsWith("on")) node.addEventListener(k.slice(2).toLowerCase(), v);
    else if (k === "dataset") for (const [dk, dv] of Object.entries(v)) node.dataset[dk] = dv;
    else if (v !== undefined && v !== null && v !== false) node.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c == null || c === false) continue;
    node.appendChild(typeof c === "string" ? document.createTextNode(c) : c);
  }
  return node;
};
const uid = (p = "id") => `${p}_${Math.random().toString(36).slice(2, 9)}`;
const clamp = (n, a, b) => Math.max(a, Math.min(b, n));

// ---------- mock data ----------
const MOCK_PROJECTS = [
  { id: "orion-ai", name: "orion-ai" },
  { id: "brain", name: "brain" },
  { id: "dispatch-runner", name: "dispatch-runner" },
  { id: "pathfinding", name: "pathfinding" },
];
const MOCK_TASKS = {
  "orion-ai": [
    { id: "T-1001", title: "Wire OAuth callback",             status: "Ready" },
    { id: "T-1002", title: "Add rate limiter to /entries",    status: "Active" },
    { id: "T-1003", title: "Migrate SSE to per-project",      status: "Blocked" },
    { id: "T-1004", title: "Docs: entitlement examples",      status: "Completed" },
    { id: "T-1005", title: "Refactor scope keying",           status: "Ready" },
  ],
  "brain": [
    { id: "T-2001", title: "Dockable pane wireframe",         status: "Active" },
    { id: "T-2002", title: "Extract Splitter component",      status: "Ready" },
    { id: "T-2003", title: "PaneRegistry lib",                status: "Ready" },
    { id: "T-2004", title: "Migrate localStorage schema",     status: "Blocked" },
  ],
  "dispatch-runner": [
    { id: "T-3001", title: "Push dispatch capacity race",     status: "Active" },
    { id: "T-3002", title: "Placement reasons cap",           status: "Completed" },
    { id: "T-3003", title: "Worker pool scaling",             status: "Ready" },
  ],
  "pathfinding": [
    { id: "T-4001", title: "A* heuristic tuning",             status: "Ready" },
    { id: "T-4002", title: "Grid partitioning benchmark",     status: "Ready" },
  ],
};
const mockLogs = (taskId) => Array.from({ length: 24 }).map((_, i) => {
  const levels = ["INFO", "INFO", "INFO", "OK", "WARN", "INFO", "ERROR"];
  const lvl = levels[(i + taskId.length) % levels.length];
  const msgs = ["spawn opencode", "session started", "step completed", "commit pushed",
    "SSE reconnect", "runner idle", "task claimed", "tests passing", "worktree ready"];
  return { ts: `12:${String(4 + i).padStart(2, "0")}:12`, lvl, msg: `[${taskId}] ${msgs[i % msgs.length]}` };
});

// ---------- pane registry ----------
const PANE_KINDS = {
  "task-list": {
    label: "Tasks",
    title: (leaf) => `Tasks: ${leaf.target.projectId}`,
    render: (leaf, ctx) => renderTaskList(leaf, ctx),
    canReuse: true,
  },
  "task-detail": {
    label: "Detail",
    title: (leaf) => leaf.target.taskId ? `Task: ${leaf.target.taskId}` : "Detail",
    render: (leaf, ctx) => renderTaskDetail(leaf, ctx),
    canReuse: true,
  },
  "task-logs": {
    label: "Logs",
    title: (leaf) => leaf.target.taskId ? `Logs: ${leaf.target.taskId}` : "Logs",
    render: (leaf) => renderTaskLogs(leaf),
    canReuse: true,
  },
  "brain": {
    label: "Brain",
    title: () => "Brain",
    render: () => renderBrain(),
    canReuse: true,
  },
  "runners": {
    label: "Runners",
    title: () => "Runners",
    render: () => renderRunners(),
    canReuse: true,
  },
  "automations": {
    label: "Automations",
    title: () => "Automations",
    render: () => renderAutomations(),
    canReuse: true,
  },
  "logs": {
    label: "Global Logs",
    title: () => "Global Logs",
    render: () => renderGlobalLogs(),
    canReuse: true,
  },
  "terminal": {
    label: "Terminal",
    title: (leaf) => `Terminal ${leaf.id.slice(-4)}`,
    render: (leaf) => renderTerminal(leaf),
    canReuse: false,
  },
  "browser": {
    label: "Browser",
    title: (leaf) => leaf.target.url ? `Web: ${leaf.target.url.replace(/^https?:\/\//,"")}` : "Browser",
    render: (leaf) => renderBrowser(leaf),
    canReuse: false,
  },
};

// ---------- default layout preset ----------
function defaultLayout(projectId) {
  const taskListLeaf   = { id: uid("l"), kind: "task-list",   target: { projectId } };
  const detailLeaf     = { id: uid("l"), kind: "task-detail", target: { projectId, taskId: MOCK_TASKS[projectId]?.[0]?.id ?? null } };
  const logsLeaf       = { id: uid("l"), kind: "task-logs",   target: { projectId, taskId: MOCK_TASKS[projectId]?.[0]?.id ?? null } };
  return {
    type: "split", axis: "col", ratio: 0.55,
    children: [
      { type: "leaf", leaf: taskListLeaf },
      {
        type: "split", axis: "row", ratio: 0.55,
        children: [
          { type: "tabs", children: [detailLeaf], activeId: detailLeaf.id },
          { type: "tabs", children: [logsLeaf],   activeId: logsLeaf.id },
        ],
      },
    ],
  };
}

// ---------- state (persisted) ----------
const LS_KEY = "brain.wireframe.workspace.v1";
const defaultState = () => ({
  openProjects: ["orion-ai", "brain"],
  activeProjectId: "orion-ai",
  projects: {
    "orion-ai": { projectId: "orion-ai", layout: defaultLayout("orion-ai"), activeLeafId: null },
    "brain":    { projectId: "brain",    layout: defaultLayout("brain"),    activeLeafId: null },
  },
  view: "tasks",
  mobile: false,
  mobileActiveLeaf: {},   // per project: current visible leaf id
});
let state = load() || defaultState();

function load() {
  try {
    const raw = localStorage.getItem(LS_KEY);
    if (!raw) return null;
    return JSON.parse(raw);
  } catch { return null; }
}
function save() {
  try { localStorage.setItem(LS_KEY, JSON.stringify(state)); } catch {}
}
function resetState() {
  localStorage.removeItem(LS_KEY);
  state = defaultState();
  render();
}

// ---------- state mutators ----------
function activeProject() {
  return state.projects[state.activeProjectId];
}

/** Walk tree, mutate in-place; runs `visit(node, parent, key)` */
function walk(node, visit, parent = null, key = null) {
  if (!node) return;
  const stop = visit(node, parent, key);
  if (stop === "stop") return;
  if (node.type === "split") {
    walk(node.children[0], visit, node, 0);
    walk(node.children[1], visit, node, 1);
  }
}

function findLeaf(root, leafId) {
  let found = null;
  walk(root, (n, parent, key) => {
    if (n.type === "leaf" && n.leaf.id === leafId) { found = { node: n, parent, key, container: parent, holder: "leaf" }; return "stop"; }
    if (n.type === "tabs") {
      const idx = n.children.findIndex(c => c.id === leafId);
      if (idx >= 0) { found = { node: n, parent, key, container: n, index: idx, holder: "tabs" }; return "stop"; }
    }
  });
  return found;
}

function findReusableLeaf(root, kind) {
  let found = null;
  walk(root, (n) => {
    if (n.type === "leaf" && n.leaf.kind === kind && !n.leaf.pinned) { found = n.leaf; return "stop"; }
    if (n.type === "tabs") {
      const c = n.children.find(l => l.kind === kind && !l.pinned);
      if (c) { found = c; return "stop"; }
    }
  });
  return found;
}

function firstLeaf(node) {
  let f = null;
  walk(node, (n) => {
    if (n.type === "leaf") { f = n.leaf; return "stop"; }
    if (n.type === "tabs") { f = n.children[0]; return "stop"; }
  });
  return f;
}

function openPane(projectId, kind, target, mode = "reuse") {
  const proj = state.projects[projectId];
  if (!proj) return;
  if (!proj.layout) {
    proj.layout = { type: "leaf", leaf: { id: uid("l"), kind, target } };
    proj.activeLeafId = proj.layout.leaf.id;
    save(); render(); return;
  }
  if (mode === "reuse" && PANE_KINDS[kind]?.canReuse !== false) {
    const existing = findReusableLeaf(proj.layout, kind);
    if (existing) {
      existing.target = { ...existing.target, ...target };
      proj.activeLeafId = existing.id;
      // If existing leaf sits in a tabs node, activate the tab
      const info = findLeaf(proj.layout, existing.id);
      if (info?.holder === "tabs") info.container.activeId = existing.id;
      save(); render();
      toast(`Reused ${PANE_KINDS[kind].label}`);
      return;
    }
  }
  // Create new leaf.
  const newLeaf = { id: uid("l"), kind, target, pinned: mode === "new" };
  // "Standalone" pane kinds (terminal, browser, runners, brain, automations, logs) get
  // their own new split so they don't pile into the Detail area. Task/entry kinds dock
  // into the first tabs node that already holds the same kind, else create tabs there.
  const standaloneKinds = new Set(["terminal", "browser", "runners", "brain", "automations", "logs"]);
  let placed = false;
  if (!standaloneKinds.has(kind)) {
    // Try to dock into a tabs node that already contains this kind
    walk(proj.layout, (n) => {
      if (placed) return "stop";
      if (n.type === "tabs" && n.children.some(l => l.kind === kind)) {
        n.children.push(newLeaf); n.activeId = newLeaf.id; placed = true; return "stop";
      }
    });
    // Fall back: any tabs node
    if (!placed) {
      walk(proj.layout, (n) => {
        if (placed) return "stop";
        if (n.type === "tabs") { n.children.push(newLeaf); n.activeId = newLeaf.id; placed = true; return "stop"; }
      });
    }
  }
  if (!placed) {
    // Add a new split on the right of the current layout root
    if (proj.layout.type === "leaf") {
      proj.layout = { type: "split", axis: "row", ratio: 0.6, children: [proj.layout, { type: "leaf", leaf: newLeaf }] };
    } else {
      proj.layout = { type: "split", axis: "row", ratio: 0.7, children: [proj.layout, { type: "leaf", leaf: newLeaf }] };
    }
  }
  proj.activeLeafId = newLeaf.id;
  save(); render();
  toast(mode === "new" ? "Opened in new pane (pinned)" : "Opened pane");
}

function closeLeaf(projectId, leafId) {
  const proj = state.projects[projectId];
  if (!proj?.layout) return;
  const info = findLeaf(proj.layout, leafId);
  if (!info) return;
  if (info.holder === "tabs") {
    info.container.children.splice(info.index, 1);
    if (info.container.children.length === 1) {
      // collapse tabs to leaf
      const remaining = info.container.children[0];
      replaceNode(proj, info.container, { type: "leaf", leaf: remaining });
    } else if (info.container.children.length === 0) {
      removeNode(proj, info.container);
    } else if (info.container.activeId === leafId) {
      info.container.activeId = info.container.children[0].id;
    }
  } else {
    removeNode(proj, info.node);
  }
  proj.activeLeafId = firstLeaf(proj.layout)?.id ?? null;
  save(); render();
}

function replaceNode(proj, oldNode, newNode) {
  if (proj.layout === oldNode) { proj.layout = newNode; return; }
  walk(proj.layout, (n) => {
    if (n.type === "split") {
      if (n.children[0] === oldNode) { n.children[0] = newNode; return "stop"; }
      if (n.children[1] === oldNode) { n.children[1] = newNode; return "stop"; }
    }
  });
}

function removeNode(proj, node) {
  if (proj.layout === node) { proj.layout = null; return; }
  walk(proj.layout, (n) => {
    if (n.type === "split") {
      if (n.children[0] === node) { replaceNode(proj, n, n.children[1]); return "stop"; }
      if (n.children[1] === node) { replaceNode(proj, n, n.children[0]); return "stop"; }
    }
  });
}

/** Drop `leaf` at drop target within `projectId`. */
function dropLeaf(sourceProjectId, sourceLeafId, targetProjectId, targetLeafId, edge) {
  // Remove from source
  const srcProj = state.projects[sourceProjectId];
  const info = findLeaf(srcProj.layout, sourceLeafId);
  if (!info) return;
  let leafObj;
  if (info.holder === "tabs") {
    leafObj = info.container.children.splice(info.index, 1)[0];
    if (info.container.children.length === 1) {
      const remaining = info.container.children[0];
      replaceNode(srcProj, info.container, { type: "leaf", leaf: remaining });
    } else if (info.container.activeId === sourceLeafId) {
      info.container.activeId = info.container.children[0].id;
    }
  } else {
    leafObj = info.node.leaf;
    removeNode(srcProj, info.node);
  }
  // Ensure open project
  const dstProj = state.projects[targetProjectId];
  if (!dstProj) { srcProj.layout = srcProj.layout; save(); render(); return; }

  if (!dstProj.layout) {
    dstProj.layout = { type: "leaf", leaf: leafObj };
  } else if (targetLeafId == null) {
    // Drop onto project tab or empty area = append as tab to first tabs, else wrap
    let placed = false;
    walk(dstProj.layout, (n) => {
      if (placed) return "stop";
      if (n.type === "tabs") { n.children.push(leafObj); n.activeId = leafObj.id; placed = true; return "stop"; }
    });
    if (!placed) {
      if (dstProj.layout.type === "leaf") {
        dstProj.layout = { type: "tabs", children: [dstProj.layout.leaf, leafObj], activeId: leafObj.id };
      } else {
        dstProj.layout = { type: "split", axis: "row", ratio: 0.7, children: [dstProj.layout, { type: "leaf", leaf: leafObj }] };
      }
    }
  } else {
    // Drop relative to a specific leaf
    const dstInfo = findLeaf(dstProj.layout, targetLeafId);
    if (!dstInfo) return;
    if (edge === "center") {
      // Convert target into tabs and add
      if (dstInfo.holder === "tabs") {
        dstInfo.container.children.push(leafObj);
        dstInfo.container.activeId = leafObj.id;
      } else {
        const oldLeaf = dstInfo.node.leaf;
        const tabs = { type: "tabs", children: [oldLeaf, leafObj], activeId: leafObj.id };
        replaceNode(dstProj, dstInfo.node, tabs);
      }
    } else {
      // Split
      const axis = (edge === "left" || edge === "right") ? "row" : "col";
      const first = (edge === "left" || edge === "top");
      const targetContainer = dstInfo.holder === "tabs" ? dstInfo.container : dstInfo.node;
      const newLeafNode = { type: "leaf", leaf: leafObj };
      const split = {
        type: "split", axis, ratio: 0.5,
        children: first ? [newLeafNode, targetContainer] : [targetContainer, newLeafNode],
      };
      replaceNode(dstProj, targetContainer, split);
    }
  }
  dstProj.activeLeafId = leafObj.id;
  if (state.activeProjectId !== targetProjectId) state.activeProjectId = targetProjectId;
  save(); render();
  toast("Moved pane");
}

function setSplitRatio(projectId, nodeRef, ratio) {
  nodeRef.ratio = clamp(ratio, 0.1, 0.9);
  save();
}

function setActiveProject(id) {
  state.activeProjectId = id;
  save(); render();
}
function openProjectTab(id) {
  if (!state.openProjects.includes(id)) {
    state.openProjects.push(id);
    if (!state.projects[id]) state.projects[id] = { projectId: id, layout: defaultLayout(id), activeLeafId: null };
  }
  state.activeProjectId = id;
  save(); render();
}
function closeProjectTab(id) {
  state.openProjects = state.openProjects.filter(p => p !== id);
  delete state.projects[id];
  if (state.activeProjectId === id) state.activeProjectId = state.openProjects[0] ?? null;
  save(); render();
}

// ---------- content renderers ----------
function renderTaskList(leaf, ctx) {
  const tasks = MOCK_TASKS[leaf.target.projectId] ?? [];
  const proj = state.projects[leaf.target.projectId];
  const activeTaskId = findReusableLeaf(proj.layout, "task-detail")?.target?.taskId;
  const root = el("div", { class: "task-list" });
  for (const t of tasks) {
    const row = el("div", {
      class: `task-row ${t.id === activeTaskId ? "selected" : ""}`,
      onclick: () => openPane(leaf.target.projectId, "task-detail", { projectId: leaf.target.projectId, taskId: t.id }, "reuse"),
      oncontextmenu: (e) => {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, [
          { label: "Open detail (reuse)",       onClick: () => openPane(leaf.target.projectId, "task-detail", { projectId: leaf.target.projectId, taskId: t.id }, "reuse") },
          { label: "Open detail in new pane",   onClick: () => openPane(leaf.target.projectId, "task-detail", { projectId: leaf.target.projectId, taskId: t.id }, "new") },
          { label: "Open logs (reuse)",         onClick: () => openPane(leaf.target.projectId, "task-logs",   { projectId: leaf.target.projectId, taskId: t.id }, "reuse") },
          { label: "Open logs in new pane",     onClick: () => openPane(leaf.target.projectId, "task-logs",   { projectId: leaf.target.projectId, taskId: t.id }, "new") },
          { sep: true },
          { label: "Open in new project pane",  onClick: () => {
              const newProj = prompt("Project id?", "brain");
              if (!newProj) return;
              openProjectTab(newProj);
              openPane(newProj, "task-detail", { projectId: newProj, taskId: t.id }, "new");
          } },
        ]);
      },
    },
      el("div", { class: "title", text: t.title }),
      el("div", { class: `status ${t.status}`, text: t.status }),
      el("div", { class: "id", text: t.id }),
    );
    root.appendChild(row);
  }
  return root;
}

function renderTaskDetail(leaf) {
  const { taskId, projectId } = leaf.target;
  if (!taskId) return el("div", { class: "detail-box", text: "Select a task…" });
  const task = (MOCK_TASKS[projectId] ?? []).find(t => t.id === taskId);
  if (!task) return el("div", { class: "detail-box", text: `Task ${taskId} not found` });
  return el("div", { class: "detail-box" },
    el("h3", { text: task.title }),
    el("div", { class: "row" }, el("b", { text: "ID:" }), " ", task.id),
    el("div", { class: "row" }, el("b", { text: "Status:" }), " ", task.status),
    el("div", { class: "row" }, el("b", { text: "Project:" }), " ", projectId),
    el("div", { class: "row" }, el("b", { text: "Deps:" }), " none"),
    el("div", { class: "row" }, el("b", { text: "Priority:" }), " medium"),
    el("div", { class: "row" }, el("b", { text: "Created:" }), " 2h ago"),
    el("div", { class: "actions" },
      el("button", { text: "▶ Run" }),
      el("button", { text: "✓ Complete" }),
      el("button", { text: "✎ Edit" }),
      el("button", { text: "⊘ Cancel" }),
    ),
    el("div", { style: { marginTop: "10px", color: "#6b757e", fontSize: "11px" } },
      "Body: Mock description for wireframe. This pane is the reusable Task Detail. ",
      "Right-click a task to “Open in new pane” to see multiple pinned details coexist."
    ),
  );
}

function renderTaskLogs(leaf) {
  const { taskId } = leaf.target;
  if (!taskId) return el("div", { text: "No task selected." });
  const root = el("div", { class: "log-list" });
  for (const line of mockLogs(taskId)) {
    root.appendChild(el("div", { class: `log-line ${line.lvl === "ERROR" ? "err" : line.lvl === "OK" ? "ok" : line.lvl === "WARN" ? "wrn" : ""}` },
      el("span", { class: "ts", text: line.ts }),
      el("span", { class: "lvl", text: line.lvl }),
      el("span", { class: "msg", text: line.msg }),
    ));
  }
  return root;
}

function renderBrain() {
  return el("div", { class: "brain-mock" },
    el("div", { class: "card" }, el("h4", { text: "plan/2026-07-14-dockable-panes.md" }), "Latest planning entry"),
    el("div", { class: "card" }, el("h4", { text: "learning/scope-per-project.md" }), "Keying scope by project"),
    el("div", { class: "card" }, el("h4", { text: "pattern/pane-registry.md" }), "Registering pane kinds"),
  );
}
function renderRunners() {
  return el("div", { class: "runners-mock" },
    el("div", { class: "card" }, el("h4", { text: "runner-macbook-01" }), "online · 2 tasks"),
    el("div", { class: "card" }, el("h4", { text: "runner-ci-eu" }),      "online · idle"),
    el("div", { class: "card" }, el("h4", { text: "runner-orion-gpu" }),  "stale · last seen 2d"),
  );
}
function renderAutomations() {
  return el("div", { class: "automations-mock" },
    el("div", { class: "card" }, el("h4", { text: "feature-checkout (builtin)" }), "trigger: feature.completed"),
    el("div", { class: "card" }, el("h4", { text: "blocked-inspector" }),          "trigger: cron */30 min"),
    el("div", { class: "card" }, el("h4", { text: "dream-consolidation" }),        "trigger: cron daily 3am"),
  );
}
function renderGlobalLogs() {
  return el("div", { class: "log-list" },
    ...Array.from({ length: 30 }).map((_, i) =>
      el("div", { class: "log-line" },
        el("span", { class: "ts", text: `12:${String(i).padStart(2, "0")}:00` }),
        el("span", { class: "lvl", text: "HTTP" }),
        el("span", { class: "msg", text: `GET /api/v1/tasks 200 (${20 + i}ms)` }),
      )
    ),
  );
}
function renderTerminal(leaf) {
  const root = el("div", { class: "terminal-mock" });
  root.appendChild(el("div", { html: `<span class='prompt'>$</span> just build<br>` +
    `→ go build ./cmd/brain-api<br>` +
    `→ go build ./cmd/brain<br>` +
    `<span style='color:#6fca7d'>ok  ✓ 2 binaries built (3.2s)</span><br>` +
    `<span class='prompt'>$</span> <span style='color:#f4b23a'>█</span>` }));
  return root;
}
function renderBrowser(leaf) {
  const url = leaf.target.url || "https://brain.local/docs";
  return el("div", { class: "browser-mock" },
    el("div", { class: "browser-bar" },
      el("button", { text: "←" }),
      el("button", { text: "→" }),
      el("button", { text: "⟳" }),
      el("input", { value: url, onchange: (e) => { leaf.target.url = e.target.value; save(); render(); } }),
    ),
    el("div", { class: "browser-body" },
      el("h2", { text: "In-app browser (mock)" }),
      el("p", { text: `Loaded: ${url}` }),
      el("p", { text: "Phase 2 registry entry. No layout changes required." }),
    ),
  );
}

// ---------- rendering ----------
const app = $("#app");

function render() {
  app.innerHTML = "";
  document.body.classList.toggle("mobile", !!state.mobile);

  // Status bar
  app.appendChild(el("div", { class: "statusbar" },
    el("span", { class: "logo", text: "brain" }),
    el("span", { text: "workspace" }),
    el("span", { class: "spacer" }),
    el("button", { class: "btn-plain", text: "⌘ command" }),
    el("button", { class: "btn-plain", text: "Assistant ▸" }),
  ));

  // Project tab bar
  const projTabs = el("div", { class: "project-tabs" });
  for (const pid of state.openProjects) {
    const proj = state.projects[pid];
    const paneCount = countLeaves(proj?.layout);
    const tab = el("div", {
      class: `project-tab ${pid === state.activeProjectId ? "active" : ""}`,
      draggable: false,
      onclick: () => setActiveProject(pid),
      oncontextmenu: (e) => {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, [
          { label: "Activate", onClick: () => setActiveProject(pid) },
          { label: "Close project", onClick: () => closeProjectTab(pid) },
          { sep: true },
          { label: "Reset this project's layout", onClick: () => { state.projects[pid] = { projectId: pid, layout: defaultLayout(pid), activeLeafId: null }; save(); render(); } },
        ]);
      },
      dataset: { projectId: pid, dropzone: "project" },
    },
      el("span", { class: "dot" }),
      el("span", { text: proj?.name ?? pid }),
      el("span", { style: { fontSize: "10px", color: "#6b757e" }, text: `(${paneCount})` }),
      el("span", {
        class: "close", text: "×",
        onclick: (e) => { e.stopPropagation(); closeProjectTab(pid); },
      }),
    );
    projTabs.appendChild(tab);
  }
  // Add project button + popover of unopened projects
  projTabs.appendChild(el("div", {
    class: "project-tab-add",
    text: "+ open project",
    dataset: { dropzone: "new-project" },
    onclick: (e) => {
      const unopened = MOCK_PROJECTS.filter(p => !state.openProjects.includes(p.id));
      if (!unopened.length) { toast("All projects open"); return; }
      showContextMenu(e.clientX, e.clientY,
        unopened.map(p => ({ label: p.name, onClick: () => openProjectTab(p.id) }))
      );
    },
  }));
  app.appendChild(projTabs);

  // Content tabs (per project)
  const contentTabs = el("div", { class: "content-tabs" },
    el("span", { class: "group-label", text: "PROJECT" }),
    ...["brain", "tasks", "automations"].map(v => el("div", {
      class: `ct-btn ${state.view === v ? "active" : ""}`,
      text: v,
      onclick: () => { state.view = v; save(); render(); },
    })),
    el("div", { class: "ct-sep" }),
    el("span", { class: "group-label", text: "GLOBAL" }),
    ...["runners", "logs"].map(v => el("div", {
      class: `ct-btn ${state.view === v ? "active" : ""}`,
      text: v,
      onclick: () => { state.view = v; save(); render(); },
    })),
    el("span", { class: "spacer", style: { flex: 1 } }),
    el("button", {
      title: "Add pane in current project",
      text: "+ pane",
      onclick: (e) => {
        const proj = state.activeProjectId;
        if (!proj) { toast("No project selected"); return; }
        showContextMenu(e.clientX, e.clientY, Object.entries(PANE_KINDS).map(([kind, def]) => ({
          label: `+ ${def.label}`,
          onClick: () => openPane(proj, kind, kind === "browser" ? { url: "https://opencode.ai/docs" } : { projectId: proj, taskId: MOCK_TASKS[proj]?.[0]?.id ?? null }, "new"),
        })));
      },
    }),
  );
  app.appendChild(contentTabs);

  // Workspace
  const ws = el("div", { class: "workspace" });
  const proj = activeProject();
  if (!proj) {
    ws.appendChild(el("div", { class: "empty-project" },
      el("div", { text: "No project open" }),
      el("button", { text: "+ Open a project", onclick: (e) => {
        showContextMenu(e.clientX, e.clientY, MOCK_PROJECTS.map(p => ({ label: p.name, onClick: () => openProjectTab(p.id) })));
      }}),
    ));
  } else if (!proj.layout) {
    ws.appendChild(el("div", { class: "empty-project" },
      el("div", { text: `No panes in ${proj.projectId}` }),
      el("button", { text: "+ Task list", onclick: () => openPane(proj.projectId, "task-list", { projectId: proj.projectId }, "new") }),
    ));
  } else {
    // Mobile switcher pills (if mobile & multiple leaves)
    if (state.mobile) {
      const leaves = allLeaves(proj.layout);
      const active = state.mobileActiveLeaf[proj.projectId] ?? leaves[0]?.id;
      if (!state.mobileActiveLeaf[proj.projectId] && active) state.mobileActiveLeaf[proj.projectId] = active;
      const sw = el("div", { class: "mobile-switcher" });
      for (const l of leaves) {
        sw.appendChild(el("div", {
          class: `pill ${l.id === active ? "active" : ""}`,
          text: PANE_KINDS[l.kind].title(l),
          onclick: () => { state.mobileActiveLeaf[proj.projectId] = l.id; save(); render(); },
        }));
      }
      ws.appendChild(sw);
    }
    ws.appendChild(renderNode(proj.layout, proj));
  }
  app.appendChild(ws);

  // Help / Mobile nav
  if (state.mobile) {
    app.appendChild(el("div", { class: "helpbar" },
      ...["tasks", "brain", "automations", "runners", "logs"].map(v =>
        el("div", { class: `mnav ${state.view === v ? "active" : ""}`, text: v, onclick: () => { state.view = v; save(); render(); } })
      ),
    ));
  } else {
    app.appendChild(el("div", { class: "helpbar" },
      el("span", null, el("kbd", { text: "⌘K" }), " command"),
      el("span", null, el("kbd", { text: "→" }), " next project"),
      el("span", null, el("kbd", { text: "Right-click" }), " context menu"),
      el("span", null, el("kbd", { text: "Long-press" }), " (mobile) move pane"),
      el("span", null, el("kbd", { text: "Drag pane header" }), " to dock"),
    ));
  }
}

function countLeaves(node) {
  if (!node) return 0;
  if (node.type === "leaf") return 1;
  if (node.type === "tabs") return node.children.length;
  return countLeaves(node.children[0]) + countLeaves(node.children[1]);
}
function allLeaves(node) {
  if (!node) return [];
  if (node.type === "leaf") return [node.leaf];
  if (node.type === "tabs") return node.children.slice();
  return [...allLeaves(node.children[0]), ...allLeaves(node.children[1])];
}

// Render a PaneNode
function renderNode(node, proj) {
  if (node.type === "split") {
    const outer = el("div", { class: `node split-${node.axis}`, style: { flex: 1, display: "flex", flexDirection: node.axis === "row" ? "row" : "column", minWidth: 0, minHeight: 0 } });
    const a = renderNode(node.children[0], proj);
    const b = renderNode(node.children[1], proj);
    a.style.flex = `${node.ratio}`;
    a.style.minWidth = 0; a.style.minHeight = 0;
    b.style.flex = `${1 - node.ratio}`;
    b.style.minWidth = 0; b.style.minHeight = 0;
    const splitter = el("div", { class: `splitter ${node.axis === "row" ? "col" : "row"}` });
    attachSplitter(splitter, node, proj, outer);
    outer.appendChild(a);
    outer.appendChild(splitter);
    outer.appendChild(b);
    return outer;
  }
  if (node.type === "tabs") {
    return renderTabsPane(node, proj);
  }
  if (node.type === "leaf") {
    return renderLeafPane(node.leaf, proj, null);
  }
}

function renderTabsPane(tabsNode, proj) {
  const pane = el("div", { class: "pane" });
  // Mobile visibility
  const activeMobile = state.mobileActiveLeaf[proj.projectId];
  const activeLeaf = tabsNode.children.find(c => c.id === tabsNode.activeId) ?? tabsNode.children[0];
  if (state.mobile) {
    if (!tabsNode.children.some(c => c.id === activeMobile)) {
      // hide entirely
    } else {
      pane.classList.add("mobile-visible");
      tabsNode.activeId = activeMobile;
    }
  } else {
    pane.classList.add("mobile-visible");
  }
  if (proj.activeLeafId === activeLeaf?.id) pane.classList.add("focused");
  const tabbar = el("div", { class: "pane-tabbar" });
  for (const l of tabsNode.children) {
    const tab = el("div", {
      class: `pane-tab ${l.id === tabsNode.activeId ? "active" : ""} ${l.pinned ? "pinned" : ""}`,
      onclick: () => { tabsNode.activeId = l.id; proj.activeLeafId = l.id; save(); render(); },
      oncontextmenu: (e) => {
        e.preventDefault();
        showContextMenu(e.clientX, e.clientY, paneContextMenu(proj.projectId, l));
      },
    },
      el("span", { class: "kind", text: PANE_KINDS[l.kind]?.label ?? l.kind }),
      el("span", { class: "pane-tab-title", text: PANE_KINDS[l.kind]?.title(l) ?? l.kind }),
      el("span", { class: "close", text: "×", onclick: (e) => { e.stopPropagation(); closeLeaf(proj.projectId, l.id); } }),
    );
    attachPaneDrag(tab, proj.projectId, l.id);
    tabbar.appendChild(tab);
  }
  // + tab button
  tabbar.appendChild(el("div", {
    class: "pane-tab",
    style: { minWidth: "24px", justifyContent: "center", color: "#6b757e", cursor: "pointer" },
    text: "+",
    onclick: (e) => {
      showContextMenu(e.clientX, e.clientY, Object.entries(PANE_KINDS).map(([kind, def]) => ({
        label: `+ ${def.label}`,
        onClick: () => openPane(proj.projectId, kind, kind === "browser" ? { url: "https://opencode.ai/docs" } : { projectId: proj.projectId, taskId: MOCK_TASKS[proj.projectId]?.[0]?.id ?? null }, "new"),
      })));
    },
  }));
  pane.appendChild(tabbar);
  const body = el("div", { class: "pane-body" });
  if (activeLeaf) body.appendChild(PANE_KINDS[activeLeaf.kind].render(activeLeaf, { proj }));
  pane.appendChild(body);
  attachDropZones(pane, proj.projectId, activeLeaf?.id);
  return pane;
}

function renderLeafPane(leaf, proj) {
  const pane = el("div", { class: "pane" });
  if (proj.activeLeafId === leaf.id) pane.classList.add("focused");
  // In mobile, only render if this is the active leaf
  if (state.mobile) {
    const activeMobile = state.mobileActiveLeaf[proj.projectId];
    if (leaf.id === activeMobile) pane.classList.add("mobile-visible");
  } else {
    pane.classList.add("mobile-visible");
  }
  const tabbar = el("div", { class: "pane-tabbar" });
  const tab = el("div", {
    class: `pane-tab active ${leaf.pinned ? "pinned" : ""}`,
    onclick: () => { proj.activeLeafId = leaf.id; save(); render(); },
    oncontextmenu: (e) => {
      e.preventDefault();
      showContextMenu(e.clientX, e.clientY, paneContextMenu(proj.projectId, leaf));
    },
  },
    el("span", { class: "kind", text: PANE_KINDS[leaf.kind]?.label ?? leaf.kind }),
    el("span", { class: "pane-tab-title", text: PANE_KINDS[leaf.kind]?.title(leaf) ?? leaf.kind }),
    el("span", { class: "close", text: "×", onclick: (e) => { e.stopPropagation(); closeLeaf(proj.projectId, leaf.id); } }),
  );
  attachPaneDrag(tab, proj.projectId, leaf.id);
  tabbar.appendChild(tab);
  pane.appendChild(tabbar);
  const body = el("div", { class: "pane-body" });
  body.appendChild(PANE_KINDS[leaf.kind].render(leaf, { proj }));
  pane.appendChild(body);
  attachDropZones(pane, proj.projectId, leaf.id);
  return pane;
}

// ---------- context menu ----------
function paneContextMenu(projectId, leaf) {
  return [
    { label: "Focus", onClick: () => { state.projects[projectId].activeLeafId = leaf.id; save(); render(); } },
    { label: leaf.pinned ? "Unpin (allow reuse)" : "Pin pane (prevent reuse)", onClick: () => { leaf.pinned = !leaf.pinned; save(); render(); } },
    { sep: true },
    { label: "Split right",  onClick: () => splitLeafDirection(projectId, leaf.id, "right") },
    { label: "Split down",   onClick: () => splitLeafDirection(projectId, leaf.id, "bottom") },
    { sep: true },
    { label: "Move to project…", onClick: () => showMoveSheet(projectId, leaf.id) },
    { label: "Close pane",   onClick: () => closeLeaf(projectId, leaf.id) },
  ];
}
function splitLeafDirection(projectId, leafId, edge) {
  // Duplicate the leaf's kind but empty target
  const l = state.projects[projectId] && findLeafObj(state.projects[projectId].layout, leafId);
  if (!l) return;
  const newLeaf = { id: uid("l"), kind: l.kind, target: { ...l.target }, pinned: true };
  // Fake "drop" the new leaf onto the source pane at direction
  const proj = state.projects[projectId];
  const info = findLeaf(proj.layout, leafId);
  const axis = (edge === "left" || edge === "right") ? "row" : "col";
  const first = (edge === "left" || edge === "top");
  const targetContainer = info.holder === "tabs" ? info.container : info.node;
  const newLeafNode = { type: "leaf", leaf: newLeaf };
  const split = {
    type: "split", axis, ratio: 0.5,
    children: first ? [newLeafNode, targetContainer] : [targetContainer, newLeafNode],
  };
  replaceNode(proj, targetContainer, split);
  proj.activeLeafId = newLeaf.id;
  save(); render();
}
function findLeafObj(root, leafId) {
  let f = null;
  walk(root, (n) => {
    if (n.type === "leaf" && n.leaf.id === leafId) { f = n.leaf; return "stop"; }
    if (n.type === "tabs") { const l = n.children.find(c => c.id === leafId); if (l) { f = l; return "stop"; } }
  });
  return f;
}

const ctxmenu = $("#ctxmenu");
function showContextMenu(x, y, items) {
  ctxmenu.innerHTML = "";
  for (const it of items) {
    if (it.sep) { ctxmenu.appendChild(el("div", { class: "sep" })); continue; }
    if (it.lbl) { ctxmenu.appendChild(el("div", { class: "lbl", text: it.lbl })); continue; }
    ctxmenu.appendChild(el("button", { text: it.label, onclick: () => { hideContextMenu(); it.onClick?.(); } }));
  }
  ctxmenu.style.left = `${Math.min(x, window.innerWidth - 260)}px`;
  ctxmenu.style.top  = `${Math.min(y, window.innerHeight - 300)}px`;
  ctxmenu.hidden = false;
  setTimeout(() => document.addEventListener("mousedown", onDocMouseDown, { once: true }), 0);
}
function hideContextMenu() { ctxmenu.hidden = true; }
function onDocMouseDown(e) {
  if (!ctxmenu.contains(e.target)) hideContextMenu();
}

// ---------- mobile long-press → bottom sheet ----------
const sheet = $("#sheet"), sheetScrim = $("#sheet-scrim"), sheetBody = $("#sheet-body");
function showSheet(title, items) {
  sheetBody.innerHTML = "";
  sheetBody.appendChild(el("h4", { text: title }));
  for (const it of items) {
    if (it.sep) { sheetBody.appendChild(el("div", { style: { height: "1px", background: "#22272c", margin: "8px 0" } })); continue; }
    sheetBody.appendChild(el("div", { class: "row", onclick: () => { hideSheet(); it.onClick?.(); } },
      el("div", { class: "icon", text: it.icon ?? "→" }),
      el("div", { text: it.label }),
    ));
  }
  sheet.hidden = false; sheetScrim.hidden = false;
}
function hideSheet() { sheet.hidden = true; sheetScrim.hidden = true; }
sheetScrim.addEventListener("click", hideSheet);

function showMoveSheet(sourceProjectId, sourceLeafId) {
  const items = [];
  for (const pid of state.openProjects) {
    items.push({
      icon: pid === sourceProjectId ? "•" : "▪",
      label: `${pid}${pid === sourceProjectId ? "  (current)" : ""}`,
      onClick: () => dropLeaf(sourceProjectId, sourceLeafId, pid, null, null),
    });
  }
  items.push({ sep: true });
  items.push({ icon: "↓", label: "Split down (in current project)", onClick: () => splitLeafDirection(sourceProjectId, sourceLeafId, "bottom") });
  items.push({ icon: "→", label: "Split right (in current project)", onClick: () => splitLeafDirection(sourceProjectId, sourceLeafId, "right") });
  items.push({ icon: "⤴", label: "Move to a new project pane…", onClick: () => {
    const unopened = MOCK_PROJECTS.filter(p => !state.openProjects.includes(p.id));
    if (!unopened.length) { toast("No more projects to open"); return; }
    showSheet("Choose project", unopened.map(p => ({
      icon: "▫", label: p.name, onClick: () => { openProjectTab(p.id); dropLeaf(sourceProjectId, sourceLeafId, p.id, null, null); },
    })));
  } });
  items.push({ icon: "✕", label: "Close pane", onClick: () => closeLeaf(sourceProjectId, sourceLeafId) });
  showSheet("Move pane", items);
}

// ---------- drag: pane header ----------
let drag = null;
const ghost = $("#drag-ghost");

function attachPaneDrag(node, projectId, leafId) {
  let longPressTimer = null;
  let startPt = null;
  let suppressClick = false;

  // Suppress the click that fires on pointerup after a drag
  node.addEventListener("click", (e) => {
    if (suppressClick) {
      e.preventDefault();
      e.stopImmediatePropagation();
      suppressClick = false;
    }
  }, true);

  node.addEventListener("pointerdown", (e) => {
    if (e.button === 2) return; // right click
    // Don't start drag when clicking the close (×) button — let its own handler fire
    if (e.target?.classList?.contains("close")) return;
    // Prevent text selection which competes with pointermove
    e.preventDefault();
    startPt = { x: e.clientX, y: e.clientY };
    // Capture the pointer so we get all subsequent events even outside `node`
    try { node.setPointerCapture(e.pointerId); } catch {}

    if (state.mobile) {
      longPressTimer = setTimeout(() => {
        showMoveSheet(projectId, leafId);
        longPressTimer = null;
      }, 500);
    }
    const onMove = (ev) => {
      const dx = ev.clientX - startPt.x, dy = ev.clientY - startPt.y;
      if (!drag && (Math.abs(dx) > 6 || Math.abs(dy) > 6)) {
        if (longPressTimer) { clearTimeout(longPressTimer); longPressTimer = null; }
        if (state.mobile) return; // no drag on mobile — long-press only
        startDrag(projectId, leafId, node);
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

function startDrag(projectId, leafId, sourceNode) {
  drag = { sourceProjectId: projectId, sourceLeafId: leafId, target: null };
  const leafObj = findLeafObj(state.projects[projectId].layout, leafId);
  ghost.textContent = `↳ ${PANE_KINDS[leafObj.kind].title(leafObj)}`;
  ghost.hidden = false;
  sourceNode.classList.add("dragging");
  // Enable dropzones
  document.querySelectorAll(".dropzones").forEach(dz => dz.classList.add("active"));
}
function endDrag(x, y) {
  ghost.hidden = true;
  document.querySelectorAll(".dragging").forEach(n => n.classList.remove("dragging"));
  document.querySelectorAll(".dz.hot").forEach(n => n.classList.remove("hot"));
  document.querySelectorAll(".dropzones").forEach(dz => dz.classList.remove("active"));
  if (drag?.target) {
    const t = drag.target;
    // Guard: dropping onto self as center-tab or edge-split of self is a no-op
    const sameLeaf = t.kind === "leaf" && t.projectId === drag.sourceProjectId && t.leafId === drag.sourceLeafId;
    if (sameLeaf) {
      toast("Drop cancelled (same pane)");
    } else if (t.kind === "project") {
      // Dropping onto the pane's own project tab is also a no-op
      if (t.projectId === drag.sourceProjectId) {
        toast("Already in this project");
      } else {
        dropLeaf(drag.sourceProjectId, drag.sourceLeafId, t.projectId, null, null);
      }
    } else if (t.kind === "new-project") {
      const unopened = MOCK_PROJECTS.filter(p => !state.openProjects.includes(p.id));
      if (unopened.length) {
        const pid = unopened[0].id;
        openProjectTab(pid);
        dropLeaf(drag.sourceProjectId, drag.sourceLeafId, pid, null, null);
      } else { toast("No unopened projects"); }
    } else if (t.kind === "leaf") {
      dropLeaf(drag.sourceProjectId, drag.sourceLeafId, t.projectId, t.leafId, t.edge);
    }
  }
  drag = null;
}
function updateDropTarget(x, y) {
  document.querySelectorAll(".dz.hot").forEach(n => n.classList.remove("hot"));
  document.querySelectorAll(".project-tab.drop-target").forEach(n => n.classList.remove("drop-target"));
  drag.target = null;
  // Check drop zones under cursor
  const stack = document.elementsFromPoint(x, y);
  for (const el of stack) {
    if (el.classList?.contains("dz")) {
      el.classList.add("hot");
      const paneEl = el.closest(".pane");
      const leafId = paneEl?.dataset?.leafId;
      const projectId = paneEl?.dataset?.projectId;
      const edge = el.dataset?.edge;
      drag.target = { kind: "leaf", projectId, leafId, edge };
      return;
    }
    if (el.dataset?.dropzone === "project") {
      el.classList.add("drop-target");
      drag.target = { kind: "project", projectId: el.dataset.projectId };
      return;
    }
    if (el.dataset?.dropzone === "new-project") {
      el.classList.add("drop-target");
      drag.target = { kind: "new-project" };
      return;
    }
  }
}

// ---------- drop zones ----------
function attachDropZones(paneEl, projectId, leafId) {
  paneEl.dataset.leafId = leafId;
  paneEl.dataset.projectId = projectId;
  const dz = el("div", { class: "dropzones" });
  for (const edge of ["top", "right", "bottom", "left", "center"]) {
    dz.appendChild(el("div", { class: `dz ${edge}`, dataset: { edge } }));
  }
  paneEl.appendChild(dz);
}

// ---------- splitter drag ----------
function attachSplitter(splitEl, node, proj, containerEl) {
  splitEl.addEventListener("pointerdown", (e) => {
    e.preventDefault();
    splitEl.setPointerCapture(e.pointerId);
    splitEl.classList.add("active");
    const rect = containerEl.getBoundingClientRect();
    const onMove = (ev) => {
      const pos = node.axis === "row"
        ? (ev.clientX - rect.left) / rect.width
        : (ev.clientY - rect.top) / rect.height;
      setSplitRatio(proj.projectId, node, pos);
      render();
    };
    const onUp = () => {
      splitEl.classList.remove("active");
      document.removeEventListener("pointermove", onMove);
      document.removeEventListener("pointerup", onUp);
    };
    document.addEventListener("pointermove", onMove);
    document.addEventListener("pointerup", onUp);
  });
  splitEl.addEventListener("dblclick", () => { setSplitRatio(proj.projectId, node, 0.5); render(); });
}

// ---------- toast ----------
const toastEl = $("#toast");
let toastTimer = null;
function toast(msg) {
  toastEl.textContent = msg;
  toastEl.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toastEl.classList.remove("show"), 1400);
}

// ---------- wire banner ----------
$("#reset-btn").addEventListener("click", () => {
  if (confirm("Reset the wireframe workspace?")) resetState();
});
$("#toggle-mobile-btn").addEventListener("click", () => {
  state.mobile = !state.mobile;
  save(); render();
});

// keyboard: cycle projects
window.addEventListener("keydown", (e) => {
  if (e.metaKey || e.ctrlKey) return;
  if (e.target.tagName === "INPUT" || e.target.tagName === "TEXTAREA") return;
  if (e.key === "ArrowRight" && e.altKey) {
    const idx = state.openProjects.indexOf(state.activeProjectId);
    setActiveProject(state.openProjects[(idx + 1) % state.openProjects.length]);
  }
  if (e.key === "ArrowLeft" && e.altKey) {
    const idx = state.openProjects.indexOf(state.activeProjectId);
    setActiveProject(state.openProjects[(idx - 1 + state.openProjects.length) % state.openProjects.length]);
  }
  if (e.key === "Escape") { hideContextMenu(); hideSheet(); }
});

// auto-set mobile flag by viewport at load if not manually toggled
if (window.matchMedia("(max-width: 720px)").matches && !localStorage.getItem(LS_KEY)) {
  state.mobile = true;
}

render();
