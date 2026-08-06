/**
 * SettingsModal — full config editor + workspace preferences.
 *
 * Loads three things in parallel:
 *   1. The workspace zustand store (local, client-only settings).
 *   2. GET /api/v1/config          — current config values from disk.
 *   3. GET /api/v1/config/schema   — field metadata (type, section,
 *                                    enum, requires_restart, secret).
 *
 * The schema is the source of truth for what to render. Every field
 * dispatches on `kind` to a type-aware input, and every field carries
 * a "requires restart" badge and optional tooltip.
 *
 * Save behavior:
 *   • Only enabled when the edited copy differs from the loaded copy.
 *   • Calls PUT /api/v1/config.
 *   • Server responds with { hot_reloaded, requires_restart, backup_path }.
 *   • We toast a summary and, if the server reports requires_restart
 *     items, show a banner inside the modal telling the user which
 *     changes need a restart.
 *
 * Secrets (server.oauth_pin, server.jwt_secret, runner.api_token)
 * arrive as the sentinel "__brain_unchanged__" and are displayed as
 * "•••••••• (unchanged)" with a "clear/change" affordance. If the
 * user doesn't touch the field, we send the sentinel back and the
 * server preserves the stored value.
 */
import { useEffect, useMemo, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { Modal } from "../common/Modal";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { useModal } from "../../store/modal";
import { useWorkspace, WORKSPACE_STORAGE_KEY } from "../../store/workspace";
import { useUI } from "../../store/ui";
import {
  getServerConfig,
  getConfigSchema,
  updateServerConfig,
  ApiError,
  type ConfigField,
  type ServerConfig,
} from "../../lib/api";
import { getByPath, setByPath, deepEqual } from "../../lib/objectPath";

// Sentinel the server writes into redacted secret fields on GET
// responses; we echo it back on PUT to keep the stored value.
const SECRET_SENTINEL = "__brain_unchanged__";

/** Sections rendered in order at the top of the settings body. */
const SECTION_ORDER = [
  "server",
  "task_defaults",
  "feature_checkout",
  "embedding",
  "assistant",
  "attachments",
  "attachment_extraction",
  "runner",
  "runner.opencode",
  "mcp",
  "plugins",
];

const SECTION_LABELS: Record<string, string> = {
  server: "Server",
  task_defaults: "Task defaults",
  feature_checkout: "Feature checkout automation",
  embedding: "Embedding (semantic search)",
  assistant: "Assistant",
  attachments: "Attachments",
  attachment_extraction: "Attachment extraction",
  runner: "Runner",
  "runner.opencode": "Runner — OpenCode executor",
  mcp: "MCP",
  plugins: "Plugins",
};

export function SettingsModal(): JSX.Element {
  const close = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);
  const queryClient = useQueryClient();

  // ─── workspace-only (client) settings ────────────────────────
  const sidebar = useWorkspace((s) => s.sidebarSection);
  const toggleSection = useWorkspace((s) => s.toggleSidebarSection);
  const streaming = useWorkspace((s) => s.streaming);
  const setStreaming = useWorkspace((s) => s.setStreaming);
  const theme = useWorkspace((s) => s.theme);
  const setTheme = useWorkspace((s) => s.setTheme);

  // ─── server config (from API) ────────────────────────────────
  const cfgQ = useQuery({
    queryKey: ["settings", "config"],
    queryFn: getServerConfig,
    staleTime: 60_000,
  });
  const schemaQ = useQuery({
    queryKey: ["settings", "config-schema"],
    queryFn: getConfigSchema,
    staleTime: Infinity, // schema shape doesn't change at runtime
  });

  // Local working copy of the config. Initialised from cfgQ once it
  // resolves; edits are stored here and diffed against cfgQ.data.
  const [edited, setEdited] = useState<ServerConfig | null>(null);
  useEffect(() => {
    if (cfgQ.data && edited === null) {
      setEdited(cfgQ.data.config);
    }
  }, [cfgQ.data, edited]);

  const [saving, setSaving] = useState(false);
  const [saveResult, setSaveResult] = useState<{
    hot_reloaded: string[];
    requires_restart: string[];
    backup_path: string;
  } | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Dirty check — anything the user edited differs from what the
  // server originally returned.
  const isDirty = useMemo(() => {
    if (!cfgQ.data || !edited) return false;
    return !deepEqual(cfgQ.data.config, edited);
  }, [cfgQ.data, edited]);

  // Field mutator — deep-set a value into the working copy.
  const setField = (path: string, value: unknown) => {
    setEdited((prev) => (prev === null ? prev : setByPath(prev, path, value)));
    // Clear any prior save result once the user touches something new.
    setSaveResult(null);
    setSaveError(null);
  };

  const doSave = async () => {
    if (!edited) return;
    setSaving(true);
    setSaveResult(null);
    setSaveError(null);
    try {
      const res = await updateServerConfig(edited);
      setSaveResult(res);
      // Refetch so `cfgQ.data` reflects what we just wrote (with
      // secrets re-redacted). This also resets the dirty check.
      const fresh = await queryClient.fetchQuery({
        queryKey: ["settings", "config"],
        queryFn: getServerConfig,
      });
      setEdited(fresh.config);
      if (res.requires_restart.length > 0) {
        toast(
          `Saved. ${res.requires_restart.length} field(s) need a server restart.`,
          "info",
        );
      } else {
        toast(
          res.hot_reloaded.length > 0
            ? `Saved. Applied ${res.hot_reloaded.length} live.`
            : "Saved.",
          "success",
        );
      }
    } catch (err) {
      const msg =
        err instanceof ApiError
          ? err.message
          : (err as Error).message ?? "unknown error";
      setSaveError(msg);
      toast(`Save failed: ${msg}`, "error");
    } finally {
      setSaving(false);
    }
  };

  const doReset = () => {
    if (typeof window !== "undefined") {
      window.localStorage.removeItem(WORKSPACE_STORAGE_KEY);
      window.location.reload();
    }
  };

  const body = (() => {
    if (cfgQ.isLoading || schemaQ.isLoading) {
      return <Loading label="Loading configuration…" />;
    }
    if (cfgQ.error) {
      return (
        <ErrorState
          error={cfgQ.error}
          onRetry={() => cfgQ.refetch()}
          title="Couldn't load server config"
        />
      );
    }
    if (schemaQ.error) {
      return (
        <ErrorState
          error={schemaQ.error}
          onRetry={() => schemaQ.refetch()}
          title="Couldn't load config schema"
        />
      );
    }
    if (!edited || !schemaQ.data) return null;

    // Group schema by section.
    const bySection: Record<string, ConfigField[]> = {};
    for (const f of schemaQ.data.fields) {
      if (!bySection[f.section]) bySection[f.section] = [];
      bySection[f.section].push(f);
    }
    const sections = SECTION_ORDER.filter((s) => bySection[s]);

    return (
      <>
        {/* Workspace (client-only) */}
        <SectionCard title="Workspace (this browser)">
          <div className="setting-field">
            <label>Theme</label>
            <select
              value={theme}
              onChange={(e) =>
                setTheme(e.target.value as "dark" | "light" | "system")
              }
            >
              <option value="dark">Dark</option>
              <option value="light">Light</option>
              <option value="system">System</option>
            </select>
          </div>
          <label className="setting-checkbox">
            <input
              type="checkbox"
              checked={streaming}
              onChange={(e) => setStreaming(e.target.checked)}
            />
            Live logs streaming
          </label>
          <div style={{ marginTop: 8 }}>
            <div style={{ color: "#f4b23a", fontSize: 10, marginBottom: 4 }}>
              Sidebar sections
            </div>
            <label className="setting-checkbox">
              <input
                type="checkbox"
                checked={sidebar.projects}
                onChange={() => toggleSection("projects")}
              />
              Show Projects
            </label>
            <label className="setting-checkbox">
              <input
                type="checkbox"
                checked={sidebar.sessions}
                onChange={() => toggleSection("sessions")}
              />
              Show Sessions
            </label>
            <label className="setting-checkbox">
              <input
                type="checkbox"
                checked={sidebar.runners}
                onChange={() => toggleSection("runners")}
              />
              Show Runners
            </label>
          </div>
          <div style={{ marginTop: 10, fontSize: 10, color: "#6b757e" }}>
            Persisted key: <code>{WORKSPACE_STORAGE_KEY}</code>
          </div>
        </SectionCard>

        {/* Server config sections */}
        {saveResult && saveResult.requires_restart.length > 0 && (
          <div
            style={{
              padding: "8px 10px",
              background: "#f4b23a22",
              border: "1px solid #f4b23a",
              borderRadius: 4,
              color: "#f4b23a",
              fontSize: 11,
            }}
          >
            <b>Restart required</b> to apply:{" "}
            {saveResult.requires_restart.join(", ")}
          </div>
        )}
        {saveResult && saveResult.hot_reloaded.length > 0 && (
          <div
            style={{
              padding: "8px 10px",
              background: "#6fca7d22",
              border: "1px solid #6fca7d55",
              borderRadius: 4,
              color: "#6fca7d",
              fontSize: 11,
            }}
          >
            Applied live: {saveResult.hot_reloaded.join(", ")}
          </div>
        )}
        {saveError && (
          <div
            style={{
              padding: "8px 10px",
              background: "#d9606022",
              border: "1px solid #d96060",
              borderRadius: 4,
              color: "#d96060",
              fontSize: 11,
            }}
          >
            {saveError}
          </div>
        )}

        {sections.map((sec) => (
          <SectionCard
            key={sec}
            title={SECTION_LABELS[sec] ?? sec}
          >
            {bySection[sec].map((f) => (
              <FieldRow
                key={f.path}
                field={f}
                value={getByPath(edited, f.path)}
                onChange={(v) => setField(f.path, v)}
              />
            ))}
          </SectionCard>
        ))}

        {cfgQ.data && (
          <div style={{ fontSize: 10, color: "#6b757e", padding: "4px 2px" }}>
            Config file: <code>{cfgQ.data.path}</code>
          </div>
        )}
      </>
    );
  })();

  return (
    <Modal
      title="Settings"
      onClose={close}
      className="wide"
      footer={
        <>
          <button onClick={doReset} title="Clear workspace preferences and reload">
            Reset workspace
          </button>
          <button
            className="primary"
            onClick={doSave}
            disabled={!isDirty || saving}
            title={
              !isDirty
                ? "No changes to save"
                : saving
                  ? "Saving…"
                  : "Save changes to ~/.config/brain/config.yaml"
            }
          >
            {saving ? "Saving…" : isDirty ? "Save changes" : "Saved"}
          </button>
          <button onClick={close}>Close</button>
        </>
      }
    >
      {body}
    </Modal>
  );
}

// ─── section wrapper ─────────────────────────────────────────────

function SectionCard({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="mod-group">
      <h4>{title}</h4>
      {children}
    </div>
  );
}

// ─── field renderer ──────────────────────────────────────────────

interface FieldRowProps {
  field: ConfigField;
  value: unknown;
  onChange: (value: unknown) => void;
}

function FieldRow({ field, value, onChange }: FieldRowProps): JSX.Element {
  const badge = (() => {
    if (field.secret) {
      return (
        <span className="chip mini" title="Stored value never leaves the server">
          secret
        </span>
      );
    }
    if (field.requires_restart) {
      return (
        <span
          className="chip mini"
          style={{ color: "#f4b23a", borderColor: "#f4b23a55" }}
          title="Requires server restart to apply"
        >
          restart
        </span>
      );
    }
    return null;
  })();

  return (
    <div className="setting-field" style={{ flexWrap: "wrap" }}>
      <label
        style={{
          display: "flex",
          gap: 6,
          alignItems: "center",
          minWidth: 180,
        }}
        title={field.path}
      >
        {field.label}
        {badge}
      </label>
      <FieldInput field={field} value={value} onChange={onChange} />
      {field.help && (
        <div
          style={{
            width: "100%",
            fontSize: 10,
            color: "#6b757e",
            paddingLeft: 4,
          }}
        >
          {field.help}
        </div>
      )}
    </div>
  );
}

function FieldInput({ field, value, onChange }: FieldRowProps): JSX.Element {
  switch (field.kind) {
    case "bool": {
      // TaskDefaults' *bool fields serialise as null when unset;
      // treat null/undefined as false for the toggle, but pass a
      // real boolean back on change.
      const checked = value === true;
      return (
        <input
          type="checkbox"
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
        />
      );
    }

    case "int":
    case "duration_ms": {
      const num =
        typeof value === "number"
          ? value
          : typeof value === "string" && value !== ""
            ? Number(value)
            : "";
      return (
        <input
          type="number"
          value={num as number | ""}
          onChange={(e) => {
            const v = e.target.value;
            onChange(v === "" ? 0 : Number(v));
          }}
          style={{ width: 160 }}
        />
      );
    }

    case "enum": {
      const s = typeof value === "string" ? value : "";
      return (
        <select
          value={s}
          onChange={(e) => onChange(e.target.value)}
          style={{ minWidth: 160 }}
        >
          {(field.enum ?? []).map((opt) => (
            <option key={opt} value={opt}>
              {opt === "" ? "(unset)" : opt}
            </option>
          ))}
        </select>
      );
    }

    case "string_array": {
      const arr = Array.isArray(value) ? (value as string[]) : [];
      // Represent as newline-separated in a textarea — matches how
      // users typically edit YAML arrays.
      return (
        <textarea
          value={arr.join("\n")}
          onChange={(e) => {
            const lines = e.target.value
              .split("\n")
              .map((s) => s.trim())
              .filter(Boolean);
            onChange(lines);
          }}
          rows={Math.max(2, Math.min(6, arr.length + 1))}
          placeholder="one per line"
          style={{ flex: 1, minWidth: 200, fontFamily: "inherit" }}
        />
      );
    }

    case "secret": {
      const isRedacted = value === SECRET_SENTINEL;
      const s = typeof value === "string" ? value : "";
      return (
        <div style={{ display: "flex", gap: 6, flex: 1, minWidth: 200 }}>
          <input
            type="password"
            value={isRedacted ? "" : s}
            placeholder={isRedacted ? "•••••••• (unchanged)" : "(empty)"}
            onChange={(e) => onChange(e.target.value)}
            style={{ flex: 1 }}
          />
          {isRedacted && (
            <button
              type="button"
              onClick={() => onChange("")}
              title="Clear the stored secret"
              style={{ fontSize: 10 }}
            >
              Clear
            </button>
          )}
        </div>
      );
    }

    case "url":
    case "path":
    case "string":
    default: {
      const s = typeof value === "string" ? value : value == null ? "" : String(value);
      return (
        <input
          type="text"
          value={s}
          onChange={(e) => onChange(e.target.value)}
          spellCheck={false}
          style={{ flex: 1, minWidth: 200 }}
        />
      );
    }
  }
}
