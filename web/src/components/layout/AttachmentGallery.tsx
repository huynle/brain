import { useAuth } from "../../lib/auth";
import type { AttachmentReference } from "../../lib/types";

function isImageAttachment(a: AttachmentReference): boolean {
  const contentType = a.content_type || a.type || "";
  if (contentType.startsWith("image/")) return true;

  const name = (a.filename || a.path || "").toLowerCase();
  return /\.(avif|gif|jpe?g|png|webp|svg)$/.test(name);
}

function attachmentUrl(id: string, token?: string | null): string {
  const base = `/api/v1/attachments/${encodeURIComponent(id)}/content`;
  return token ? `${base}?token=${encodeURIComponent(token)}` : base;
}

function attachmentLabel(a: AttachmentReference): string {
  return a.filename || a.path || a.id;
}

export function AttachmentGallery({ attachments }: { attachments?: AttachmentReference[] }) {
  const token = useAuth((s) => s.token);
  if (!attachments || attachments.length === 0) return null;

  const images = attachments.filter(isImageAttachment);
  const files = attachments.filter((a) => !isImageAttachment(a));

  return (
    <div className="field">
      <label>Attachments</label>
      {images.length > 0 && (
        <div
          style={{
            display: "grid",
            gap: "0.6rem",
            gridTemplateColumns: "repeat(auto-fit, minmax(160px, 1fr))",
          }}
        >
          {images.map((a) => (
            <a
              key={a.id}
              href={attachmentUrl(a.id, token)}
              target="_blank"
              rel="noopener noreferrer"
              style={{ color: "inherit", textDecoration: "none" }}
            >
              <img
                src={attachmentUrl(a.id, token)}
                alt={a.caption || attachmentLabel(a)}
                style={{
                  aspectRatio: "4 / 3",
                  border: "1px solid var(--border)",
                  borderRadius: 10,
                  objectFit: "cover",
                  width: "100%",
                }}
              />
              <div className="mono faint" style={{ fontSize: 11.5, marginTop: "0.25rem" }}>
                {attachmentLabel(a)}
              </div>
            </a>
          ))}
        </div>
      )}
      {files.length > 0 && (
        <div className="col" style={{ gap: "0.35rem", marginTop: images.length ? "0.6rem" : 0 }}>
          {files.map((a) => (
            <a
              key={a.id}
              className="btn sm"
              href={attachmentUrl(a.id, token)}
              target="_blank"
              rel="noopener noreferrer"
              style={{ justifyContent: "flex-start", gap: "0.5rem" }}
            >
              Attachment: {attachmentLabel(a)}
              {(a.content_type || a.type) && <span className="faint">- {a.content_type || a.type}</span>}
            </a>
          ))}
        </div>
      )}
    </div>
  );
}
