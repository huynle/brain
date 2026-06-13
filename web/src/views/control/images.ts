// Image attachment helpers for the chat composer. Pasted/dropped images are
// downscaled client-side (keeps payloads small) and encoded as data: URLs,
// which OpenCode accepts directly as file parts — no upload to the runner.

export interface Attachment {
  id: string;
  filename: string;
  mime: string;
  dataUrl: string; // data:<mime>;base64,<...>
}

const MAX_DIM = 1568; // Claude's optimal max edge; larger images gain nothing
let counter = 0;

function nextId(): string {
  counter += 1;
  return `att_${counter}_${performance.now().toString(36)}`;
}

/** True for image MIME types we can render and send. */
export function isImageType(mime: string): boolean {
  return mime.startsWith("image/");
}

/**
 * Read an image File/Blob, downscale it so its longest edge is <= MAX_DIM,
 * and return a data-URL attachment. Falls back to the raw bytes if the image
 * can't be decoded (e.g. SVG/exotic formats).
 */
export async function fileToAttachment(file: File | Blob, name?: string): Promise<Attachment> {
  const mime = file.type || "image/png";
  const filename = name || (file instanceof File ? file.name : "") || `pasted-${Date.now()}.png`;

  const rawUrl = await blobToDataUrl(file);
  // SVGs and tiny images: skip the canvas round-trip.
  if (mime === "image/svg+xml") {
    return { id: nextId(), filename, mime, dataUrl: rawUrl };
  }

  try {
    const img = await loadImage(rawUrl);
    const scale = Math.min(1, MAX_DIM / Math.max(img.width, img.height));
    if (scale >= 1) {
      return { id: nextId(), filename, mime, dataUrl: rawUrl };
    }
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(img.width * scale);
    canvas.height = Math.round(img.height * scale);
    const ctx = canvas.getContext("2d");
    if (!ctx) return { id: nextId(), filename, mime, dataUrl: rawUrl };
    ctx.drawImage(img, 0, 0, canvas.width, canvas.height);
    // PNG preserves screenshots/diagrams crisply; everything else → JPEG.
    const outMime = mime === "image/png" ? "image/png" : "image/jpeg";
    const dataUrl = canvas.toDataURL(outMime, 0.9);
    return { id: nextId(), filename, mime: outMime, dataUrl };
  } catch {
    return { id: nextId(), filename, mime, dataUrl: rawUrl };
  }
}

/** Extract image attachments from a clipboard or drag-drop DataTransfer. */
export async function attachmentsFromDataTransfer(dt: DataTransfer | null): Promise<Attachment[]> {
  if (!dt) return [];
  const out: Attachment[] = [];
  const items = dt.items ? Array.from(dt.items) : [];
  for (const item of items) {
    if (item.kind === "file" && item.type.startsWith("image/")) {
      const file = item.getAsFile();
      if (file) out.push(await fileToAttachment(file));
    }
  }
  // Some browsers expose dropped files only via dt.files.
  if (out.length === 0 && dt.files) {
    for (const file of Array.from(dt.files)) {
      if (file.type.startsWith("image/")) out.push(await fileToAttachment(file));
    }
  }
  return out;
}

function blobToDataUrl(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader();
    fr.onload = () => resolve(fr.result as string);
    fr.onerror = () => reject(fr.error);
    fr.readAsDataURL(blob);
  });
}

function loadImage(src: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = reject;
    img.src = src;
  });
}
