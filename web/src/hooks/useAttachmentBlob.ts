/**
 * Object URL for an attachment's bytes.
 *
 * Attachment content is auth-gated behind a Bearer header, so it has to be
 * fetched rather than pointed at (see `fetchAttachmentObjectURL`). The URL
 * is revoked when the last consumer unmounts, and react-query dedupes the
 * fetch so the same picture appearing in the gallery and in the markdown
 * body downloads once.
 */
import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { fetchAttachmentObjectURL } from "../lib/api";

export function useAttachmentBlob(downloadUrl: string | undefined) {
  const qc = useQueryClient();
  const key = ["attachment", "blob", downloadUrl ?? ""];

  const q = useQuery({
    queryKey: key,
    queryFn: () => fetchAttachmentObjectURL(downloadUrl!),
    enabled: !!downloadUrl,
    // Bytes are content-addressed by sha256 upstream; an attachment's
    // content never changes under a stable id, so never refetch.
    staleTime: Infinity,
    gcTime: 5 * 60_000,
    retry: 1,
  });

  // Revoke when react-query evicts the entry, not on every unmount — two
  // mounted consumers share one URL, and revoking on the first unmount
  // would blank the second.
  useEffect(() => {
    if (!downloadUrl) return;
    const cache = qc.getQueryCache();
    const unsub = cache.subscribe((event) => {
      if (
        event.type === "removed" &&
        event.query.queryKey[0] === "attachment" &&
        event.query.queryKey[2] === downloadUrl
      ) {
        const url = event.query.state.data;
        if (typeof url === "string") URL.revokeObjectURL(url);
      }
    });
    return unsub;
  }, [qc, downloadUrl]);

  return {
    url: q.data ?? null,
    loading: q.isPending && q.fetchStatus !== "idle",
    error: q.error,
  };
}
