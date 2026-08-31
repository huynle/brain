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
 *   • external links forced to a new tab;
 *   • images resolved against the entry's attachments — `![x](gradient.png)`,
 *     `![x](attachment:12)` and `![x](./figures/gradient.png)` all reach the
 *     attachment's authed bytes, which a bare <img src> could not (see
 *     `fetchAttachmentObjectURL`);
 *   • ```mermaid fences rendered as diagrams, with mermaid loaded on
 *     demand so entries without one never pay for it.
 *
 * Styling lives under `.entry-md` in `styles/global.css`.
 */
import React, { useId, useMemo } from "react";
import ReactMarkdown, { defaultUrlTransform } from "react-markdown";
import remarkGfm from "remark-gfm";
import { AttachmentImage } from "./AttachmentImage";
import { MermaidDiagram } from "./MermaidDiagram";
import {
  classifyEntryHref,
  extractHeadings,
  isLoneImageParagraph,
  slugifyHeading,
  type MarkdownNode,
} from "../../lib/entries";
import { entryHref } from "../../lib/entryNav";
import { resolveAttachmentSrc } from "../../lib/attachments";
import type { AttachmentReference } from "../../lib/types";

function textOf(node: React.ReactNode): string {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(textOf).join("");
  if (React.isValidElement(node)) {
    return textOf((node.props as { children?: React.ReactNode }).children);
  }
  return "";
}

/** The subset of the hast node react-markdown hands components. */
interface MdNodeProps {
  node?: MarkdownNode & { position?: { start?: { line?: number } } };
  children?: React.ReactNode;
}

/**
 * react-markdown drops URLs whose scheme isn't in its allowlist, so
 * `![x](attachment:12)` reached the img component as an empty string and
 * rendered as a missing image. `attachment:` refs are resolved against the
 * entry's own attachment list and never emitted as an href, so letting
 * them through costs nothing; everything else keeps the default sanitizer.
 */
function entryUrlTransform(url: string): string {
  if (/^attachment:/i.test(url)) return url;
  return defaultUrlTransform(url);
}

export function EntryMarkdown({
  content,
  onOpenEntry,
  attachments,
}: {
  content: string;
  onOpenEntry?: (ref: string) => void;
  /** The entry's attachments, so `![x](file.png)` can find its bytes. */
  attachments?: readonly AttachmentReference[];
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
              href={entryHref(target.ref)}
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
      img: ({ src, alt, title }: {
        src?: string;
        alt?: string;
        title?: string;
      }) => {
        const resolved = resolveAttachmentSrc(src, attachments);
        if (resolved && "attachment" in resolved) {
          return (
            <AttachmentImage
              attachment={{
                ...resolved.attachment,
                caption: alt || resolved.attachment.caption,
              }}
              className="entry-md-img"
            />
          );
        }
        if (resolved) {
          return (
            <img
              className="entry-md-img"
              src={resolved.url}
              alt={alt || ""}
              title={title}
              loading="lazy"
            />
          );
        }
        // A relative src with no matching attachment would resolve against
        // the SPA origin and render a broken-image icon. Say what is wrong
        // instead.
        return (
          <span className="entry-md-img-missing" title={src}>
            🖼 missing image: {alt || src}
          </span>
        );
      },
      // A paragraph holding nothing but an image is a figure and gets its
      // own block; an image among words stays inline. See
      // `isLoneImageParagraph` for why CSS can't make this call.
      p: ({ node, children }: MdNodeProps) => (
        <p className={isLoneImageParagraph(node) ? "entry-md-figure" : undefined}>
          {children}
        </p>
      ),
      code: ({
        className,
        children,
      }: {
        className?: string;
        children?: React.ReactNode;
      }) => {
        const lang = /language-(\w+)/.exec(className || "")?.[1];
        if (lang === "mermaid") {
          return <MermaidDiagram source={textOf(children).trim()} />;
        }
        return <code className={className}>{children}</code>;
      },
    };
  }, [content, instanceId, onOpenEntry, attachments]);

  return (
    <div className="entry-md" data-md-instance={instanceId}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={components}
        urlTransform={entryUrlTransform}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}
