/**
 * MermaidDiagram — a ```mermaid fence rendered as a diagram.
 *
 * mermaid is ~600KB, so it is imported dynamically: an entry with no
 * mermaid fence never pays for it. The module is initialised once per
 * page load and shared across every diagram on screen.
 *
 * Rendering happens through mermaid's string API and the SVG is injected
 * with innerHTML. That is safe here in a way it would not be for entry
 * prose: the input is diagram source that mermaid itself parses and the
 * output is markup mermaid generated, not author-supplied HTML. Bad
 * syntax throws and is shown as an error block with the source, rather
 * than blanking the entry.
 */
import { useEffect, useId, useRef, useState } from "react";
import { useResolvedTheme } from "../../hooks/useResolvedTheme";

type MermaidModule = typeof import("mermaid")["default"];

let mermaidPromise: Promise<MermaidModule> | null = null;
let initialisedTheme: string | null = null;

async function getMermaid(dark: boolean): Promise<MermaidModule> {
  if (!mermaidPromise) {
    mermaidPromise = import("mermaid").then((m) => m.default);
  }
  const mermaid = await mermaidPromise;
  const theme = dark ? "dark" : "default";
  if (initialisedTheme !== theme) {
    mermaid.initialize({
      startOnLoad: false,
      theme,
      securityLevel: "strict",
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, monospace",
    });
    initialisedTheme = theme;
  }
  return mermaid;
}

export function MermaidDiagram({ source }: { source: string }): JSX.Element {
  const rawId = useId();
  // mermaid uses the id as an SVG element id; React's useId contains ":".
  const domId = `mermaid-${rawId.replace(/[^a-zA-Z0-9_-]/g, "")}`;
  const hostRef = useRef<HTMLDivElement | null>(null);
  const [error, setError] = useState<string | null>(null);
  const dark = useResolvedTheme() === "dark";

  useEffect(() => {
    let cancelled = false;
    setError(null);
    void (async () => {
      try {
        const mermaid = await getMermaid(dark);
        const { svg } = await mermaid.render(domId, source);
        if (cancelled || !hostRef.current) return;
        hostRef.current.innerHTML = svg;
      } catch (e) {
        if (cancelled) return;
        setError(e instanceof Error ? e.message : String(e));
        // mermaid leaves its failed render node parked in the body.
        document.getElementById(`d${domId}`)?.remove();
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [source, domId, dark]);

  if (error) {
    return (
      <div className="mermaid-error">
        <div className="mermaid-error-label">Diagram failed to render</div>
        <div className="mermaid-error-msg">{error}</div>
        <pre>{source}</pre>
      </div>
    );
  }
  return <div className="mermaid-diagram" ref={hostRef} />;
}
