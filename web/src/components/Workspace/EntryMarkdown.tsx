/**
 * EntryMarkdown — rendered markdown body for a Brain entry.
 *
 * First consumer of the react-markdown + remark-gfm deps that shipped
 * with the PWA scaffold. Adds:
 *   • heading ids (instance-prefixed so two readers side-by-side don't
 *     collide) taken from the same `extractHeadings` pre-pass the TOC
 *     uses, keyed by remark's source line — a pure derivation, so ids
 *     are stable under StrictMode double-render and always match the
 *     TOC slugs;
 *   • entry-link interception — `[Title](projects/x/plan/ab12cd34.md)`
 *     or `[Title](ab12cd34)` opens in the reader instead of navigating
 *     the SPA away;
 *   • external links forced to a new tab.
 *
 * Styling lives under `.entry-md` in `styles/global.css`.
 */
import React, { useId, useMemo } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  classifyEntryHref,
  extractHeadings,
  slugifyHeading,
} from "../../lib/entries";

function textOf(node: React.ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (React.isValidElement(node)) {
    return textOf((node.props as { children?: React.ReactNode }).children);
  }
  return "";
}

/** The subset of remark's node position react-markdown hands components. */
interface MdNodeProps {
  node?: { position?: { start?: { line?: number } } };
  children?: React.ReactNode;
}

export function EntryMarkdown({
  content,
  onOpenEntry,
}: {
  content: string;
  onOpenEntry?: (ref: string) => void;
}): JSX.Element {
  const instanceId = useId();

  const components = useMemo(() => {
    const slugByLine = new Map<number, string>();
    for (const h of extractHeadings(content)) slugByLine.set(h.line, h.slug);

    const heading = (Tag: "h1" | "h2" | "h3" | "h4" | "h5" | "h6") => {
      return function Heading({ node, children }: MdNodeProps): JSX.Element {
        const line = node?.position?.start?.line;
        // Headings the pre-pass can't detect (setext, blockquoted) fall
        // back to an unnumbered slug of their rendered text.
        const slug =
          (line !== undefined && slugByLine.get(line)) ||
          slugifyHeading(textOf(children)) ||
          "section";
        return <Tag id={`${instanceId}-${slug}`}>{children}</Tag>;
      };
    };

    return {
      h1: heading("h1"),
      h2: heading("h2"),
      h3: heading("h3"),
      h4: heading("h4"),
      h5: heading("h5"),
      h6: heading("h6"),
      a: ({ href, children }: { href?: string; children?: React.ReactNode }) => {
        const target = classifyEntryHref(href || "");
        if (target.kind === "entry" && onOpenEntry) {
          return (
            <a
              href={`/${target.ref}`}
              className="entry-link"
              title={target.ref}
              onClick={(e) => {
                e.preventDefault();
                onOpenEntry(target.ref);
              }}
            >
              {children}
            </a>
          );
        }
        if (target.kind === "anchor") {
          const slug = (href || "").slice(1);
          return (
            <a
              href={href}
              onClick={(e) => {
                e.preventDefault();
                document
                  .getElementById(`${instanceId}-${slug}`)
                  ?.scrollIntoView({ block: "start" });
              }}
            >
              {children}
            </a>
          );
        }
        return (
          <a href={href} target="_blank" rel="noopener noreferrer">
            {children}
          </a>
        );
      },
    };
  }, [content, instanceId, onOpenEntry]);

  return (
    <div className="entry-md" data-md-instance={instanceId}>
      <ReactMarkdown remarkPlugins={[remarkGfm]} components={components}>
        {content}
      </ReactMarkdown>
    </div>
  );
}
