import { useMemo, useRef, useState } from "react";
import CodeMirror, { type ReactCodeMirrorRef } from "@uiw/react-codemirror";
import { oneDark } from "@codemirror/theme-one-dark";
import { openSearchPanel } from "@codemirror/search";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { editorExtensions, setVimEnabled, vimEnabled } from "../../lib/cm";

/**
 * A markdown editor with frontmatter highlighting, an edit/preview toggle,
 * an optional vim mode, and a find/replace panel. Shared by the entry editor
 * and the compose modal.
 */
export function MarkdownEditor({
  value,
  onChange,
  height = "52vh",
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  height?: string;
  autoFocus?: boolean;
}) {
  const cmRef = useRef<ReactCodeMirrorRef>(null);
  const [mode, setMode] = useState<"edit" | "preview">("edit");
  const [useVim, setUseVim] = useState(vimEnabled());

  const extensions = useMemo(() => editorExtensions(useVim), [useVim]);

  function toggleVim() {
    const next = !useVim;
    setUseVim(next);
    setVimEnabled(next);
  }

  function find() {
    const view = cmRef.current?.view;
    if (view) {
      openSearchPanel(view);
      view.focus();
    }
  }

  return (
    <div>
      <div className="editor-toolbar">
        <div className="seg">
          <button
            className={mode === "edit" ? "on" : ""}
            onClick={() => setMode("edit")}
          >
            Edit
          </button>
          <button
            className={mode === "preview" ? "on" : ""}
            onClick={() => setMode("preview")}
          >
            Preview
          </button>
        </div>
        <div className="spacer" style={{ flex: 1 }} />
        {mode === "edit" && (
          <>
            <button
              className={`btn sm ghost ${useVim ? "primary" : ""}`}
              onClick={toggleVim}
              title="Toggle vim keybindings"
            >
              vim
            </button>
            <button className="btn sm ghost" onClick={find} title="Find / replace">
              ⌕
            </button>
          </>
        )}
      </div>

      {mode === "edit" ? (
        <div className="editor-frame">
          <CodeMirror
            ref={cmRef}
            value={value}
            height={height}
            theme={oneDark}
            extensions={extensions}
            onChange={onChange}
            autoFocus={autoFocus}
            basicSetup={{
              lineNumbers: true,
              highlightActiveLine: true,
              foldGutter: false,
              highlightSelectionMatches: true,
            }}
          />
        </div>
      ) : (
        <div
          className="editor-frame markdown"
          style={{ height, overflowY: "auto", padding: "0.8rem" }}
        >
          {value.trim() ? (
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{value}</ReactMarkdown>
          ) : (
            <span className="faint">Nothing to preview.</span>
          )}
        </div>
      )}
    </div>
  );
}
