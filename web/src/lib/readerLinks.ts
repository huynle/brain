import { classifyEntryHref } from "./entries";

export function readerHref(ref: string, hash = ""): string {
  return `/read.html?entry=${encodeURIComponent(ref)}${hash ? "#" + hash.replace(/^#/, "") : ""}`;
}

// Resolve relative Markdown paths against the entry, not the reader page.
export function readerLink(
  href: string,
  path: string,
  origin: string,
): string | undefined {
  if (href.startsWith("#")) return href;
  let u: URL;
  try {
    u = new URL(href, origin + "/" + path);
  } catch {
    return undefined;
  }
  if (u.origin !== origin) return undefined;
  const entry = u.searchParams.get("entry");
  if (entry && ["/", "/read", "/read.html"].includes(u.pathname))
    return readerHref(entry, u.hash);
  const plain = href.split(/[?#]/)[0];
  const classified = classifyEntryHref(plain);
  if (/^[a-z0-9]{8}(?:\.md)?$/.test(plain))
    return readerHref(plain.replace(/\.md$/, ""), u.hash);
  if (
    /^(?:\/?projects\/|\/?global\/)/.test(plain) &&
    classified.kind === "entry"
  )
    return readerHref(classified.ref, u.hash);
  let decoded: string;
  try {
    decoded = decodeURIComponent(u.pathname).replace(/^\//, "");
  } catch {
    return undefined;
  }
  if (/^(projects|global)\/.*\.md$/.test(decoded))
    return readerHref(decoded, u.hash);
  return undefined;
}

interface WikiNode {
  type: string;
  value?: string;
  children?: WikiNode[];
  url?: string;
}
// Transform only Markdown text nodes; code, images, and existing links stay literal.
export function remarkReaderWikiLinks() {
  return (tree: WikiNode) => {
    const walk = (node: WikiNode) => {
      if (
        !node.children ||
        ["link", "image", "code", "inlineCode"].includes(node.type)
      )
        return;
      node.children = node.children.flatMap((child) => {
        if (child.type !== "text" || !child.value) {
          walk(child);
          return [child];
        }
        const out: WikiNode[] = [];
        let start = 0;
        for (const m of child.value.matchAll(/\[\[([^\]\n]+)\]\]/g)) {
          if (m.index! > start)
            out.push({
              type: "text",
              value: child.value.slice(start, m.index),
            });
          const [target, ...alias] = m[1].split("|");
          out.push({
            type: "link",
            url: target.trim(),
            children: [
              { type: "text", value: alias.length ? alias.join("|") : target },
            ],
          });
          start = m.index! + m[0].length;
        }
        if (!out.length) return [child];
        if (start < child.value.length)
          out.push({ type: "text", value: child.value.slice(start) });
        return out;
      });
    };
    walk(tree);
  };
}
