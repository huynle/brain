import * as React from "react";
import { useBackgroundOperations, type BackgroundOperation } from "../../store/backgroundOperations";

export function BackgroundOperations() {
  const operations = useBackgroundOperations((s) => s.operations);
  const dismiss = useBackgroundOperations((s) => s.dismiss);
  const running = operations.some((o) => o.state === "running");
  React.useEffect(() => {
    if (!running) return;
    const warn = (event: BeforeUnloadEvent) => { event.preventDefault(); event.returnValue = ""; };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [running]);
  return <BackgroundOperationsView operations={operations} dismiss={dismiss} />;
}

export function BackgroundOperationsView({ operations, dismiss }: { operations: BackgroundOperation[]; dismiss: (id: number) => void }) {
  if (!operations.length) return null;
  return (
    <aside className="background-operations" aria-label="Background operations">
      {operations.map((o) => (
        <section key={o.id} className={`background-operation ${o.state}`}>
          <div className="background-operation-heading">
            <strong>{o.label}</strong>
            {o.state !== "running" && <button onClick={() => dismiss(o.id)} aria-label={`Dismiss ${o.label}`}>×</button>}
          </div>
          <div role="status">{o.state === "running" ? "In progress" : o.state === "finished" ? (o.issue ? "Needs attention" : "Finished") : o.state === "stopped" ? "Stopped" : "Needs attention"} · {o.detail === "Working…" && o.state === "finished" ? "Operation finished. Check the result notification for details." : o.detail}</div>
          {o.state === "running" && <>
            <progress aria-label={`${o.label} progress`} max={o.total || undefined} value={o.total ? o.completed : undefined} />
            <small>You can keep using the dashboard. Keep this tab open.</small>
          </>}
        </section>
      ))}
    </aside>
  );
}
