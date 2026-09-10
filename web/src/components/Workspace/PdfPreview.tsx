import { useEffect, useRef, useState } from "react";
import {
  getDocument,
  GlobalWorkerOptions,
  type PDFDocumentProxy,
  type RenderTask,
} from "pdfjs-dist";
import workerURL from "pdfjs-dist/build/pdf.worker.min.mjs?url";

GlobalWorkerOptions.workerSrc = workerURL;

export default function PdfPreview({
  url,
  label,
}: {
  url: string;
  label: string;
}) {
  const [pdf, setPDF] = useState<PDFDocumentProxy | null>(null);
  const [page, setPage] = useState(1);
  const [error, setError] = useState(false);
  const [ready, setReady] = useState(false);
  const [text, setText] = useState("");
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    let live = true;
    setError(false);
    setPDF(null);
    setPage(1);
    const task = getDocument({
      url,
      cMapUrl: "/assets/pdf/cmaps/",
      cMapPacked: true,
      standardFontDataUrl: "/assets/pdf/standard_fonts/",
      wasmUrl: "/assets/pdf/wasm/",
    });
    void task.promise
      .then((doc) => {
        if (live) setPDF(doc);
      })
      .catch(() => {
        if (live) setError(true);
      });
    return () => {
      live = false;
      void task.destroy();
    };
  }, [url]);
  useEffect(() => {
    if (!pdf) return;
    let live = true;
    let render: RenderTask | undefined;
    setReady(false);
    setText("");
    void pdf
      .getPage(page)
      .then(async (p) => {
        if (!live || !canvas.current) return;
        const viewport = p.getViewport({ scale: 1 });
        // Bound bitmap size, including unusually large page dimensions.
        const scale = Math.min(
          2,
          2000 / viewport.width,
          2800 / viewport.height,
        );
        const sized = p.getViewport({ scale });
        const target = canvas.current;
        target.width = Math.ceil(sized.width);
        target.height = Math.ceil(sized.height);
        render = p.render({ canvas: target, viewport: sized });
        await render.promise;
        const content = await p.getTextContent();
        if (live) {
          setText(
            content.items
              .map((item) => ("str" in item ? item.str : ""))
              .join(" "),
          );
          setReady(true);
        }
      })
      .catch(() => {
        if (live) setError(true);
      });
    return () => {
      live = false;
      render?.cancel();
    };
  }, [pdf, page]);
  if (error)
    return (
      <p role="alert">
        Unable to preview this PDF. It may be damaged or password protected.
        Download it to view it.
      </p>
    );
  return (
    <div className="attachment-pdf">
      {pdf && (
        <nav aria-label="PDF pages">
          <button
            type="button"
            disabled={page <= 1}
            onClick={() => setPage(page - 1)}
          >
            Previous page
          </button>
          <span>
            Page {page} of {pdf.numPages}
          </span>
          <button
            type="button"
            disabled={page >= pdf.numPages}
            onClick={() => setPage(page + 1)}
          >
            Next page
          </button>
        </nav>
      )}
      {!ready && <p role="status">Rendering PDF…</p>}
      <canvas
        ref={canvas}
        role="img"
        aria-label={`${label}, page ${page}${text ? ": " + text : ""}`}
        data-rendered={ready}
        style={{ display: ready ? "block" : "none" }}
      />
    </div>
  );
}
