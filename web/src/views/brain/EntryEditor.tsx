import { useState } from "react";
import { Modal } from "../../components/common/Modal";
import { MarkdownEditor } from "../../components/editor/MarkdownEditor";
import { updateEntry } from "../../lib/api";
import { useUI } from "../../store/ui";

export function EntryEditor({
  path,
  title,
  initialContent,
  onClose,
  onSaved,
}: {
  path: string;
  title: string;
  initialContent: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const [content, setContent] = useState(initialContent);
  const [busy, setBusy] = useState(false);
  const dirty = content !== initialContent;

  async function save() {
    setBusy(true);
    try {
      await updateEntry(path, { content });
      toast("Entry saved", "success");
      onSaved();
      onClose();
    } catch (e) {
      toast(e instanceof Error ? e.message : "Save failed", "error");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal
      title={`Edit · ${title}`}
      onClose={onClose}
      footer={
        <>
          <button className="btn ghost" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button
            className="btn primary"
            style={{ marginLeft: "auto" }}
            onClick={() => void save()}
            disabled={busy || !dirty}
          >
            {busy ? "Saving…" : "Save"}
          </button>
        </>
      }
    >
      <MarkdownEditor value={content} onChange={setContent} />
    </Modal>
  );
}
