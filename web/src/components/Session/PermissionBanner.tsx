/**
 * PermissionBanner — pending OpenCode permission requests for a live
 * instance, with respond actions.
 *
 * Poll-backed (10s) against the control pending-permissions endpoint;
 * responses use OpenCode's vocabulary: "once" | "always" | "reject"
 * (the control handler is a pass-through — pinned during Phase 3).
 */
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { controlPendingPermissions, controlRespondPermission } from "../../lib/api";
import { useUI } from "../../store/ui";

export function PermissionBanner({
  runnerId,
  instanceId,
  sessionId,
}: {
  runnerId: string;
  instanceId: string;
  sessionId?: string;
}): JSX.Element | null {
  const toast = useUI((s) => s.toast);
  const qc = useQueryClient();
  const key = ["v2", "permissions", runnerId, instanceId];
  const q = useQuery({
    queryKey: key,
    queryFn: () => controlPendingPermissions(runnerId, instanceId),
    refetchInterval: 10_000,
  });

  const pending = (q.data?.permissions ?? []).filter(
    (p) => !sessionId || !p.sessionID || p.sessionID === sessionId,
  );
  if (pending.length === 0) return null;

  const respond = async (permissionId: string, response: "once" | "always" | "reject") => {
    try {
      await controlRespondPermission(
        runnerId,
        instanceId,
        sessionId ?? pending.find((p) => p.id === permissionId)?.sessionID ?? "",
        permissionId,
        response,
      );
      toast(`Permission ${response === "reject" ? "rejected" : "granted"}`, "success");
    } catch (err) {
      toast(`Respond failed: ${(err as Error)?.message ?? err}`, "error");
    } finally {
      void qc.invalidateQueries({ queryKey: key });
    }
  };

  return (
    <div
      style={{
        gridColumn: "1 / -1",
        background: "#2a2314",
        borderBottom: "1px solid #4a3d1e",
        padding: "6px 12px",
        fontSize: 12,
        display: "flex",
        flexDirection: "column",
        gap: 4,
      }}
    >
      {pending.map((p) => (
        <div key={p.id} style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <span style={{ color: "#f4b23a" }}>permission</span>
          <span>{p.title || p.type || p.id}</span>
          {p.pattern && <code style={{ fontSize: 11 }}>{p.pattern}</code>}
          <span style={{ flex: 1 }} />
          <button onClick={() => void respond(p.id, "once")}>Allow once</button>
          <button onClick={() => void respond(p.id, "always")}>Always</button>
          <button onClick={() => void respond(p.id, "reject")} style={{ color: "#e06c5f" }}>
            Reject
          </button>
        </div>
      ))}
    </div>
  );
}
