/**
 * panes-v2 BrowserLeaf.
 *
 * Mock URL viewer. Renders an iframe with a URL from `target.url`
 * and a URL input above it so users can navigate. This is strictly
 * a viewer — we do not proxy traffic; browsers will refuse to render
 * many origins (X-Frame-Options / frame-ancestors), and that's
 * expected. The iframe uses a strict sandbox.
 *
 * The URL input is committed on Enter or blur. Empty URL renders an
 * empty state.
 */
import { useEffect, useState } from "react";

export function BrowserLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const initialUrl = (target.url as string | undefined) ?? "";
  const [url, setUrl] = useState(initialUrl);
  const [committed, setCommitted] = useState(initialUrl);

  // If the underlying target changes (e.g. tree rehydrated with a
  // different URL), pick up the new value.
  useEffect(() => {
    setUrl(initialUrl);
    setCommitted(initialUrl);
  }, [initialUrl]);

  const commit = () => {
    // Cheap validation: only commit http(s):// URLs. Anything else
    // shows an error state below without swapping the iframe src.
    setCommitted(url);
  };

  const isHttp = /^https?:\/\//i.test(committed);

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      <div className="p2-browser-urlbar">
        <input
          value={url}
          placeholder="https://…"
          onChange={(e) => setUrl(e.target.value)}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commit();
            }
          }}
          aria-label="URL"
        />
      </div>
      {!committed && (
        <div
          style={{
            flex: 1,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: "var(--p2-fg-faint)",
            fontSize: 12,
          }}
        >
          Enter an https:// URL above to load a page.
        </div>
      )}
      {committed && !isHttp && (
        <div
          style={{
            flex: 1,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: "var(--p2-fg-faint)",
            fontSize: 12,
          }}
        >
          Only http:// and https:// URLs are supported.
        </div>
      )}
      {committed && isHttp && (
        <iframe
          className="p2-browser-iframe"
          src={committed}
          sandbox="allow-same-origin allow-scripts"
          title={committed}
          style={{ flex: 1 }}
        />
      )}
    </div>
  );
}
