import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Modal } from "../../components/common/Modal";
import { MarkdownEditor } from "../../components/editor/MarkdownEditor";
import { Loading, ErrorState } from "../../components/common/states";
import { getEntryRaw, updateEntryRaw } from "../../lib/api";
import { useUI } from "../../store/ui";

// Full-file editor for any entry (note, task, automation, goal). Fetches the raw
// file (frontmatter + body) by path and saves it back via the same content
// negotiation the TUI's $EDITOR flow uses, so the PWA can edit the whole entry.
// Reused by the Brain, Tasks, and Automations tabs.
export function EntryEditModal({
  path,
  title,
  onClose,
  onSaved,
}: {
  path: string;
  title?: string;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const q = useQuery({
    queryKey: ["entry-raw", path],
    queryFn: () => getEntryRaw(path),
  });

  if (q.isLoading) {
    return (
      <Modal title={`Edit · ${title ?? path}`} onClose={onClose}>
        <Loading label="Loading entry…" />
      </Modal>
    );
  }
  if (q.error || q.data == null) {
    return (
      <Modal title={`Edit · ${title ?? path}`} onClose={onClose}>
        <ErrorState error={q.error ?? new Error("Entry not found")} onRetry={() => void q.refetch()} />
      </Modal>
    );
  }

  // Re-mount the editor per path so its internal buffer initializes from the
  // freshly loaded file.
  return (
    <EditorBody
      key={path}
      path={path}
      title={title ?? path}
      initial={q.data}
      onClose={onClose}
      onSaved={onSaved}
    />
  );
}

function EditorBody({
  path,
  title,
  initial,
  onClose,
  onSaved,
}: {
  path: string;
  title: string;
  initial: string;
  onClose: () => void;
  onSaved?: () => void;
}) {
  const toast = useUI((s) => s.toast);
  const [content, setContent] = useState(initial);
  const [busy, setBusy] = useState(false);
  const dirty = content !== initial;

  async function save() {
    setBusy(true);
    try {
      await updateEntryRaw(path, content);
      toast("Entry saved", "success");
      onSaved?.();
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
      <MarkdownEditor value={content} onChange={setContent} autoFocus />
    </Modal>
  );
}
