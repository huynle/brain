import { useEffect, useState } from "react";
import { attachmentDisplayLabel, attachmentDisplayUrl, isImageAttachment } from "../../lib/attachments";
import { useAuth } from "../../lib/auth";
import type { AttachmentReference } from "../../lib/types";

function formatAttachmentSize(size: number | undefined): string | undefined {
  if (size === undefined) return undefined;
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

export function AttachmentGallery({ attachments }: { attachments?: AttachmentReference[] }) {
  const token = useAuth((s) => s.token);
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);
  const [failedImages, setFailedImages] = useState<Set<string>>(() => new Set());

  const images = attachments?.filter(isImageAttachment) ?? [];
  const files = attachments?.filter((a) => !isImageAttachment(a)) ?? [];
  const openLightbox = (index: number) => setLightboxIndex(index);
  const closeLightbox = () => setLightboxIndex(null);
  const previousImage = () => {
    setLightboxIndex((index) => {
      if (index === null || images.length === 0) return index;
      return (index - 1 + images.length) % images.length;
    });
  };
  const nextImage = () => {
    setLightboxIndex((index) => {
      if (index === null || images.length === 0) return index;
      return (index + 1) % images.length;
    });
  };

  useEffect(() => {
    if (lightboxIndex === null) return undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") closeLightbox();
      if (event.key === "ArrowLeft" && images.length > 1) previousImage();
      if (event.key === "ArrowRight" && images.length > 1) nextImage();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [lightboxIndex, images.length]);

  if (!attachments || attachments.length === 0) return null;

  const lightboxImage = lightboxIndex === null ? undefined : images[lightboxIndex];

  return (
    <div className="field">
      <label>Attachments</label>
      {images.length > 0 && (
        <div className="attachment-gallery__grid">
          {images.map((a, index) => {
            const label = attachmentDisplayLabel(a);
            const url = attachmentDisplayUrl(a, { token });
            const failed = failedImages.has(a.id);
            return (
              <button
                key={a.id}
                type="button"
                className="attachment-gallery__image-link"
                onClick={() => openLightbox(index)}
                aria-label={`Open image ${label}`}
              >
                {failed ? (
                  <div className="attachment-gallery__image-fallback" data-image-error-fallback>
                    Image unavailable
                  </div>
                ) : (
                  <img
                    className="attachment-gallery__image"
                    src={url}
                    alt={label}
                    loading="lazy"
                    onError={() => setFailedImages((prev) => new Set(prev).add(a.id))}
                  />
                )}
                <div className="attachment-gallery__caption mono faint">
                  {label}
                </div>
              </button>
            );
          })}
        </div>
      )}
      {files.length > 0 && (
        <div className="attachment-gallery__files">
          {files.map((a) => {
            const type = a.content_type || a.type;
            const size = formatAttachmentSize(a.size);
            return (
              <a
                key={a.id}
                className="attachment-gallery__file"
                href={attachmentDisplayUrl(a, { token })}
                target="_blank"
                rel="noopener noreferrer"
                aria-label={`Download attachment ${attachmentDisplayLabel(a)}`}
              >
                <span>Attachment: {attachmentDisplayLabel(a)}</span>
                {type && <span className="faint">{type}</span>}
                {size && <span className="faint">{size}</span>}
              </a>
            );
          })}
        </div>
      )}
      {lightboxImage && (
        <div className="attachment-lightbox" role="dialog" aria-modal="true" aria-label="Image preview">
          <button
            type="button"
            className="attachment-lightbox__backdrop"
            onClick={closeLightbox}
            aria-label="Close image preview"
            data-lightbox-close
          />
          <div className="attachment-lightbox__panel">
            <button type="button" className="attachment-lightbox__close" onClick={closeLightbox} aria-label="Close image preview">x</button>
            {images.length > 1 && (
              <button type="button" className="attachment-lightbox__nav prev" onClick={previousImage} aria-label="Previous image">Prev</button>
            )}
            <img
              className="attachment-lightbox__image"
              src={attachmentDisplayUrl(lightboxImage, { token })}
              alt={attachmentDisplayLabel(lightboxImage)}
            />
            {images.length > 1 && (
              <button type="button" className="attachment-lightbox__nav next" onClick={nextImage} aria-label="Next image">Next</button>
            )}
            <div className="attachment-lightbox__caption">
              {attachmentDisplayLabel(lightboxImage)}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
