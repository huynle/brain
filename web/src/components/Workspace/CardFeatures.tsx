/**
 * CardFeatures — wireframe-parity port.
 *
 * List of features for a project. Each rendered as a `.feat` with
 * head (name · life-badge · age · assign-chip · progress bar) plus
 * a description row and meta row (id · state · age · runner-warn ·
 * priority · checkout · target-branch).
 *
 * Features that declare `feature_depends_on` render as a tree: a
 * feature nests under the feature it waits on, so a multi-feature
 * build reads top-down in the order it can actually execute. With no
 * declared dependencies the tree is all roots and the list looks
 * exactly as it did before.
 *
 * Verbs come from `lib/actions/featureActions` via `useRowActions`, so
 * right-click, long-press and keyboard offer the same set.
 */
import { useMemo } from "react";
import { useSelection } from "../../store/selection";
import { useWorkspace } from "../../store/workspace";
import { useRunners } from "../../hooks/useRunners";
import { useRowActions } from "../../hooks/useRowActions";
import { useFeatureActionContext } from "../../hooks/useFeatureActionContext";
import { DepGuide } from "../common/DepGuide";
import { beginDrag, endDrag } from "../../hooks/useDragDrop";
import { buildFeatureActions } from "../../lib/actions/featureActions";
import { buildSelectionActions } from "../../lib/actions/selectionActions";
import { isRangeKey } from "../../lib/selection";
import { buildFeatureForest, type DerivedFeature } from "../../lib/features";
import { flattenDepForest } from "../../lib/depTree";

const LIFECYCLE_TONE = {
  "in-progress": { tone: "active", label: "active" },
  blocked: { tone: "blocked", label: "blocked" },
  finished: { tone: "finished", label: "finished" },
  "mr-open": { tone: "mr", label: "MR open" },
  merged: { tone: "merged", label: "merged" },
} as const;

function featStateClass(f: DerivedFeature): string {
  if (f.lifecycle === "blocked") return "block";
  if (f.lifecycle === "merged" || f.lifecycle === "finished") return "done";
  return "busy";
}

export interface CardFeaturesProps {
  projectId: string;
  features: DerivedFeature[];
}

export function CardFeatures({
  projectId,
  features,
}: CardFeaturesProps): JSX.Element {
  const openFeatureDrawer = useWorkspace((s) => s.openFeatureDrawer);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const mergedExpanded = useWorkspace(
    (s) => s.mergedExpanded[projectId] ?? false,
  );
  const toggleMergedExpanded = useWorkspace((s) => s.toggleMergedExpanded);
  const { runners } = useRunners();

  const featureCtx = useFeatureActionContext(projectId);
  const { rowProps, overlays } = useRowActions();

  // Subscribed so checkboxes react to toggles from any surface.
  const selProjectId = useSelection((s) => s.projectId);
  const selFeatureIds = useSelection((s) => s.featureIds);
  const toggleFeatureSel = useSelection((s) => s.toggleFeature);
  const rangeFeatureSel = useSelection((s) => s.rangeFeature);
  const requestVerb = useSelection((s) => s.requestVerb);
  const clearSel = useSelection((s) => s.clear);
  const selScoped = selProjectId === projectId;
  const selTaskIds = useSelection((s) => s.taskIds);
  // Selection mode: once anything in this project is marked, every row
  // shows its checkbox. Until then boxes appear only on hover/focus.
  const selActive =
    selScoped && (selTaskIds.size > 0 || selFeatureIds.size > 0);

  // The whole-selection verbs marked rows offer on right-click /
  // long-press / `m` instead of their own menu.
  const selCount = selScoped ? selTaskIds.size + selFeatureIds.size : 0;
  const selectionActions = useMemo(
    () =>
      selCount > 0
        ? buildSelectionActions({
            count: selCount,
            requestVerb,
            clearSelection: clearSel,
          })
        : null,
    [selCount, requestVerb, clearSel],
  );

  const merged = features.filter((f) => f.lifecycle === "merged");

  // Tree is built from the *visible* set, so collapsing the merged
  // bucket promotes anything that depended on a merged feature to a
  // root rather than hiding it along with its parent.
  const rows = useMemo(() => {
    const visible = mergedExpanded
      ? features
      : features.filter((f) => f.lifecycle !== "merged");
    return flattenDepForest(buildFeatureForest(visible));
  }, [features, mergedExpanded]);

  // Visual order of the rendered rows, for shift-click ranges. Derived
  // from `rows` so a collapsed merged bucket is out of range reach.
  const orderedFeatureIds = useMemo(
    () => rows.map((row) => row.node.item.id),
    [rows],
  );

  if (features.length === 0) {
    return (
      <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
        No features yet.
      </div>
    );
  }

  return (
    <div>
      {rows.map((row) => {
        const f = row.node.item;
        const tone = LIFECYCLE_TONE[f.lifecycle];
        const runnerId = featureAssignments[f.id];
        const runner = runners.find((r) => r.runner_id === runnerId);
        const pct = Math.round(f.progress * 100);
        const stateClass = featStateClass(f);
        const actions = buildFeatureActions(f, featureCtx);
        const marked = selScoped && selFeatureIds.has(f.id);
        const rp = rowProps(
          actions,
          f.name,
          // Selection mode is modal: Enter toggles like a click, it
          // does not open the drawer.
          selActive
            ? () => toggleFeatureSel(projectId, f.id)
            : () => openFeatureDrawer(projectId, f.id),
          {
            selectionActions: marked ? selectionActions ?? undefined : undefined,
          },
        );

        return (
          <div
            key={f.id}
            className={`feat ${stateClass}`}
            style={{
              marginBottom: 8,
              cursor: "pointer",
              // Indent nested features so the tree reads at a glance.
              // The guide glyphs carry the exact structure; this just
              // gives each level a visible step.
              marginLeft: row.depth > 0 ? row.depth * 12 : undefined,
            }}
          >
            <div
              className={`feat-head${marked ? " marked" : ""}`}
              {...rp}
              onKeyDown={(e) => {
                // Shift+V ranges from the anchor — keyboard parity
                // with shift-click for rows focused via Tab.
                if (isRangeKey(e)) {
                  e.preventDefault();
                  rangeFeatureSel(projectId, orderedFeatureIds, f.id);
                  return;
                }
                rp.onKeyDown(e);
              }}
              draggable
              onDragStart={(e) =>
                beginDrag(e, {
                  source: "feature-header",
                  kind: "assign",
                  target: {
                    projectId,
                    featureId: f.id,
                    currentRunnerId: runnerId,
                  },
                  title: f.name,
                })
              }
              onDragEnd={endDrag}
              onClick={(e) => {
                if (
                  (e.target as HTMLElement).closest(
                    "button, .caret, .assign-chip, a, .selbox",
                  )
                )
                  return;
                // Shift-click anywhere on the head is a selection
                // gesture, not an open: range from the anchor, or
                // start a selection here.
                if (e.shiftKey) {
                  rangeFeatureSel(projectId, orderedFeatureIds, f.id);
                  return;
                }
                // Selection mode is modal: clicks toggle, never open.
                if (selActive) {
                  toggleFeatureSel(projectId, f.id);
                  return;
                }
                openFeatureDrawer(projectId, f.id);
              }}
              // Shift-click would otherwise extend the browser's text
              // selection across the rows before our click handler
              // runs. The explicit focus() keeps what preventDefault
              // suppresses: accelerators target the last-clicked row.
              onMouseDown={(e) => {
                if (e.shiftKey) {
                  e.preventDefault();
                  e.currentTarget.focus();
                }
              }}
            >
              {/* Reserved 12px slot: empty until hover/selection, then
                  the checkbox materializes — content never shifts. */}
              <span
                className={`glyph-slot${marked || selActive ? " boxed" : ""}`}
              >
                <span
                  className={`selbox${marked ? " on" : ""}`}
                  role="checkbox"
                  aria-checked={marked}
                  aria-label={`Select feature ${f.name}`}
                  onClick={(e) => {
                    e.stopPropagation();
                    if (e.shiftKey)
                      rangeFeatureSel(projectId, orderedFeatureIds, f.id);
                    else toggleFeatureSel(projectId, f.id);
                  }}
                />
              </span>
              <span className="name">
                <DepGuide
                  prefix={row.prefix}
                  inCycle={row.node.inCycle}
                  extraDeps={row.node.extraDeps}
                  extraLabel="features"
                />
                {f.name}
              </span>
              <span className={`life-badge ${tone.tone}`}>{tone.label}</span>
              {runner ? (
                <span
                  className={`assign-chip ${runner.status !== "online" ? "warn" : ""}`}
                  title="Assigned runner"
                >
                  🖥 {runner.runner_id}
                </span>
              ) : (
                <span className="assign-chip empty">· unassigned ·</span>
              )}
              <span className="bar">
                <i style={{ width: `${pct}%` }} />
              </span>
              <span className="prog">
                {f.taskCount.completed}/{f.taskCount.total}
              </span>
            </div>
            <div
              style={{
                fontSize: 10.5,
                color: "#9098a1",
                padding: "2px 0 2px 6px",
              }}
            >
              {f.id} · {f.taskCount.total} tasks
              {f.dependsOn.length > 0 && (
                <span title={`Waits on: ${f.dependsOn.join(", ")}`}>
                  {" · waits on "}
                  {f.dependsOn.join(", ")}
                </span>
              )}
              {f.prUrl && (
                <>
                  {" · "}
                  <a
                    href={f.prUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ color: "#6a8bff" }}
                  >
                    MR
                  </a>
                </>
              )}
            </div>
          </div>
        );
      })}

      {!mergedExpanded && merged.length > 0 && (
        <button
          onClick={() => toggleMergedExpanded(projectId)}
          style={{
            border: "1px dashed #22272c",
            padding: "5px 8px",
            width: "100%",
            textAlign: "left",
            color: "#6b757e",
            fontSize: 11,
            marginTop: 6,
          }}
        >
          ▸ {merged.length} merged feature{merged.length === 1 ? "" : "s"}
        </button>
      )}

      {overlays}
    </div>
  );
}
